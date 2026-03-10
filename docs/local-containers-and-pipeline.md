# Contenedores locales y pipeline de datos (StyleRAG)

Este setup sigue lo definido en `CLAUDE.md` y `StyleRAG - System Design Document.md`:
- Monolito Go (`app`)
- PostgreSQL (`catalog`, config de retailers, metricas)
- Redis (`session store`)
- Qdrant (`vector DB`)

## 1. Levantar infraestructura local (Docker)

```bash
cp .env.example .env
docker compose up -d --build
```

Servicios:
- API: `http://localhost:8080`
- PostgreSQL: `localhost:5432` (`stylerag/stylerag`)
- Redis: `localhost:6379`
- Qdrant: `http://localhost:6333`

Comandos utiles:

```bash
# Ver estado
docker compose ps

# Ver logs
docker compose logs -f app postgres redis qdrant

# Apagar stack
docker compose down
```

El esquema de PostgreSQL se crea automaticamente con:
- `infra/postgres/init/001_schema.sql`
- `infra/postgres/init/002_seed.sql`

## 2. Pipeline de datos (descarga -> normaliza -> carga -> vectoriza)

### Paso A: descargar dataset Kaggle

Requisitos:
- `kaggle` CLI instalado
- Variables `KAGGLE_USERNAME` y `KAGGLE_KEY` disponibles

```bash
./scripts/pipeline/download_kaggle.sh
```

Salida:
- ZIP descargado en `data/raw/`
- Dataset extraido en `data/raw/fashion-product-images/`

### Paso B: normalizar `styles.csv`

```bash
./scripts/pipeline/prepare_catalog.sh
```

Comando interno:
- `go run ./cmd/catalogprep ...`

Salida:
- `data/processed/products.csv` listo para `COPY` hacia PostgreSQL

### Paso C: inyectar catalogo en PostgreSQL

```bash
./scripts/pipeline/load_postgres.sh
```

Acciones:
1. `TRUNCATE TABLE products`
2. `\copy` de `data/processed/products.csv` -> tabla `products`
3. validacion de `COUNT(*)`

### Paso D: inyectar vectores en Qdrant

```bash
./scripts/pipeline/load_qdrant.sh
```

Comando interno:
- `go run ./cmd/qdrantseed --recreate ...`

Acciones:
1. recrea coleccion `products`
2. genera embeddings deterministicos locales (placeholder hasta definir modelo final)
3. hace upsert por lotes en Qdrant

## 3. Ejecutar pipeline completo

```bash
./scripts/pipeline/run_full_pipeline.sh
```

## 4. Notas importantes

- El embedding usado en local es deterministico y sirve para desarrollo/integracion.
- Para produccion, sustituir `cmd/qdrantseed` por embedding real (CLIP / text-embedding-3 / Cohere) tras benchmark.
- Si cambias el esquema SQL y ya tienes volumen persistente, reinicia con volumen limpio:

```bash
docker compose down -v
docker compose up -d --build
```
