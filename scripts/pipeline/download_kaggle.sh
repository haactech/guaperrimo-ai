#!/usr/bin/env bash
set -euo pipefail

DATASET="${1:-paramaggarwal/fashion-product-images-dataset}"
OUT_DIR="${2:-data/raw}"
TARGET_DIR="${OUT_DIR}/fashion-product-images"

if ! command -v kaggle >/dev/null 2>&1; then
  echo "Error: kaggle CLI no esta instalado. Instala con: pip install kaggle"
  exit 1
fi

if [[ -z "${KAGGLE_USERNAME:-}" || -z "${KAGGLE_KEY:-}" ]]; then
  echo "Error: define KAGGLE_USERNAME y KAGGLE_KEY en tu entorno (.env)."
  exit 1
fi

mkdir -p "${OUT_DIR}"

echo "Descargando dataset ${DATASET} ..."
kaggle datasets download -d "${DATASET}" -p "${OUT_DIR}" --force

ZIP_FILE="$(ls -1t "${OUT_DIR}"/*.zip | head -n 1)"
if [[ -z "${ZIP_FILE}" ]]; then
  echo "Error: no se encontro archivo .zip en ${OUT_DIR}"
  exit 1
fi

rm -rf "${TARGET_DIR}"
mkdir -p "${TARGET_DIR}"

echo "Extrayendo ${ZIP_FILE} ..."
unzip -o "${ZIP_FILE}" -d "${TARGET_DIR}" >/dev/null

echo "Dataset listo en ${TARGET_DIR}"
