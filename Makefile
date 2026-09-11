.PHONY: build test vet tidy run-receiver run-worker run-api \
        analyzer-test dashboard-build up down demo

# ---- Go ----
build:
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy

run-receiver:
	go run ./cmd/receiver

run-worker:
	go run ./cmd/worker

run-api:
	go run ./cmd/api

# Enqueue a sample PR review offline (no GitHub, no API keys).
demo:
	OFFLINE=true go run ./cmd/demo

# ---- Python analyzer ----
analyzer-test:
	cd analyzer && python -m pytest -q

# ---- Dashboard ----
dashboard-build:
	cd web/dashboard && npm ci && npm run build

# ---- Whole platform ----
# Newer Docker uses `docker compose`; the standalone binary is `docker-compose`.
COMPOSE ?= docker-compose

up:
	$(COMPOSE) up --build -d

down:
	$(COMPOSE) down -v
