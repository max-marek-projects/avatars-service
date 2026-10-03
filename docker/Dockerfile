# syntax=docker/dockerfile:1.7

# ---------- build stage ----------
FROM golang:1.27-alpine AS builder

# Update Alpine packages to patch OS-level CVEs.
RUN apk update && apk upgrade --no-cache

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=docker
ARG DATE
ARG COMMIT

ENV CGO_ENABLED=0 GOOS=linux GOFLAGS="-trimpath"

RUN go build \
    -ldflags "-s -w \
    -X main.buildVersion=${VERSION} \
    -X main.buildDate=${DATE} \
    -X main.buildCommit=${COMMIT}" \
    -o /out/server ./cmd/server \
    && go build \
    -ldflags "-s -w \
    -X main.buildVersion=${VERSION} \
    -X main.buildDate=${DATE} \
    -X main.buildCommit=${COMMIT}" \
    -o /out/worker ./cmd/worker

# ---------- runtime stage ----------
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/server      /app/server
COPY --from=builder /out/worker      /app/worker
COPY --from=builder /src/migrations  /app/migrations
COPY --from=builder /src/web         /app/web

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/app/server"]