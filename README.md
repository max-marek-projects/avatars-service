# avatars-service

Backend service for managing user avatars: upload, storage in an S3-compatible backend, asynchronous thumbnail generation, and delivery of the user's active avatar.

## Features

- **REST API** for uploading, fetching metadata, downloading, and deleting avatars.
- **Asynchronous processing**: thumbnail generation (100×100, 300×300) in a separate worker process via RabbitMQ.
- **Soft delete** with asynchronous S3 object cleanup.
- **Placeholder** for users without an avatar (configurable S3 key).
- **Observability** out of the box: OpenTelemetry → Jaeger, Prometheus metrics, JSON logs with `trace_id` → OpenSearch + Dashboards, alerts via Alertmanager.
- **Kubernetes & Helm** ready: Includes a full Helm chart with Deployments, Services, Ingress, HPA, NetworkPolicy, and ServiceMonitor.
- **Graceful shutdown** of both processes on SIGINT/SIGTERM/SIGQUIT.

## Architecture

```mermaid
flowchart TD
    Client([Client / Browser]) -->|HTTP :80| Ingress[Nginx Ingress]

    subgraph K8s Cluster
        Ingress --> SvcServer[Service: Server]
        Ingress --> SvcWorker[Service: Worker-Metrics]

        subgraph Server Deployment
            SvcServer --> Pod1[Server Pod]
            SvcServer --> Pod2[Server Pod]
        end

        subgraph Worker Deployment
            W1[Worker Pod]
            W2[Worker Pod]
        end

        Pod1 & Pod2 -->|SQL| DB[(PostgreSQL)]
        W1 & W2 -->|SQL| DB

        Pod1 & Pod2 -->|Upload| S3[(MinIO / S3)]
        W1 & W2 -->|Thumbnails| S3

        Pod1 & Pod2 -->|Publish| MQ[(RabbitMQ)]
        MQ -->|Consume| W1 & W2

        Pod1 & W1 -->|Traces| Jaeger[Jaeger]
        Prometheus[Prometheus] -.->|Scrape /metrics| SvcServer & SvcWorker
    end
```

## Quick start

Prerequisites: **Docker Desktop** (or Docker + Compose v2), **Go 1.22+** for local development.

```bash
git clone https://github.com/&lt;you&gt;/avatars-service
cd avatars-service

# bring up the whole stack and start server + worker
make server
```

The service becomes reachable at `http://localhost:8080` after 30–60 seconds.

## Kubernetes Deployment (Helm)

The project is packaged as a Helm chart located in `deploy/helm/avatars-service`.

### Prerequisites

- A running Kubernetes cluster (e.g., Rancher Desktop, Minikube, Kind).
- Helm 3 installed.
- Nginx Ingress Controller installed (`ingress-nginx`). If using Rancher Desktop, disable the default Traefik controller.
- Prometheus Operator CRDs installed (for `ServiceMonitor`).

### Vault Integration

Vault secrets are the source of truth for `DATABASE_URI`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, and `RABBIT_URL`. They are synced into a Kubernetes Secret by [Vault Secrets Operator](https://developer.hashicorp.com/vault/docs/platform/k8s/vso).

#### First-time setup (fresh cluster)

```bash
# 1. Install everything except the application
make infra

# 2. Wait for Vault, then initialize & unseal it
make vault-wait
make vault-init       # creates vault-init.json (keep it local, do NOT commit)
make vault-unseal

# 3. Enable Kubernetes auth, write policies/roles, and store secrets in Vault
make vault-configure

# 4. Build and deploy the application
make app-deploy
```

Or as a single command

```bash
make kuber
```

**Configure hosts**:
Add the following line to `C:\Windows\System32\drivers\etc\hosts` (or `/etc/hosts`):

```text
127.0.0.1 avatars.example.com
```

1. Verify: `http://avatars.example.com/health`

## API

| Method   | Path                              | Headers     | Description                                             |
| -------- | --------------------------------- | ----------- | ------------------------------------------------------- |
| `GET`    | `/health`                         | —           | Readiness probe (DB + S3 + broker).                     |
| `GET`    | `/metrics`                        | —           | Prometheus metrics (server only).                       |
| `POST`   | `/api/v1/avatars`                 | `X-User-ID` | Upload an avatar (`multipart/form-data`, field `file`). |
| `GET`    | `/api/v1/avatars/{id}`            | —           | Download the original.                                  |
| `GET`    | `/api/v1/avatars/{id}/metadata`   | —           | Metadata + thumbnail URLs.                              |
| `DELETE` | `/api/v1/avatars/{id}`            | `X-User-ID` | Soft delete (owner check).                              |
| `GET`    | `/api/v1/users/{user_id}/avatar`  | —           | Active avatar or placeholder.                           |
| `DELETE` | `/api/v1/users/{user_id}/avatar`  | `X-User-ID` | Delete the active avatar.                               |
| `GET`    | `/api/v1/users/{user_id}/avatars` | —           | List active avatars.                                    |

HTML pages for manual testing: `GET /web/upload`, `GET /web/gallery/{user_id}`.

Limits: file size ≤ `MAX_FILE_SIZE` (10 MiB by default), MIME ∈ `ALLOWED_MIME_TYPES` (`image/jpeg,image/png,image/webp`).

## Configuration

Values are resolved in this order (highest priority first): CLI flags → JSON file (`-c path.json`) → env → `.env` → defaults.

| Variable                          | Default                             | Purpose                                                                    |
| --------------------------------- | ----------------------------------- | -------------------------------------------------------------------------- |
| `SERVER_ADDRESS`                  | `:8080`                             | HTTP server address.                                                       |
| `LOGGER_LEVEL`                    | `INFO`                              | `DEBUG` / `INFO` / `WARN` / `ERROR`.                                       |
| `USER`                            | `nonroot`                           | System user for OpenTelemetry resource detection (required in distroless). |
| `DATABASE_URI`                    | —                                   | PostgreSQL DSN.                                                            |
| `MIGRATIONS`                      | `./migrations`                      | Path to SQL migrations.                                                    |
| `S3_ENDPOINT`                     | `minio:9000`                        | S3 host:port.                                                              |
| `S3_ACCESS_KEY` / `S3_SECRET_KEY` | `minio` / `minio12345`              | Credentials.                                                               |
| `S3_BUCKET`                       | `avatars`                           | Bucket name.                                                               |
| `S3_USE_SSL`                      | `false`                             | HTTPS for S3.                                                              |
| `RABBIT_URL`                      | `amqp://guest:guest@rabbitmq:5672/` | RabbitMQ DSN.                                                              |
| `MAX_FILE_SIZE`                   | `10485760`                          | Size limit, bytes.                                                         |
| `ALLOWED_MIME_TYPES`              | `image/jpeg,image/png,image/webp`   | Whitelist.                                                                 |
| `DEFAULT_AVATAR_KEY`              | —                                   | Placeholder S3 key.                                                        |
| `OTEL_EXPORTER_OTLP_ENDPOINT`     | —                                   | `jaeger:4318`. Empty disables tracing.                                     |
| `OTEL_SERVICE_NAME`               | `avatars-service`                   | Service name in traces.                                                    |
| `OTEL_SAMPLE_RATIO`               | `1.0`                               | Fraction of traces to sample (0..1].                                       |

Full list — in `internal/config/config.go`.

## Observability

| Component             | URL                                    | What to look at                                                                                         |
| --------------------- | -------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| Jaeger UI             | <http://localhost:16686>               | Traces, cross-process correlation server → worker by `trace_id`.                                        |
| Prometheus            | <http://localhost:9090>                | `http_requests_total`, `avatar_uploads_total`, `avatar_processing_duration_seconds`. Rules at `/rules`. |
| Alertmanager          | <http://localhost:9093>                | Active and resolved alerts.                                                                             |
| Grafana               | <http://localhost:3000> (admin/admin)  | Dashboards for metrics.                                                                                 |
| OpenSearch Dashboards | <http://localhost:5601>                | Search logs by `trace_id`, `level`, `msg`.                                                              |
| RabbitMQ Management   | <http://localhost:15672> (guest/guest) | Queues `avatars.upload` / `avatars.delete`.                                                             |
| MinIO Console         | <http://localhost:9001>                | Bucket `avatars`.                                                                                       |

### Verifying end-to-end correlation

1. Upload an avatar and grab a `trace_id` from Jaeger (open any trace).
2. In OpenSearch Dashboards run `trace_id:"&lt;id&gt;"` — you will see logs from **both** server and worker.
3. Stop the worker (`docker compose stop worker`) — after ~45 seconds `WorkerDown` transitions to `Firing` at `http://localhost:9090/alerts` and becomes Active at `http://localhost:9093`.

## Development

```bash
make test        # go test -coverprofile=... ./internal/... ./cmd/...
make server      # bring up the whole stack + rebuild server and worker
```

### Migrations

Located in `migrations/`, applied automatically on startup by both processes (idempotent, `golang-migrate`). Format: `NNNNNN_name.up.sql` / `.down.sql`. A dirty schema version can be forced with the `-f` flag.

## Project layout

```text
cmd/
  server/         # HTTP binary
  worker/         # async consumer
internal/
  broker/         # RabbitMQ publisher + consumer
  config/         # env/flags/JSON config
  handlers/       # HTTP layer (chi)
  logger/         # slog + trace_id/span_id correlation
  metrics/        # Prometheus metrics
  middlewares/    # request logging + metrics
  models/         # domain types + Duration
  repository/     # PostgreSQL (pgx + golang-migrate)
  server/         # chi router assembly
  services/       # business logic
  storage/        # S3/MinIO (aws-sdk-go v2)
  tracing/        # OTel init + helpers
  worker/         # event processing (resize, delete)
migrations/       # SQL migrations
deploy/helm/      # Kubernetes Helm chart
deploy/vault/     # Kubernetes Vault chart
docker/           # compose, Dockerfile, observability configs
docs/             # OpenAPI swagger
```

## API Documentation (Swagger UI)

Interactive Swagger UI is served by the application itself:

- **Kubernetes:** http://avatars.example.com/swagger/index.html
- **Local:** http://localhost:8080/swagger/index.html

**Generate the spec** (after changing handler annotations):

```bash
make swagger
```

## License

MIT.
