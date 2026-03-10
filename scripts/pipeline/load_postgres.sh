#!/usr/bin/env bash
set -euo pipefail

INPUT_CSV="${1:-data/processed/products.csv}"
POSTGRES_SERVICE="${POSTGRES_SERVICE:-postgres}"
POSTGRES_DB="${POSTGRES_DB:-stylerag}"
POSTGRES_USER="${POSTGRES_USER:-stylerag}"

if [[ ! -f "${INPUT_CSV}" ]]; then
  echo "Error: no existe ${INPUT_CSV}. Ejecuta primero prepare_catalog.sh"
  exit 1
fi

if ! docker compose ps --status running "${POSTGRES_SERVICE}" >/dev/null 2>&1; then
  echo "Error: el servicio ${POSTGRES_SERVICE} no esta corriendo. Ejecuta: docker compose up -d"
  exit 1
fi

echo "Vaciando tabla products ..."
docker compose exec -T "${POSTGRES_SERVICE}" psql -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" \
  -c "TRUNCATE TABLE products;"

echo "Cargando ${INPUT_CSV} en PostgreSQL ..."
docker compose exec -T "${POSTGRES_SERVICE}" psql -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" \
  -c "\\copy products (id,source_id,name,description,category,subcategory,colors,fit,style_tags,price,currency,sizes,image_url,metadata) FROM STDIN WITH (FORMAT csv, HEADER true)" < "${INPUT_CSV}"

echo "Registros cargados:"
docker compose exec -T "${POSTGRES_SERVICE}" psql -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" \
  -c "SELECT COUNT(*) AS total_products FROM products;"
