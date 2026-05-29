# SKILL: Docker Compose Health Checks for GhostRoute Stack

## Overview

Complete Docker Compose configuration for the GhostRoute cloaking infrastructure with
health checks, dependency ordering, resource limits, and production-ready configuration
for all services in the stack.

## Complete docker-compose.yml

```yaml
version: "3.8"

services:
  # =============================================================================
  # NGINX - Reverse proxy with JA3 TLS fingerprinting
  # =============================================================================
  nginx:
    build:
      context: ./nginx
      dockerfile: Dockerfile
    container_name: ghostroute-nginx
    ports:
      - "443:443"
      - "80:80"
    volumes:
      - ./nginx/conf.d:/etc/nginx/conf.d:ro
      - ./nginx/ssl:/etc/nginx/ssl:ro
      - nginx-cache:/var/cache/nginx
    networks:
      - frontend
      - backend
    depends_on:
      go-api:
        condition: service_healthy
      python-ml:
        condition: service_healthy
      php-legacy:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "curl", "-f", "-k", "https://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 15s
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "2.0"
          memory: 256M
        reservations:
          cpus: "0.5"
          memory: 128M
    logging:
      driver: json-file
      options:
        max-size: "50m"
        max-file: "5"
    environment:
      - TZ=UTC

  # =============================================================================
  # GO API - Main GhostRoute decision engine
  # =============================================================================
  go-api:
    build:
      context: ./cmd/ghostroute
      dockerfile: Dockerfile
    container_name: ghostroute-api
    expose:
      - "8080"
    volumes:
      - ./config:/app/config:ro
    networks:
      - frontend
      - backend
    depends_on:
      redis:
        condition: service_healthy
      clickhouse:
        condition: service_healthy
      rabbitmq:
        condition: service_healthy
      qdrant:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/healthz"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 10s
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "2.0"
          memory: 512M
        reservations:
          cpus: "0.5"
          memory: 256M
    logging:
      driver: json-file
      options:
        max-size: "100m"
        max-file: "5"
    env_file:
      - .env
    environment:
      - REDIS_URL=redis://redis:6379/0
      - CLICKHOUSE_URL=http://clickhouse:8123
      - RABBITMQ_URL=amqp://ghostroute:${RABBITMQ_PASSWORD}@rabbitmq:5672/ghostroute
      - QDRANT_URL=http://qdrant:6333
      - ML_SERVICE_URL=http://python-ml:8000
      - LOG_LEVEL=info
      - BOT_THRESHOLD=0.7

  # =============================================================================
  # PYTHON ML - XGBoost bot detection model serving
  # =============================================================================
  python-ml:
    build:
      context: ./ml
      dockerfile: Dockerfile.serve
    container_name: ghostroute-ml
    expose:
      - "8000"
    volumes:
      - ml-models:/models
    networks:
      - frontend
      - backend
    depends_on:
      rabbitmq:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8000/health"]
      interval: 15s
      timeout: 5s
      retries: 3
      start_period: 30s
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "2.0"
          memory: 2G
        reservations:
          cpus: "1.0"
          memory: 1G
    logging:
      driver: json-file
      options:
        max-size: "50m"
        max-file: "3"
    environment:
      - MODELS_DIR=/models
      - MODEL_BUCKET=ghostroute-models
      - MODEL_PREFIX=bot-detection/
      - REFRESH_INTERVAL=300
      - WORKERS=4

  # =============================================================================
  # CLICKHOUSE - Analytics and click event storage
  # =============================================================================
  clickhouse:
    image: clickhouse/clickhouse-server:24.1
    container_name: ghostroute-clickhouse
    expose:
      - "8123"
      - "9000"
    volumes:
      - clickhouse-data:/var/lib/clickhouse
      - clickhouse-logs:/var/log/clickhouse-server
      - ./clickhouse/config.xml:/etc/clickhouse-server/config.d/custom.xml:ro
      - ./clickhouse/users.xml:/etc/clickhouse-server/users.d/custom.xml:ro
      - ./clickhouse/init:/docker-entrypoint-initdb.d:ro
    networks:
      - backend
    healthcheck:
      test: ["CMD", "bash", "-c", "/opt/healthcheck/clickhouse-healthcheck.sh"]
      interval: 15s
      timeout: 10s
      retries: 5
      start_period: 30s
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "4.0"
          memory: 4G
        reservations:
          cpus: "1.0"
          memory: 2G
    logging:
      driver: json-file
      options:
        max-size: "100m"
        max-file: "5"
    environment:
      - CLICKHOUSE_DB=ghostroute
      - CLICKHOUSE_USER=ghostroute
      - CLICKHOUSE_PASSWORD=${CLICKHOUSE_PASSWORD}
      - CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1
    ulimits:
      nofile:
        soft: 262144
        hard: 262144

  # =============================================================================
  # REDIS - IP lookup cache and session store
  # =============================================================================
  redis:
    image: redis:7.2-alpine
    container_name: ghostroute-redis
    expose:
      - "6379"
    volumes:
      - redis-data:/data
      - ./redis/redis.conf:/usr/local/etc/redis/redis.conf:ro
    networks:
      - backend
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 3s
      retries: 3
      start_period: 5s
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "1.0"
          memory: 1G
        reservations:
          cpus: "0.25"
          memory: 256M
    logging:
      driver: json-file
      options:
        max-size: "20m"
        max-file: "3"
    command: redis-server /usr/local/etc/redis/redis.conf
    sysctls:
      - net.core.somaxconn=511

  # =============================================================================
  # QDRANT - Fingerprint vector similarity search
  # =============================================================================
  qdrant:
    image: qdrant/qdrant:v1.7.4
    container_name: ghostroute-qdrant
    expose:
      - "6333"
      - "6334"
    volumes:
      - qdrant-data:/qdrant/storage
      - ./qdrant/config.yaml:/qdrant/config/production.yaml:ro
    networks:
      - backend
    healthcheck:
      test: ["CMD", "bash", "-c", "/opt/healthcheck/qdrant-healthcheck.sh"]
      interval: 15s
      timeout: 5s
      retries: 3
      start_period: 20s
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "2.0"
          memory: 2G
        reservations:
          cpus: "0.5"
          memory: 1G
    logging:
      driver: json-file
      options:
        max-size: "50m"
        max-file: "3"
    environment:
      - QDRANT__SERVICE__GRPC_PORT=6334
      - QDRANT__SERVICE__HTTP_PORT=6333

  # =============================================================================
  # RABBITMQ - Event queue for async processing
  # =============================================================================
  rabbitmq:
    image: rabbitmq:3.13-management-alpine
    container_name: ghostroute-rabbitmq
    expose:
      - "5672"
      - "15672"
    volumes:
      - rabbitmq-data:/var/lib/rabbitmq
      - ./rabbitmq/definitions.json:/etc/rabbitmq/definitions.json:ro
      - ./rabbitmq/rabbitmq.conf:/etc/rabbitmq/rabbitmq.conf:ro
    networks:
      - backend
    healthcheck:
      test: ["CMD", "bash", "-c", "/opt/healthcheck/rabbitmq-healthcheck.sh"]
      interval: 15s
      timeout: 10s
      retries: 5
      start_period: 30s
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "1.0"
          memory: 512M
        reservations:
          cpus: "0.25"
          memory: 256M
    logging:
      driver: json-file
      options:
        max-size: "50m"
        max-file: "3"
    environment:
      - RABBITMQ_DEFAULT_USER=ghostroute
      - RABBITMQ_DEFAULT_PASS=${RABBITMQ_PASSWORD}
      - RABBITMQ_DEFAULT_VHOST=ghostroute

  # =============================================================================
  # PHP LEGACY - YellowTDS compatibility layer
  # =============================================================================
  php-legacy:
    build:
      context: ./php
      dockerfile: Dockerfile
    container_name: ghostroute-php
    expose:
      - "9000"
    volumes:
      - ./php/src:/var/www/html:ro
      - php-cache:/var/cache/ghostroute
    networks:
      - frontend
      - backend
    depends_on:
      redis:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9000/status.php"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 10s
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "1.0"
          memory: 512M
        reservations:
          cpus: "0.25"
          memory: 128M
    logging:
      driver: json-file
      options:
        max-size: "50m"
        max-file: "3"
    environment:
      - REDIS_HOST=redis
      - REDIS_PORT=6379
      - APP_ENV=production
      - BOT_THRESHOLD=0.7

# =============================================================================
# NETWORKS
# =============================================================================
networks:
  frontend:
    driver: bridge
    ipam:
      config:
        - subnet: 172.20.0.0/24
  backend:
    driver: bridge
    internal: true
    ipam:
      config:
        - subnet: 172.20.1.0/24
  monitoring:
    driver: bridge
    ipam:
      config:
        - subnet: 172.20.2.0/24

# =============================================================================
# VOLUMES
# =============================================================================
volumes:
  nginx-cache:
    driver: local
  clickhouse-data:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: /data/clickhouse
  clickhouse-logs:
    driver: local
  redis-data:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: /data/redis
  qdrant-data:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: /data/qdrant
  rabbitmq-data:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: /data/rabbitmq
  ml-models:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: /data/ml-models
  php-cache:
    driver: local
```


## Environment Variables (.env)

```bash
# .env - Environment configuration for GhostRoute stack
# Copy to .env and fill in production values

# =============================================================================
# Database Credentials
# =============================================================================
CLICKHOUSE_PASSWORD=ch_ghostroute_s3cur3_2025
RABBITMQ_PASSWORD=rmq_ghostroute_pr0d_2025
REDIS_PASSWORD=redis_ghostroute_2025

# =============================================================================
# API Configuration
# =============================================================================
API_SECRET_KEY=gr_api_key_change_in_production
BOT_THRESHOLD=0.7
LOG_LEVEL=info

# =============================================================================
# ML Service
# =============================================================================
ML_MODEL_VERSION=latest
ML_REFRESH_INTERVAL=300
MODEL_BUCKET=ghostroute-models
AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
AWS_REGION=us-east-1

# =============================================================================
# SSL/TLS
# =============================================================================
SSL_CERT_PATH=/etc/nginx/ssl/cert.pem
SSL_KEY_PATH=/etc/nginx/ssl/key.pem

# =============================================================================
# External Services (optional)
# =============================================================================
MAXMIND_LICENSE_KEY=your_maxmind_key_here
IPINFO_TOKEN=your_ipinfo_token_here

# =============================================================================
# Monitoring (optional, for docker-compose.monitoring.yml)
# =============================================================================
GRAFANA_ADMIN_PASSWORD=admin_change_me
PROMETHEUS_RETENTION=30d
```

## Health Check Scripts

### clickhouse-healthcheck.sh

```bash
#!/bin/bash
# Health check script for ClickHouse
# Checks both TCP connectivity and query readiness

set -e

# Check 1: TCP port is listening
if ! nc -z localhost 9000 2>/dev/null; then
    echo "ClickHouse native port 9000 not responding"
    exit 1
fi

# Check 2: HTTP interface responds
if ! curl -sf http://localhost:8123/ping >/dev/null 2>&1; then
    echo "ClickHouse HTTP ping failed"
    exit 1
fi

# Check 3: Can execute a query (proves the server is actually ready)
RESULT=$(clickhouse-client --query "SELECT 1" 2>/dev/null)
if [ "$RESULT" != "1" ]; then
    echo "ClickHouse query execution failed"
    exit 1
fi

# Check 4: Verify the ghostroute database exists (after init)
DB_EXISTS=$(clickhouse-client --query "SELECT count() FROM system.databases WHERE name = 'ghostroute'" 2>/dev/null)
if [ "$DB_EXISTS" != "1" ]; then
    echo "Database ghostroute not found (may still be initializing)"
    exit 1
fi

# Check 5: Verify no stuck mutations or merges blocking queries
STUCK_MUTATIONS=$(clickhouse-client --query "SELECT count() FROM system.mutations WHERE is_done = 0 AND create_time < now() - INTERVAL 5 MINUTE" 2>/dev/null)
if [ "$STUCK_MUTATIONS" -gt "0" ] 2>/dev/null; then
    echo "Warning: $STUCK_MUTATIONS stuck mutations detected"
    # Do not fail health check for this, just warn
fi

echo "ClickHouse is healthy"
exit 0
```

### rabbitmq-healthcheck.sh

```bash
#!/bin/bash
# Health check script for RabbitMQ
# Checks both the node and critical queue availability

set -e

# Check 1: Node is running
if ! rabbitmq-diagnostics -q check_running 2>/dev/null; then
    echo "RabbitMQ node is not running"
    exit 1
fi

# Check 2: Port is responsive
if ! rabbitmq-diagnostics -q check_port_connectivity 2>/dev/null; then
    echo "RabbitMQ port connectivity failed"
    exit 1
fi

# Check 3: Virtual host exists
if ! rabbitmqctl list_vhosts --quiet 2>/dev/null | grep -q "ghostroute"; then
    echo "Virtual host ghostroute not found"
    exit 1
fi

# Check 4: Critical queues exist (after initialization)
REQUIRED_QUEUES=("click_events" "bot_verdicts" "fingerprint_updates")
for queue in "${REQUIRED_QUEUES[@]}"; do
    EXISTS=$(rabbitmqctl list_queues -p ghostroute name --quiet 2>/dev/null | grep -c "^$queue$" || true)
    if [ "$EXISTS" -eq "0" ]; then
        echo "Required queue ''$queue'' not found in vhost ghostroute"
        exit 1
    fi
done

# Check 5: Memory alarm is not triggered
if rabbitmq-diagnostics -q check_alarms 2>&1 | grep -q "memory"; then
    echo "RabbitMQ memory alarm triggered"
    exit 1
fi

echo "RabbitMQ is healthy"
exit 0
```

### qdrant-healthcheck.sh

```bash
#!/bin/bash
# Health check script for Qdrant vector database
# Checks health endpoint and collection availability

set -e

# Check 1: HTTP health endpoint
HEALTH_STATUS=$(curl -sf http://localhost:6333/readyz 2>/dev/null)
if [ $? -ne 0 ]; then
    echo "Qdrant HTTP health check failed"
    exit 1
fi

# Check 2: gRPC port is listening
if ! nc -z localhost 6334 2>/dev/null; then
    echo "Qdrant gRPC port 6334 not responding"
    exit 1
fi

# Check 3: Required collection exists and is loaded
COLLECTION_INFO=$(curl -sf http://localhost:6333/collections/fingerprints 2>/dev/null)
if [ $? -ne 0 ]; then
    echo "Collection fingerprints not found"
    exit 1
fi

# Check collection status is "green"
STATUS=$(echo "$COLLECTION_INFO" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('result',{}).get('status',''))" 2>/dev/null || echo "")
if [ "$STATUS" != "green" ]; then
    echo "Collection fingerprints status is $STATUS (expected green)"
    # Allow "yellow" during indexing, fail on "red"
    if [ "$STATUS" == "red" ]; then
        exit 1
    fi
fi

# Check 4: Can perform a basic search (proves indexing is working)
SEARCH_RESULT=$(curl -sf -X POST http://localhost:6333/collections/fingerprints/points/search     -H "Content-Type: application/json"     -d '{"vector": [0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0], "limit": 1}' 2>/dev/null)
if [ $? -ne 0 ]; then
    echo "Qdrant search test failed"
    exit 1
fi

echo "Qdrant is healthy"
exit 0
```

## Health Check Timing Parameters Reference

| Service      | Interval | Timeout | Retries | Start Period | Rationale                                    |
|-------------|----------|---------|---------|--------------|----------------------------------------------|
| nginx       | 10s      | 5s      | 3       | 15s          | Fast startup, critical path                  |
| go-api      | 10s      | 5s      | 3       | 10s          | Go binary starts fast, needs deps ready      |
| python-ml   | 15s      | 5s      | 3       | 30s          | Model loading takes time on startup          |
| clickhouse  | 15s      | 10s     | 5       | 30s          | Large data dirs need time to initialize      |
| redis       | 10s      | 3s      | 3       | 5s           | Very fast startup, simple ping check         |
| qdrant      | 15s      | 5s      | 3       | 20s          | Collection loading depends on data size      |
| rabbitmq    | 15s      | 10s     | 5       | 30s          | Erlang VM boot + plugin initialization       |
| php-legacy  | 10s      | 5s      | 3       | 10s          | PHP-FPM pool starts quickly                  |

## Dependency Graph

```
                    +--------+
                    | nginx  |
                    +--------+
                   /    |                       /     |                +------+  +--------+  +-----------+
          |go-api|  |python-ml|  |php-legacy|
          +------+  +--------+  +-----------+
         /  |  |  \      |            |
        /   |  |   \     |            |
+-----+ +--+ +---+ +---+ +--------+  |
|redis| |CH| |RMQ| |QDR| |rabbitmq|  |
+-----+ +--+ +---+ +---+ +--------+  |
   ^                                  |
   +----------------------------------+
```

Legend:
- CH = ClickHouse
- RMQ = RabbitMQ  
- QDR = Qdrant
- All arrows point from dependent to dependency ("depends on")


## Production Override (docker-compose.override.yml)

```yaml
# docker-compose.override.yml - Production overrides
# Applied automatically when running docker compose up

version: "3.8"

services:
  nginx:
    ports:
      - "443:443"
      - "80:80"
    environment:
      - NGINX_WORKER_PROCESSES=auto
      - NGINX_WORKER_CONNECTIONS=4096

  go-api:
    deploy:
      replicas: 2
      resources:
        limits:
          cpus: "4.0"
          memory: 1G
    environment:
      - GOMAXPROCS=4
      - LOG_LEVEL=warn

  python-ml:
    deploy:
      replicas: 2
      resources:
        limits:
          cpus: "4.0"
          memory: 4G
    environment:
      - WORKERS=8

  clickhouse:
    deploy:
      resources:
        limits:
          cpus: "8.0"
          memory: 8G
    environment:
      - CLICKHOUSE_MAX_MEMORY_USAGE=6000000000
      - CLICKHOUSE_MAX_THREADS=8

  redis:
    command: redis-server /usr/local/etc/redis/redis.conf --maxmemory 900mb --maxmemory-policy allkeys-lru
    deploy:
      resources:
        limits:
          memory: 1G

  qdrant:
    deploy:
      resources:
        limits:
          cpus: "4.0"
          memory: 4G
```

## Development Override (docker-compose.dev.yml)

```yaml
# docker-compose.dev.yml - Development environment
# Usage: docker compose -f docker-compose.yml -f docker-compose.dev.yml up

version: "3.8"

services:
  nginx:
    ports:
      - "8443:443"
      - "8080:80"
    volumes:
      - ./nginx/conf.d:/etc/nginx/conf.d:ro  # Hot reload config
    environment:
      - NGINX_WORKER_PROCESSES=1

  go-api:
    build:
      context: ./cmd/ghostroute
      dockerfile: Dockerfile.dev
    volumes:
      - ./cmd/ghostroute:/app  # Live reload with air
      - go-modules:/go/pkg/mod
    ports:
      - "8081:8080"  # Direct access for debugging
      - "2345:2345"  # Delve debugger
    environment:
      - LOG_LEVEL=debug
      - ENABLE_PPROF=true
    deploy:
      resources:
        limits:
          cpus: "1.0"
          memory: 256M

  python-ml:
    build:
      context: ./ml
      dockerfile: Dockerfile.dev
    volumes:
      - ./ml:/app
      - ./models:/models
    ports:
      - "8000:8000"  # Direct access
    environment:
      - WORKERS=1
      - RELOAD=true
    deploy:
      resources:
        limits:
          memory: 1G

  clickhouse:
    ports:
      - "8123:8123"  # HTTP interface
      - "9000:9000"  # Native protocol
    deploy:
      resources:
        limits:
          cpus: "2.0"
          memory: 2G

  redis:
    ports:
      - "6379:6379"  # Direct access for redis-cli
    command: redis-server --save "" --appendonly no  # No persistence in dev
    deploy:
      resources:
        limits:
          memory: 256M

  qdrant:
    ports:
      - "6333:6333"  # REST API
      - "6334:6334"  # gRPC
    deploy:
      resources:
        limits:
          memory: 512M

  rabbitmq:
    ports:
      - "5672:5672"   # AMQP
      - "15672:15672" # Management UI
    deploy:
      resources:
        limits:
          memory: 256M

  php-legacy:
    volumes:
      - ./php/src:/var/www/html  # Live editing
    environment:
      - APP_ENV=development
      - DISPLAY_ERRORS=1

volumes:
  go-modules:
    driver: local
```

## Logging Configuration

### JSON-File Driver Settings

```yaml
# Applied per-service in docker-compose.yml
logging:
  driver: json-file
  options:
    max-size: "50m"     # Max size per log file
    max-file: "5"       # Number of rotated files to keep
    compress: "true"    # Compress rotated files
    labels: "service"   # Include container labels in log
    tag: "{{.Name}}"    # Tag format for log aggregation
```

### Recommended Log Sizes by Service

| Service     | max-size | max-file | Total Max | Rationale                          |
|-------------|----------|----------|-----------|-------------------------------------|
| nginx       | 50m      | 5        | 250 MB    | High request volume, access logs    |
| go-api      | 100m     | 5        | 500 MB    | Decision logs, structured JSON      |
| python-ml   | 50m      | 3        | 150 MB    | Prediction logs, model events       |
| clickhouse  | 100m     | 5        | 500 MB    | Query logs, merge events            |
| redis       | 20m      | 3        | 60 MB     | Minimal logging                     |
| qdrant      | 50m      | 3        | 150 MB    | Search/index logs                   |
| rabbitmq    | 50m      | 3        | 150 MB    | Connection/queue events             |
| php-legacy  | 50m      | 3        | 150 MB    | Request processing logs             |

## Network Configuration Details

```yaml
networks:
  # Frontend network: services that receive external traffic
  # nginx, go-api, python-ml, php-legacy
  frontend:
    driver: bridge
    ipam:
      config:
        - subnet: 172.20.0.0/24
          gateway: 172.20.0.1

  # Backend network: internal service-to-service communication
  # All services connected here. Marked as internal (no external access)
  backend:
    driver: bridge
    internal: true  # No external connectivity
    ipam:
      config:
        - subnet: 172.20.1.0/24
          gateway: 172.20.1.1

  # Monitoring network: for Prometheus/Grafana (separate overlay)
  # Only monitoring tools and services exposing /metrics endpoints
  monitoring:
    driver: bridge
    ipam:
      config:
        - subnet: 172.20.2.0/24
          gateway: 172.20.2.1
```

### Network Security Rules

- **backend** network is  which means no direct internet access from containers on this network
- Services needing external access (S3 for model downloads) must also be on the **frontend** network
- Database services (ClickHouse, Redis, Qdrant, RabbitMQ) are ONLY on **backend**
- nginx is the only service with published ports to the host

## Makefile

```makefile
# Makefile for GhostRoute Docker Compose management

.PHONY: up down logs restart rebuild status health clean
.DEFAULT_GOAL := help

COMPOSE_FILE := docker-compose.yml
DEV_COMPOSE := docker-compose.dev.yml
PROD_COMPOSE := docker-compose.override.yml

# Colors for output
GREEN  := \033[0;32m
YELLOW := \033[0;33m
RED    := \033[0;31m
NC     := \033[0m

## help: Show this help message
help:
	@echo "GhostRoute Docker Compose Commands"
	@echo "====================================="
	@grep -E '^## ' Makefile | sed 's/## //'

## up: Start all services (production)
up:
	@echo "$(GREEN)Starting GhostRoute stack...$(NC)"
	docker compose -f $(COMPOSE_FILE) up -d
	@echo "$(GREEN)Stack started. Run 'make health' to check status.$(NC)"

## up-dev: Start all services (development)
up-dev:
	@echo "$(GREEN)Starting GhostRoute stack (development)...$(NC)"
	docker compose -f $(COMPOSE_FILE) -f $(DEV_COMPOSE) up -d
	@echo "$(GREEN)Dev stack started.$(NC)"

## down: Stop all services
down:
	@echo "$(YELLOW)Stopping GhostRoute stack...$(NC)"
	docker compose -f $(COMPOSE_FILE) down
	@echo "$(GREEN)Stack stopped.$(NC)"

## down-volumes: Stop all services and remove volumes (DATA LOSS!)
down-volumes:
	@echo "$(RED)WARNING: This will delete all data volumes!$(NC)"
	@read -p "Are you sure? [y/N] " confirm && [ "$$confirm" = "y" ] || exit 1
	docker compose -f $(COMPOSE_FILE) down -v
	@echo "$(GREEN)Stack stopped and volumes removed.$(NC)"

## logs: Tail logs for all services
logs:
	docker compose -f $(COMPOSE_FILE) logs -f --tail=100

## logs-service: Tail logs for a specific service (usage: make logs-service SVC=go-api)
logs-service:
	@test -n "$(SVC)" || (echo "Usage: make logs-service SVC=<service-name>" && exit 1)
	docker compose -f $(COMPOSE_FILE) logs -f --tail=200 $(SVC)

## restart: Restart a specific service (usage: make restart SVC=go-api)
restart:
	@test -n "$(SVC)" || (echo "Usage: make restart SVC=<service-name>" && exit 1)
	@echo "$(YELLOW)Restarting $(SVC)...$(NC)"
	docker compose -f $(COMPOSE_FILE) restart $(SVC)

## rebuild: Rebuild and restart a specific service
rebuild:
	@test -n "$(SVC)" || (echo "Usage: make rebuild SVC=<service-name>" && exit 1)
	@echo "$(YELLOW)Rebuilding $(SVC)...$(NC)"
	docker compose -f $(COMPOSE_FILE) up -d --build --force-recreate $(SVC)

## rebuild-all: Rebuild all custom images and restart
rebuild-all:
	@echo "$(YELLOW)Rebuilding all services...$(NC)"
	docker compose -f $(COMPOSE_FILE) build --no-cache
	docker compose -f $(COMPOSE_FILE) up -d --force-recreate

## status: Show service status
status:
	docker compose -f $(COMPOSE_FILE) ps -a

## health: Check health of all services
health:
	@echo "Service Health Status:"
	@echo "====================="
	@docker compose -f $(COMPOSE_FILE) ps --format "table {{.Name}}\t{{.Status}}\t{{.Ports}}"

## health-detail: Detailed health check for each service
health-detail:
	@echo "Detailed Health Checks:"
	@echo "======================="
	@echo "\n--- nginx ---"
	@docker inspect --format='{{.State.Health.Status}}' ghostroute-nginx 2>/dev/null || echo "not running"
	@echo "\n--- go-api ---"
	@docker inspect --format='{{.State.Health.Status}}' ghostroute-api 2>/dev/null || echo "not running"
	@echo "\n--- python-ml ---"
	@docker inspect --format='{{.State.Health.Status}}' ghostroute-ml 2>/dev/null || echo "not running"
	@echo "\n--- clickhouse ---"
	@docker inspect --format='{{.State.Health.Status}}' ghostroute-clickhouse 2>/dev/null || echo "not running"
	@echo "\n--- redis ---"
	@docker inspect --format='{{.State.Health.Status}}' ghostroute-redis 2>/dev/null || echo "not running"
	@echo "\n--- qdrant ---"
	@docker inspect --format='{{.State.Health.Status}}' ghostroute-qdrant 2>/dev/null || echo "not running"
	@echo "\n--- rabbitmq ---"
	@docker inspect --format='{{.State.Health.Status}}' ghostroute-rabbitmq 2>/dev/null || echo "not running"
	@echo "\n--- php-legacy ---"
	@docker inspect --format='{{.State.Health.Status}}' ghostroute-php 2>/dev/null || echo "not running"

## shell: Open a shell in a service container (usage: make shell SVC=go-api)
shell:
	@test -n "$(SVC)" || (echo "Usage: make shell SVC=<service-name>" && exit 1)
	docker compose -f $(COMPOSE_FILE) exec $(SVC) sh

## clean: Remove stopped containers, dangling images, and build cache
clean:
	@echo "$(YELLOW)Cleaning up...$(NC)"
	docker compose -f $(COMPOSE_FILE) down --remove-orphans
	docker system prune -f
	@echo "$(GREEN)Cleanup complete.$(NC)"

## backup-volumes: Backup all data volumes
backup-volumes:
	@echo "$(GREEN)Backing up volumes...$(NC)"
	@mkdir -p ./backups/$$(date +%Y%m%d)
	docker run --rm -v clickhouse-data:/data -v ./backups/$$(date +%Y%m%d):/backup alpine tar czf /backup/clickhouse-data.tar.gz -C /data .
	docker run --rm -v redis-data:/data -v ./backups/$$(date +%Y%m%d):/backup alpine tar czf /backup/redis-data.tar.gz -C /data .
	docker run --rm -v qdrant-data:/data -v ./backups/$$(date +%Y%m%d):/backup alpine tar czf /backup/qdrant-data.tar.gz -C /data .
	docker run --rm -v rabbitmq-data:/data -v ./backups/$$(date +%Y%m%d):/backup alpine tar czf /backup/rabbitmq-data.tar.gz -C /data .
	@echo "$(GREEN)Backup complete: ./backups/$$(date +%Y%m%d)/$(NC)"
```

## Restart Policies

| Policy          | Behavior                                                    | Use Case                        |
|-----------------|------------------------------------------------------------|---------------------------------|
| no              | Never restart                                               | One-off tasks, debugging        |
| always          | Always restart, even if manually stopped                    | Never (use unless-stopped)      |
| on-failure      | Restart only on non-zero exit code                         | Batch jobs, migrations          |
| unless-stopped  | Restart unless explicitly stopped with docker compose stop  | All production services         |

All GhostRoute services use `unless-stopped` because:
- Services should recover from crashes automatically
- Manual stops (for maintenance) should be respected
- On host reboot, services resume automatically via Docker daemon

## Resource Limits Summary

| Service      | CPU Limit | Memory Limit | CPU Reserve | Memory Reserve | Notes                           |
|-------------|-----------|-------------|-------------|----------------|----------------------------------|
| nginx       | 2.0       | 256 MB      | 0.5         | 128 MB         | Proxy only, low memory needs     |
| go-api      | 2.0       | 512 MB      | 0.5         | 256 MB         | Scales horizontally              |
| python-ml   | 2.0       | 2 GB        | 1.0         | 1 GB           | Model in memory + numpy arrays   |
| clickhouse  | 4.0       | 4 GB        | 1.0         | 2 GB           | Query processing, merges         |
| redis       | 1.0       | 1 GB        | 0.25        | 256 MB         | In-memory data store             |
| qdrant      | 2.0       | 2 GB        | 0.5         | 1 GB           | Vector index in memory           |
| rabbitmq    | 1.0       | 512 MB      | 0.25        | 256 MB         | Message broker, queues           |
| php-legacy  | 1.0       | 512 MB      | 0.25        | 128 MB         | PHP-FPM worker pool              |
| **Total**   | **15.0**  | **11 GB**   | **4.25**    | **5 GB**       | Minimum 16-core, 16 GB server   |

## Startup Order Verification

```bash
#!/bin/bash
# verify-startup.sh - Verify all services start correctly in order

set -e

echo "Starting GhostRoute stack with startup verification..."
echo "======================================================"

# Start infrastructure services first
echo "[1/4] Starting infrastructure (redis, clickhouse, qdrant, rabbitmq)..."
docker compose up -d redis clickhouse qdrant rabbitmq

# Wait for infrastructure to be healthy
echo "Waiting for infrastructure health checks..."
services=("ghostroute-redis" "ghostroute-clickhouse" "ghostroute-qdrant" "ghostroute-rabbitmq")
for svc in ""; do
    echo -n "  Waiting for ..."
    timeout 120 bash -c "until [ "\" == "healthy" ]; do sleep 2; done"
    echo " healthy"
done

# Start application services
echo "[2/4] Starting application services (go-api, python-ml, php-legacy)..."
docker compose up -d go-api python-ml php-legacy

# Wait for application services
echo "Waiting for application health checks..."
app_services=("ghostroute-api" "ghostroute-ml" "ghostroute-php")
for svc in ""; do
    echo -n "  Waiting for ..."
    timeout 60 bash -c "until [ "\" == "healthy" ]; do sleep 2; done"
    echo " healthy"
done

# Start edge proxy
echo "[3/4] Starting nginx..."
docker compose up -d nginx

echo -n "  Waiting for ghostroute-nginx..."
timeout 30 bash -c "until [ "\" == "healthy" ]; do sleep 2; done"
echo " healthy"

# Final verification
echo "[4/4] Final verification..."
echo ""
docker compose ps --format "table {{.Name}}	{{.Status}}"
echo ""
echo "All services are healthy. GhostRoute is ready."
```

## Troubleshooting Common Issues

### Service Fails Health Check

```bash
# Check health check logs
docker inspect --format='{{range .State.Health.Log}}{{.Output}}{{end}}' <container_name>

# Check last 5 health check results
docker inspect --format='{{json .State.Health}}' <container_name> | python3 -m json.tool

# Run health check manually inside container
docker exec ghostroute-clickhouse bash -c "/opt/healthcheck/clickhouse-healthcheck.sh"
```

### Dependency Deadlock

If services fail because their dependencies never become healthy:

```bash
# Check which services are waiting
docker compose ps --format "table {{.Name}}	{{.Status}}" | grep -v "healthy"

# Force start without health check waiting (debugging only)
docker compose up -d --no-deps <service_name>

# Check health check configuration
docker inspect --format='{{json .Config.Healthcheck}}' <container_name> | python3 -m json.tool
```

### Out of Memory (OOM) Kills

```bash
# Check for OOM events
docker events --filter event=oom --since 1h

# Check current memory usage vs limits
docker stats --no-stream --format "table {{.Name}}	{{.MemUsage}}	{{.MemPerc}}"

# Increase limit for specific service
# Edit docker-compose.yml deploy.resources.limits.memory, then:
docker compose up -d <service_name>
```
