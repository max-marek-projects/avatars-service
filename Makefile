# ========== BUILD / RUN ==========

VERSION := $(shell git describe --tags --always 2>/dev/null || echo "")
DATE    := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
COMMIT  := $(shell git rev-parse HEAD 2>/dev/null || echo "")

export VERSION
export DATE
export COMMIT

server:  # build and run
	docker compose -f ./docker/docker-compose.yml up -d
	docker compose -f ./docker/docker-compose.yml up --build server worker

ifneq (,$(wildcard ./.env))
    include .env
    export
endif

POSTGRES_USER ?= avatars
POSTGRES_PASSWORD ?= avatars
POSTGRES_DB ?= avatars
RABBIT_USER ?= guest
RABBIT_PASSWORD ?= guest
S3_ACCESS_KEY ?= minio
S3_SECRET_KEY ?= minio12345
S3_BUCKET ?= avatars

.PHONY: kuber infra vault-wait vault-init vault-unseal vault-configure app-deploy

# Full end-to-end pipeline
kuber: infra vault-wait vault-init vault-unseal vault-configure app-deploy
	@echo "✅ All components deployed and configured"

# --- Infrastructure ---
infra:
	@echo "=== Installing Infrastructure ==="
	helm upgrade --install postgres oci://registry-1.docker.io/bitnamicharts/postgresql \
		--set fullnameOverride=postgres \
		--set auth.username=$(POSTGRES_USER) \
		--set auth.password=$(POSTGRES_PASSWORD) \
		--set auth.database=$(POSTGRES_DB)

	helm upgrade --install rabbitmq oci://registry-1.docker.io/bitnamicharts/rabbitmq \
		--set fullnameOverride=rabbitmq \
		--set auth.username=$(RABBIT_USER) \
		--set auth.password=$(RABBIT_PASSWORD) \
		--set image.repository=bitnamilegacy/rabbitmq \
		--set volumePermissions.image.repository=bitnamilegacy/os-shell \
		--set global.security.allowInsecureImages=true

	helm upgrade --install minio oci://registry-1.docker.io/bitnamicharts/minio \
		--set fullnameOverride=minio \
		--set auth.rootUser=$(S3_ACCESS_KEY) \
		--set auth.rootPassword=$(S3_SECRET_KEY) \
		--set defaultBuckets=$(S3_BUCKET) \
		--set image.repository=bitnamilegacy/minio \
		--set clientImage.repository=bitnamilegacy/minio-client \
		--set console.enabled=false \
		--set volumePermissions.image.repository=bitnamilegacy/os-shell \
		--set global.security.allowInsecureImages=true

	helm upgrade --install jaeger jaegertracing/jaeger

	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
	helm repo update
	helm upgrade --install monitoring prometheus-community/kube-prometheus-stack \
		-n monitoring --create-namespace \
		--set grafana.adminPassword=admin \
		--set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
		--set prometheus.prometheusSpec.podMonitorSelectorNilUsesHelmValues=false \
		--set prometheus.prometheusSpec.resources.requests.memory=256Mi \
		--set prometheus.prometheusSpec.resources.requests.cpu=100m \
		--set prometheus.prometheusSpec.resources.limits.memory=512Mi \
		--set alertmanager.enabled=true \
		--wait --timeout 10m

	helm upgrade --install vault oci://registry-1.docker.io/bitnamicharts/vault \
		-n vault --create-namespace \
		-f deploy/vault/vault-values.yaml

	kubectl apply -f https://raw.githubusercontent.com/hashicorp/vault-secrets-operator/main/config/crd/bases/secrets.hashicorp.com_vaultauths.yaml
	kubectl apply -f https://raw.githubusercontent.com/hashicorp/vault-secrets-operator/main/config/crd/bases/secrets.hashicorp.com_vaultconnections.yaml
	kubectl apply -f https://raw.githubusercontent.com/hashicorp/vault-secrets-operator/main/config/crd/bases/secrets.hashicorp.com_vaultstaticsecrets.yaml

	helm upgrade --install vault-secrets-operator \
		oci://ghcr.io/ricoberger/charts/vault-secrets-operator \
		-n vault-secrets-operator --create-namespace \
		--set "vault.address=http://vault-server.vault.svc.cluster.local:8200" \
		--set "vault.authMethod=kubernetes" \
		--set "vault.kubernetesRole=avatars" \
		--set "vault.kubernetesPath=auth/kubernetes"

	kubectl apply -f deploy/vault/vault-auth.yaml

# --- Wait for Vault pod ---
vault-wait:
	@echo "=== Waiting for Vault pod ==="
	kubectl wait --for=create pod/vault-server-0 -n vault --timeout=120s
	kubectl wait --for=jsonpath='{.status.phase}'=Running pod/vault-server-0 -n vault --timeout=120s

# --- Init Vault (idempotent) ---
vault-init:
	@echo "=== Initializing Vault ==="
	@if [ -f vault-init.json ]; then \
		echo "✅ vault-init.json already exists, skipping"; \
	elif kubectl exec -n vault vault-server-0 -- vault status 2>/dev/null | grep -q "Initialized.*true"; then \
		echo "❌ Vault is already initialized but vault-init.json is missing."; \
		echo "   Restore the file, or reset Vault with 'make reset-vault'."; \
		exit 1; \
	else \
		kubectl exec -n vault vault-server-0 -- vault operator init \
			-key-shares=1 -key-threshold=1 -format=json > vault-init.json; \
		echo "✅ Vault initialized, credentials saved to vault-init.json"; \
	fi

# --- Unseal Vault (idempotent) ---
vault-unseal:
	@echo "=== Unsealing Vault ==="
	@test -f vault-init.json || (echo "❌ vault-init.json missing. Run 'make vault-init'."; exit 1)
	@if kubectl exec -n vault vault-server-0 -- vault status 2>/dev/null | grep -q "Sealed.*false"; then \
		echo "✅ Already unsealed, skipping"; \
	else \
		UNSEAL_KEY=$$(tr -d '\n ' < vault-init.json | grep -o '"unseal_keys_b64":\["[^"]*"' | cut -d'"' -f4); \
		if [ -z "$$UNSEAL_KEY" ]; then echo "❌ Could not parse unseal key"; exit 1; fi; \
		kubectl exec -n vault vault-server-0 -- vault operator unseal "$$UNSEAL_KEY" >/dev/null; \
		echo "✅ Vault unsealed"; \
	fi

# --- Configure Vault (secrets, auth, roles) ---
vault-configure:
	@echo "=== Configuring Vault ==="
	@bash scripts/vault-configure.sh

# --- Build and deploy the application ---
app-deploy:
	@echo "=== Building app image ==="
	docker context use default
	docker build --progress=plain -t avatars-service:dev -f docker/Dockerfile .

	@echo "=== Deploying Helm chart ==="
	helm upgrade --install my-app deploy/helm/avatars-service

	@echo "=== Waiting for VSO to sync secrets (up to 60s) ==="
	@for i in $$(seq 1 30); do \
		if kubectl get secret my-app-avatars-service-secrets >/dev/null 2>&1; then \
			echo "✅ Secret created by VSO"; exit 0; \
		fi; \
		sleep 2; \
	done; \
	echo "⚠️  Secret not ready yet, pods may retry"; exit 0

# --- Full teardown ---
reset:
	helm uninstall my-app || true
	helm uninstall vault -n vault || true
	kubectl delete pvc -n vault -l app.kubernetes.io/instance=vault --ignore-not-found
	rm -f vault-init.json

# ========== GENERATE ==========

mocks: # generate all mocks
	go generate ./...

.PHONY: swagger swagger-serve

swagger:
	@echo "=== Generating Swagger docs ==="
	swag init -g cmd/server/main.go -o docs --parseDependency --parseInternal --parseDepth 2
	@echo "✅ docs/swagger.yaml and docs/swagger.json generated"

swagger-serve:
	@echo "=== Serving Swagger UI at http://localhost:8080/swagger ==="
	swag serve --dir=./docs --port=8090

# ========== TESTING AND LINTING ==========

lint:
	gofmt -w .
	goimports -w .

test:  # run tests
	go test -coverprofile=coverage.out ./internal/... ./cmd/...
	go tool cover -func=coverage.out | grep total
	go tool cover -html=coverage.out -o coverage.html
