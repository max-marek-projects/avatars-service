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

.PHONY: kuber vault-init

kuber:
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
	helm upgrade --install prometheus-crds prometheus-community/prometheus-operator-crds

	@echo "=== Installing Vault & VSO ==="
	helm upgrade --install vault oci://registry-1.docker.io/bitnamicharts/vault \
		-n vault --create-namespace \
		-f deploy/vault/vault-values.yaml

	# CRDs
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

	@echo "=== Building App ==="
	docker build --progress=plain -t avatars-service:dev -f docker/Dockerfile .

	@echo "=== Waiting for VSO to create secrets ==="
	kubectl wait --for=create secret/my-app-avatars-service-secrets --timeout=60s || true

	@echo "=== Deploying App ==="
	helm upgrade --install my-app deploy/helm/avatars-service

vault-init:
	@echo "=== Initializing Vault ==="
	@if kubectl exec -n vault vault-server-0 -- vault status 2>/dev/null | grep -q "Initialized.*true"; then \
		echo "✅ Vault is already initialized. Skipping init."; \
	else \
		kubectl exec -n vault vault-server-0 -- vault operator init \
			-key-shares=1 -key-threshold=1 \
			-format=json > vault-init.json; \
		echo "✅ Vault initialized. Check vault-init.json for unseal key and root token."; \
	fi
	@echo "⚠️  Don't forget to unseal Vault and write secrets to it (see README)."


# ========== GENERATE ==========

mocks: # generate all mocks
	go generate ./...

# ========== TESTING AND LINTING ==========

lint:
	gofmt -w .
	goimports -w .

test:  # run tests
	go test -coverprofile=coverage.out ./internal/... ./cmd/...
	go tool cover -func=coverage.out | grep total
	go tool cover -html=coverage.out -o coverage.html
