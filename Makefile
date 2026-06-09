# INEC Election Platform — Service Management
#
# 12 microservices: 8 Go + 2 Rust + 2 Python
#
# Quick start:
#   make dev          # Start monolith locally (requires PostgreSQL)
#   make dev-distributed  # Start all services independently
#   make docker-up    # Start full stack in Docker
#   make test         # Run all tests
#   make lint         # Run all linters

.PHONY: help dev dev-distributed dev-go dev-rust dev-python docker-up docker-down test lint build clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# --- Development (Monolith) ---

dev: ## Start Go monolith (all routes in one binary)
	cd inec-go-backend && go run . --port=8088

# --- Development (Distributed) ---

dev-distributed: ## Start all services independently (microservice mode)
	@echo "Starting infrastructure..."
	@echo "Starting Go services..."
	cd inec-go-backend && go run ./cmd/middleware-svc --port=8085 &
	cd inec-go-backend && go run ./cmd/auth-svc --port=8090 &
	cd inec-go-backend && go run ./cmd/election-svc --port=8091 &
	cd inec-go-backend && go run ./cmd/biometric-svc --port=8092 &
	cd inec-go-backend && go run ./cmd/geo-svc --port=8093 &
	cd inec-go-backend && go run ./cmd/compliance-svc --port=8094 &
	cd inec-go-backend && go run ./cmd/ingestion-svc --port=8095 &
	cd inec-go-backend && go run ./cmd/bvas-svc --port=8096 &
	@echo "Starting gateway (distributed mode)..."
	cd inec-go-backend && go run ./cmd/gateway --port=8088 --distributed

dev-gateway: ## Start gateway in distributed mode
	cd inec-go-backend && go run ./cmd/gateway --port=8088 --distributed

dev-auth: ## Start auth service independently
	cd inec-go-backend && go run ./cmd/auth-svc --port=8090

dev-election: ## Start election service independently
	cd inec-go-backend && go run ./cmd/election-svc --port=8091

dev-biometric: ## Start biometric service independently
	cd inec-go-backend && go run ./cmd/biometric-svc --port=8092

dev-geo: ## Start geo service independently
	cd inec-go-backend && go run ./cmd/geo-svc --port=8093

dev-compliance: ## Start compliance service independently
	cd inec-go-backend && go run ./cmd/compliance-svc --port=8094

dev-ingestion: ## Start ingestion service independently
	cd inec-go-backend && go run ./cmd/ingestion-svc --port=8095

dev-bvas: ## Start BVAS service independently
	cd inec-go-backend && go run ./cmd/bvas-svc --port=8096

dev-middleware: ## Start middleware service independently
	cd inec-go-backend && go run ./cmd/middleware-svc --port=8085

dev-rust: ## Start Rust services
	cd services/inference-engine && cargo run &
	cd services/fluvio-stream && cargo run &

dev-python: ## Start Python services
	cd services/lakehouse-analytics && uvicorn main:app --port 8098 --reload &
	cd services/document-ai && uvicorn main:app --port 8099 --reload &

dev-frontend: ## Start frontend dev server
	cd inec-frontend && npm run dev

dev-mobile: ## Start mobile dev server
	cd inec-mobile && npx expo start

# --- Docker ---

docker-up: ## Start all services in Docker
	docker compose up -d

docker-down: ## Stop all Docker services
	docker compose down

docker-logs: ## Follow all service logs
	docker compose logs -f

docker-ps: ## Show service status
	docker compose ps

docker-rebuild: ## Rebuild and restart all services
	docker compose up -d --build

docker-build-svc: ## Build a single Go service image (usage: make docker-build-svc SVC=auth-svc)
	docker build --build-arg SERVICE_NAME=$(SVC) -f inec-go-backend/Dockerfile.service -t inec-$(SVC) inec-go-backend/

# --- Testing ---

test: test-go test-python test-mobile ## Run all tests

test-go: ## Run Go tests
	cd inec-go-backend && go test ./... -v -count=1

test-rust: ## Run Rust tests
	cd services/inference-engine && cargo test
	cd services/fluvio-stream && cargo test

test-python: ## Run Python tests
	cd services/lakehouse-analytics && python -m pytest
	cd services/document-ai && python -m pytest

test-mobile: ## Type-check mobile app
	cd inec-mobile && npx tsc --noEmit

test-frontend: ## Type-check and build frontend
	cd inec-frontend && npm run build

# --- Linting ---

lint: lint-go lint-python lint-frontend ## Run all linters

lint-go: ## Lint Go code
	cd inec-go-backend && go vet ./...

lint-python: ## Lint Python code
	cd services/lakehouse-analytics && ruff check .
	cd services/document-ai && ruff check .

lint-frontend: ## Lint frontend
	cd inec-frontend && npm run lint

# --- Build ---

build: build-go build-rust build-frontend ## Build all services

build-go: ## Build all Go binaries
	cd inec-go-backend && mkdir -p bin
	cd inec-go-backend && go build -o bin/inec-backend .
	cd inec-go-backend && go build -o bin/gateway ./cmd/gateway
	cd inec-go-backend && go build -o bin/auth-svc ./cmd/auth-svc
	cd inec-go-backend && go build -o bin/election-svc ./cmd/election-svc
	cd inec-go-backend && go build -o bin/biometric-svc ./cmd/biometric-svc
	cd inec-go-backend && go build -o bin/geo-svc ./cmd/geo-svc
	cd inec-go-backend && go build -o bin/compliance-svc ./cmd/compliance-svc
	cd inec-go-backend && go build -o bin/ingestion-svc ./cmd/ingestion-svc
	cd inec-go-backend && go build -o bin/bvas-svc ./cmd/bvas-svc
	cd inec-go-backend && go build -o bin/middleware-svc ./cmd/middleware-svc

build-rust: ## Build Rust services
	cd services/inference-engine && cargo build --release
	cd services/fluvio-stream && cargo build --release

build-frontend: ## Build frontend for production
	cd inec-frontend && npm run build

# --- Cleanup ---

clean: ## Remove build artifacts
	rm -rf inec-go-backend/bin/
	cd services/inference-engine && cargo clean
	cd services/fluvio-stream && cargo clean
