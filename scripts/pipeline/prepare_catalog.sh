#!/usr/bin/env bash
set -euo pipefail

RAW_DIR="${1:-data/raw/fashion-product-images}"
OUTPUT_CSV="${2:-data/processed/products.csv}"
IMAGE_URL_TEMPLATE="${IMAGE_URL_TEMPLATE:-https://cdn.local/stylerag/kaggle/images/%s.jpg}"

if [[ ! -d "${RAW_DIR}" ]]; then
  echo "Error: no existe directorio ${RAW_DIR}."
  exit 1
fi

STYLES_CSV="$(find "${RAW_DIR}" -type f -name 'styles.csv' | head -n 1)"
if [[ -z "${STYLES_CSV}" ]]; then
  echo "Error: no se encontro styles.csv en ${RAW_DIR}."
  exit 1
fi

mkdir -p "$(dirname "${OUTPUT_CSV}")"

echo "Normalizando ${STYLES_CSV} ..."
go run ./cmd/catalogprep \
  --input "${STYLES_CSV}" \
  --output "${OUTPUT_CSV}" \
  --image-url-template "${IMAGE_URL_TEMPLATE}"

echo "CSV normalizado en ${OUTPUT_CSV}"
