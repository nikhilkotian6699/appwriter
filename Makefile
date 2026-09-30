SHELL := /bin/bash
export PATH := $(HOME)/.docker/bin:$(PATH)
-include .env
export

.PHONY: help generate web-install web-build build test test-integration test-db docker-up docker-down fake dev-api dev-web

help: ## list targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

generate: ## regenerate the Go server interface, sqlc queries and the TypeScript client
	go tool oapi-codegen -config oapi-codegen.yaml api/openapi.yaml
	sqlc generate
	cd web && npm run generate

web-install: ## install frontend dependencies
	cd web && npm ci

web-build: ## build the frontend into web/dist (embedded by the Go binary)
	cd web && npm run build

build: web-build ## build both binaries into bin/
	go build -o bin/writersguild ./cmd/writersguild
	go build -o bin/fakegateway ./cmd/fakegateway

test: ## unit tests and frontend type check
	go test ./...
	cd web && npm run typecheck

test-integration: ## integration tests against TEST_DATABASE_URL (see .env)
	@test -n "$(TEST_DATABASE_URL)" || (echo "TEST_DATABASE_URL is not set" && exit 1)
	go test ./... -count=1

test-db: ## create the writersguild_test database in the compose Postgres
	docker compose exec -T db psql -U writersguild -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='writersguild_test'" | grep -q 1 \
		|| docker compose exec -T db createdb -U writersguild writersguild_test

docker-up: ## build and start app + db (add --profile fake for the fake gateway)
	docker compose up -d --build

docker-down: ## stop the containers
	docker compose down

fake: ## run the fake gateway locally on :4000
	go run ./cmd/fakegateway

dev-api: ## run the API locally against the compose database and .env
	go run ./cmd/writersguild

dev-web: ## run the Vite dev server (proxies /api to :8080)
	cd web && npm run dev
