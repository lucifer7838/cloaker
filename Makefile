.PHONY: build up down migrate ingest-asn test lint train-ml test-ml docker-build-ml

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

train-ml:
	python3 ml/train.py --data $(DATA) --output $(OUTPUT)

test-ml:
	python3 -c "import ast; ast.parse(open('ml/app.py').read()); ast.parse(open('ml/train.py').read()); ast.parse(open('ml/consumer.py').read()); print('ML syntax OK')"

docker-build-ml:
	docker build -t ghostroute-ml ./ml
