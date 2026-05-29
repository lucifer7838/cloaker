.PHONY: build up down migrate ingest-asn test lint

build:
	go build -o bin/ghostroute ./cmd/ghostroute

up:
	docker compose up -d

down:
	docker compose down

migrate:
	@echo "Running database migrations..."
	@if [ -f scripts/migrate.sh ]; then bash scripts/migrate.sh; else echo "Migration script not found yet"; fi

ingest-asn:
	python3 scripts/ingest_asn.py

test:
	go test ./...

lint:
	go vet ./...
