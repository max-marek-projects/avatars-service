# avatars-service

Backend service for managing user avatars: upload, storage in an S3-compatible backend, asynchronous thumbnail generation, and delivery of the user's active avatar.

## Features

- **REST API** for uploading, fetching metadata, downloading, and deleting avatars.
- **Asynchronous processing**: thumbnail generation (100×100, 300×300) in a separate worker process via RabbitMQ.
- **Soft delete** with asynchronous S3 object cleanup.
- **Placeholder** for users without an avatar (configurable S3 key).
- **Observability** out of the box: OpenTelemetry → Jaeger, Prometheus metrics, JSON logs with `trace_id` → OpenSearch + Dashboards, alerts via Alertmanager.
- **Graceful shutdown** of both processes on SIGINT/SIGTERM/SIGQUIT.

## Architecture

```text
                          ┌──────────────┐
              HTTP :8080  │   server     │  /api/v1/avatars
   client ──────────────► │  (chi+otel)  │
                          └──────┬───────┘
                                 │ publish
                                 ▼
                          ┌──────────────┐       ┌──────────────┐
                          │  rabbitmq    │◄──────┤   worker     │  consume
                          └──────────────┘       │              │
                                                 └──────┬───────┘
                                                        │ upload thumbs
                          ┌──────────────┐              │
                          │  postgres    │◄─────────────┤
                          │  (metadata)  │              │
                          └──────────────┘              │
                          ┌──────────────┐              │
                          │  minio / S3  │◄─────────────┘
                          └──────────────┘

Observability:
  server/worker ──► OTLP ──► jaeger
  server/worker ──► metrics ──► prometheus ──► grafana ──► alertmanager
  server/worker ──► fluentd ──► fluent-bit ──► opensearch ──► opensearch-dashboards
```

## Quick start

Prerequisites: **Docker Desktop** (or Docker + Compose v2), **Go 1.22+** for local development.

```bash
git clone https://github.com/<you>/avatars-service
cd avatars-service

# bring up the whole stack and start server + worker
make server
```

The service becomes reachable at `http://localhost:8080` after 30–60 seconds.

## API

| Method | Path | Headers | Description |
| --- | --- | --- | --- |
| `GET` | `/health` | — | Readiness probe (DB + S3 + broker). |
| `GET` | `/metrics` | — | Prometheus metrics (server only). |
| `POST` | `/api/v1/avatars` | `X-User-ID` | Upload an avatar (`multipart/form-data`, field `file`). |
| `GET` | `/api/v1/avatars/{id}` | — | Download the original. |
| `GET` | `/api/v1/avatars/{id}/metadata` | — | Metadata + thumbnail URLs. |
| `DELETE` | `/api/v1/avatars/{id}` | `X-User-ID` | Soft delete (owner check). |
| `GET` | `/api/v1/users/{user_id}/avatar` | — | Active avatar or placeholder. |
| `DELETE` | `/api/v1/users/{user_id}/avatar` | `X-User-ID` | Delete the active avatar. |
| `GET` | `/api/v1/users/{user_id}/avatars` | — | List active avatars. |

HTML pages for manual testing: `GET /web/upload`, `GET /web/gallery/{user_id}`.

Limits: file size ≤ `MAX_FILE_SIZE` (10 MiB by default), MIME ∈ `ALLOWED_MIME_TYPES` (`image/jpeg,image/png,image/webp`).

## Configuration

Values are resolved in this order (highest priority first): CLI flags → JSON file (`-c path.json`) → env → `.env` → defaults.

| Variable | Default | Purpose |
| --- | --- | --- |
| `SERVER_ADDRESS` | `:8080` | HTTP server address. |
| `LOGGER_LEVEL` | `INFO` | `DEBUG` / `INFO` / `WARN` / `ERROR`. |
| `DATABASE_URI` | — | PostgreSQL DSN. |
| `MIGRATIONS` | `./migrations` | Path to SQL migrations. |
| `S3_ENDPOINT` | `minio:9000` | S3 host:port. |
| `S3_ACCESS_KEY` / `S3_SECRET_KEY` | `minio` / `minio12345` | Credentials. |
| `S3_BUCKET` | `avatars` | Bucket name. |
| `S3_USE_SSL` | `false` | HTTPS for S3. |
| `RABBIT_URL` | `amqp://guest:guest@rabbitmq:5672/` | RabbitMQ DSN. |
| `MAX_FILE_SIZE` | `10485760` | Size limit, bytes. |
| `ALLOWED_MIME_TYPES` | `image/jpeg,image/png,image/webp` | Whitelist. |
| `DEFAULT_AVATAR_KEY` | — | Placeholder S3 key. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | `jaeger:4318`. Empty disables tracing. |
| `OTEL_SERVICE_NAME` | `avatars-service` | Service name in traces. |
| `OTEL_SAMPLE_RATIO` | `1.0` | Fraction of traces to sample (0..1]. |

Full list — in `internal/config/config.go`.

## Observability

| Component | URL | What to look at |
| --- | --- | --- |
| Jaeger UI | <http://localhost:16686> | Traces, cross-process correlation server → worker by `trace_id`. |
| Prometheus | <http://localhost:9090> | `http_requests_total`, `avatar_uploads_total`, `avatar_processing_duration_seconds`. Rules at `/rules`. |
| Alertmanager | <http://localhost:9093> | Active and resolved alerts. |
| Grafana | <http://localhost:3000> (admin/admin) | Dashboards for metrics. |
| OpenSearch Dashboards | <http://localhost:5601> | Search logs by `trace_id`, `level`, `msg`. |
| RabbitMQ Management | <http://localhost:15672> (guest/guest) | Queues `avatars.upload` / `avatars.delete`. |
| MinIO Console | <http://localhost:9001> | Bucket `avatars`. |

### Verifying end-to-end correlation

1. Upload an avatar and grab a `trace_id` from Jaeger (open any trace).
2. In OpenSearch Dashboards run `trace_id:"<id>"` — you will see logs from **both** server and worker.
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
docker/           # compose, Dockerfile, observability configs
```

## License

MIT.
