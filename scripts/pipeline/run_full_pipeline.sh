#!/usr/bin/env bash
set -euo pipefail

DATASET="${1:-paramaggarwal/fashion-product-images-dataset}"
RAW_DIR="${RAW_DIR:-data/raw}"
PROCESSED_CSV="${PROCESSED_CSV:-data/processed/products.csv}"

./scripts/pipeline/download_kaggle.sh "${DATASET}" "${RAW_DIR}"
./scripts/pipeline/prepare_catalog.sh "${RAW_DIR}/fashion-product-images" "${PROCESSED_CSV}"
./scripts/pipeline/load_postgres.sh "${PROCESSED_CSV}"
./scripts/pipeline/load_qdrant.sh "${PROCESSED_CSV}"

echo "Pipeline completado. Datos en PostgreSQL y Qdrant."
