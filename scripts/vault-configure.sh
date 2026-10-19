#!/usr/bin/env bash
set -euo pipefail

VAULT_POD="vault-server-0"
VAULT_NS="vault"
VAULT_ADDR="http://127.0.0.1:8200"

if [ ! -f vault-init.json ]; then
    echo "❌ vault-init.json not found. Run 'make vault-init' first."
    exit 1
fi

ROOT_TOKEN=$(tr -d '\n ' < vault-init.json | grep -o '"root_token":"[^"]*"' | cut -d'"' -f4)
if [ -z "$ROOT_TOKEN" ]; then
    echo "❌ Could not parse root_token from vault-init.json"
    exit 1
fi

DATABASE_URI="postgres://${POSTGRES_USER:-avatars}:${POSTGRES_PASSWORD:-avatars}@postgres:5432/${POSTGRES_DB:-avatars}?sslmode=disable"
RABBIT_URL="amqp://${RABBIT_USER:-guest}:${RABBIT_PASSWORD:-guest}@rabbitmq:5672/"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-minio}"
S3_SECRET_KEY="${S3_SECRET_KEY:-minio12345}"

ve() {
    kubectl exec -n "$VAULT_NS" "$VAULT_POD" -- env \
        VAULT_ADDR="$VAULT_ADDR" VAULT_TOKEN="$ROOT_TOKEN" "$@"
}

echo "→ Enabling KV v2 (secret/)..."
ve vault secrets enable -path=secret kv-v2 2>/dev/null || echo "  (already enabled)"

echo "→ Writing application secrets..."
ve vault kv put secret/avatars/db DATABASE_URI="$DATABASE_URI" >/dev/null
ve vault kv put secret/avatars/s3 S3_ACCESS_KEY="$S3_ACCESS_KEY" S3_SECRET_KEY="$S3_SECRET_KEY" >/dev/null
ve vault kv put secret/avatars/rabbit RABBIT_URL="$RABBIT_URL" >/dev/null

echo "→ Enabling Kubernetes auth..."
ve vault auth enable kubernetes 2>/dev/null || echo "  (already enabled)"

echo "→ Configuring Kubernetes auth..."
kubectl exec -n "$VAULT_NS" "$VAULT_POD" -- sh -c "
VAULT_ADDR='$VAULT_ADDR' VAULT_TOKEN='$ROOT_TOKEN' \
vault write auth/kubernetes/config \
    kubernetes_host='https://kubernetes.default.svc:443' \
    kubernetes_ca_cert=@/var/run/secrets/kubernetes.io/serviceaccount/ca.crt \
    token_reviewer_jwt=@/var/run/secrets/kubernetes.io/serviceaccount/token
" >/dev/null

echo "→ Writing policies..."

cat <<'EOF' | kubectl exec -i -n "$VAULT_NS" "$VAULT_POD" -- env \
    VAULT_ADDR="$VAULT_ADDR" VAULT_TOKEN="$ROOT_TOKEN" vault policy write avatars-policy -
path "secret/data/avatars/*" { capabilities = ["read"] }
EOF

cat <<'EOF' | kubectl exec -i -n "$VAULT_NS" "$VAULT_POD" -- env \
    VAULT_ADDR="$VAULT_ADDR" VAULT_TOKEN="$ROOT_TOKEN" vault policy write vault-secrets-operator-policy -
path "secret/data/*" { capabilities = ["read"] }
path "auth/token/renew-self" { capabilities = ["update"] }
path "auth/token/lookup-self" { capabilities = ["read"] }
EOF

echo "→ Creating roles..."

ve vault write auth/kubernetes/role/avatars \
    bound_service_account_names=my-app-avatars-service \
    bound_service_account_namespaces=default \
    policies=avatars-policy ttl=1h >/dev/null

ve vault write auth/kubernetes/role/vault-secrets-operator \
    bound_service_account_names=vault-secrets-operator \
    bound_service_account_namespaces=vault-secrets-operator \
    policies=vault-secrets-operator-policy ttl=1h >/dev/null

echo "✅ Vault configured successfully"