# ========== BUILD / RUN ==========

VERSION := $(shell git describe --tags --always 2>/dev/null || echo "")
DATE    := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
COMMIT  := $(shell git rev-parse HEAD 2>/dev/null || echo "")

export VERSION
export DATE
export COMMIT

server:  # build and run binary
	docker compose -f ./docker/docker-compose.yml up -d --build
	docker compose -f ./docker/docker-compose.yml logs -f server worker

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
