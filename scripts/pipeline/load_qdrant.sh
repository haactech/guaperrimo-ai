#!/usr/bin/env bash
set -euo pipefail

INPUT_CSV="${1:-data/processed/products.csv}"
QDRANT_URL="${QDRANT_URL:-http://localhost:6333}"
QDRANT_COLLECTION="${QDRANT_COLLECTION:-products}"
QDRANT_VECTOR_SIZE="${QDRANT_VECTOR_SIZE:-64}"
QDRANT_BATCH_SIZE="${QDRANT_BATCH_SIZE:-256}"

if [[ ! -f "${INPUT_CSV}" ]]; then
  echo "Error: no existe ${INPUT_CSV}. Ejecuta primero prepare_catalog.sh"
  exit 1
fi

echo "Inyectando embeddings en Qdrant (${QDRANT_URL}, collection=${QDRANT_COLLECTION}) ..."
go run ./cmd/qdrantseed \
  --input "${INPUT_CSV}" \
  --qdrant-url "${QDRANT_URL}" \
  --collection "${QDRANT_COLLECTION}" \
  --vector-size "${QDRANT_VECTOR_SIZE}" \
  --batch-size "${QDRANT_BATCH_SIZE}" \
  --recreate
