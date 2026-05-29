#!/bin/bash
# retrain.sh - Nightly retraining cron script for GhostRoute bot detection model.
#
# Extracts training data from ClickHouse, runs training pipeline,
# and signals the FastAPI service to hot-swap the new model.
#
# Cron entry (run at 2 AM daily):
#   0 2 * * * /app/retrain.sh >> /var/log/retrain.log 2>&1

set -euo pipefail

CLICKHOUSE_URL="${CLICKHOUSE_URL:-http://clickhouse:8123}"
CLICKHOUSE_USER="${CLICKHOUSE_USER:-default}"
CLICKHOUSE_PASSWORD="${CLICKHOUSE_PASSWORD:-}"
MODELS_DIR="${MODELS_DIR:-/models}"
ML_SERVICE_URL="${ML_SERVICE_URL:-http://localhost:8000}"
DATA_DIR="/tmp/training_data"
VERSION=$(date -u +v%Y%m%d_%H%M%S)

echo "=========================================="
echo "Starting model retraining: ${VERSION}"
echo "Time: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "=========================================="

# Create temporary data directory
mkdir -p "${DATA_DIR}"

# Extract training data from ClickHouse
# Conversions are labeled as human (is_bot=0)
# Datacenter ASN hits are labeled as bot (is_bot=1)
echo "Extracting training data from ClickHouse..."

QUERY="SELECT
    toUnixTimestamp(event_time) as timestamp,
    visitor_ip,
    user_agent,
    campaign_id,
    bot_score,
    is_bot,
    ja3_hash as ja3_fingerprint,
    '[]' as recent_clicks,
    '{}' as fingerprint_data,
    '{}' as request_headers,
    '[]' as mouse_events,
    '{}' as js_timing
FROM visits
WHERE event_time >= now() - INTERVAL 30 DAY
FORMAT Parquet"

curl -s "${CLICKHOUSE_URL}" \
    --user "${CLICKHOUSE_USER}:${CLICKHOUSE_PASSWORD}" \
    --data-binary "${QUERY}" \
    -o "${DATA_DIR}/training_data.parquet"

ROWS=$(wc -c < "${DATA_DIR}/training_data.parquet")
if [ "${ROWS}" -lt 100 ]; then
    echo "ERROR: Training data too small (${ROWS} bytes). Aborting."
    rm -rf "${DATA_DIR}"
    exit 1
fi

echo "Training data extracted: ${ROWS} bytes"

# Run training pipeline
echo "Running training pipeline..."
python3 /app/train.py \
    --data "${DATA_DIR}/training_data.parquet" \
    --output "${MODELS_DIR}" \
    --version "${VERSION}"

# Verify model files exist
if [ ! -f "${MODELS_DIR}/${VERSION}/model.joblib" ]; then
    echo "ERROR: Model file not found after training. Aborting."
    rm -rf "${DATA_DIR}"
    exit 1
fi

echo "Model trained successfully: ${MODELS_DIR}/${VERSION}"

# Signal FastAPI to load the new model (hot-swap)
echo "Signaling model hot-swap..."
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
    -X POST "${ML_SERVICE_URL}/model/load?version_dir=${MODELS_DIR}/${VERSION}&slot=primary")

if [ "${HTTP_CODE}" -eq 200 ]; then
    echo "Model hot-swap successful"
else
    echo "WARNING: Model hot-swap failed (HTTP ${HTTP_CODE}). Service may need restart."
fi

# Cleanup old models (keep last 5 versions)
echo "Cleaning up old model versions..."
ls -dt "${MODELS_DIR}"/v* 2>/dev/null | tail -n +6 | xargs rm -rf 2>/dev/null || true

# Cleanup temporary data
rm -rf "${DATA_DIR}"

echo "=========================================="
echo "Retraining complete: ${VERSION}"
echo "Time: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "=========================================="
