# Motor RAG — Búsqueda Semántica de Productos

## Resumen

El motor RAG (Retrieval-Augmented Generation) conecta el diagnóstico de estilo del usuario con productos reales del catálogo. Después de que Phase 3 (Diagnosis) identifica gaps de estilo con recomendaciones accionables, el RAG busca productos semánticamente similares en Qdrant y los inyecta en el prompt de Phase 4 para que el LLM referencie items reales.

## Arquitectura

```
┌─────────────────────────────────────────────────────────────────┐
│                     Flujo Conversacional                        │
│                                                                 │
│  Phase 1         Phase 2         Phase 3         Phase 4        │
│  Capture    →    Discovery   →   Diagnosis   →   Recommendation │
│  (imagen)        (preguntas)     (gap analysis)  (consejos)     │
│                                       │                ▲        │
│                                       │                │        │
│                              ┌────────▼────────┐       │        │
│                              │  Phase 3.5: RAG │       │        │
│                              │  (búsqueda)     │───────┘        │
│                              └────────┬────────┘                │
│                                       │                         │
└───────────────────────────────────────│─────────────────────────┘
                                        │
                    ┌───────────────────┼───────────────────┐
                    │                   │                   │
              ┌─────▼─────┐     ┌──────▼──────┐     ┌─────▼─────┐
              │  OpenAI    │     │   Qdrant    │     │ PostgreSQL│
              │ Embeddings │     │  (vectores) │     │ (metadata)│
              └───────────┘     └─────────────┘     └───────────┘
```

### Secuencia detallada de Phase 3.5

```
GapAnalysis[] ──→ selectTopGaps(3) ──→ BuildSearchQueries()
                                              │
                                    ┌─────────┼─────────┐
                                    │         │         │  (paralelo con errgroup)
                                    ▼         ▼         ▼
                              EmbedText  EmbedText  EmbedText   ← OpenAI API
                                    │         │         │
                                    ▼         ▼         ▼
                              Qdrant     Qdrant    Qdrant       ← Vector search
                              search     search    search
                                    │         │         │
                                    └─────────┼─────────┘
                                              ▼
                                    GetProducts(ids[])           ← PostgreSQL hydration
                                              │
                                              ▼
                                    GapProductContext[]
                                              │
                                              ▼
                              GenerateRecommendationWithProducts() ← LLM con contexto
```

## Componentes

### 1. Embedder (`internal/rag/embedder.go`)

Genera vectores de embedding usando OpenAI `text-embedding-3-small`.

```go
type OpenAIEmbedder struct { ... }

func (e *OpenAIEmbedder) EmbedText(ctx, text) ([]float32, error)
func (e *OpenAIEmbedder) EmbedBatch(ctx, texts) ([][]float32, error)
```

- **Modelo**: `text-embedding-3-small` (1536 dimensiones, $0.02/1M tokens)
- **API**: POST `https://api.openai.com/v1/embeddings`
- Patrón HTTP idéntico a `internal/llm/mistral.go`

### 2. Qdrant Engine (`internal/rag/qdrant_engine.go`)

Implementa `rag.Engine` — búsqueda vectorial + hidratación desde PostgreSQL.

```go
type QdrantEngine struct { ... }

func (e *QdrantEngine) Search(ctx, query) ([]Product, error)
func (e *QdrantEngine) SearchWithScores(ctx, query) ([]SearchResult, error)
```

**Flujo de `SearchWithScores`:**
1. `EmbedText(query.Text)` → vector de 1536 dimensiones
2. POST a Qdrant `/collections/products/points/search` con vector + filtros
3. Extraer IDs y scores del response
4. `hydrator.GetProducts(ids)` — hidratar metadata desde PostgreSQL
5. Re-ordenar por score de Qdrant (PostgreSQL retorna orden arbitrario)

**Filtros soportados:**
| Campo | Operación Qdrant | Ejemplo |
|-------|-----------------|---------|
| `Categories` | `match: {value: "Topwear"}` | Solo ropa superior |
| `MaxPrice` | `range: {lte: 2000}` | Productos bajo $2,000 |
| `MinPrice` | `range: {gte: 500}` | Productos sobre $500 |

### 3. Query Builder (`internal/rag/query_builder.go`)

Traduce `GapItem[]` + `UserStyleProfile` a `SearchQuery[]`.

```go
func BuildSearchQueries(gaps []session.GapItem, profile *UserStyleProfile, limit int) []SearchQuery
```

- Cada `GapItem.Actionable` → `SearchQuery.Text` (ya es descripción semántica rica)
- Extrae presupuesto máximo de `profile.Constraints` vía regex
- Ignora gaps con `Actionable == ""`
- Default: 3 productos por gap

### 4. Integración en Chat Handler (`internal/api/chat_handler.go`)

Se insertó **Phase 3.5** entre Diagnosis y Recommendation:

```go
// ChatDeps ahora incluye:
RAGEngine rag.Engine // nil = RAG deshabilitado

// Helpers nuevos:
selectTopGaps(gaps, n)           // top N por Priority
collectUniqueProducts(gapProds)  // deduplica por ID
```

La búsqueda es **paralela** (errgroup) y **no-fatal** — si falla una query, las demás continúan. Si RAG no está configurado, el flujo funciona idénticamente al anterior.

### 5. Prompt con Productos (`internal/vision/recommendation_prompt.go`)

`advisorPromptWithProducts` extiende el prompt base con:

- Sección de productos agrupados por gap (JSON minificado: id, name, price, image_url)
- Regla: "Referencia productos reales por nombre cuando existan"
- Regla: "Incluye `product_ids` en cada `priority_action`"
- Regla: "Si no hay productos para un gap, da recomendación genérica"

### 6. Response Actualizado

```json
{
  "session_id": "abc-123",
  "phase": "recommendation",
  "message": "Tu outfit tiene buena base con los jeans oscuros...",
  "is_final": true,
  "priority_actions": [
    {
      "id": "upgrade_shirt",
      "title": "Cambiar camisa",
      "description": "Una Blue Oxford Shirt en regular fit elevaría tu look",
      "impact": "alto",
      "effort": "bajo",
      "product_ids": ["6377254e-2884-48df-a685-69e535401c18"]
    }
  ],
  "products": [
    {
      "id": "6377254e-2884-48df-a685-69e535401c18",
      "name": "Blue Oxford Shirt",
      "description": "Classic blue oxford button-down shirt",
      "category": "Topwear",
      "subcategory": "Shirts",
      "colors": ["Blue"],
      "fit": "Regular",
      "price": 899,
      "image_url": "https://example.com/blue-oxford.jpg"
    }
  ]
}
```

- `priority_actions[].product_ids` → IDs de productos que resuelven ese gap
- `products[]` → flat list deduplicada de todos los productos referenciados (para iOS)

## Degradación Graceful

El RAG se habilita **solo si** las 3 condiciones se cumplen:

| Variable | Requerida para |
|----------|---------------|
| `OPENAI_API_KEY` | Generar embeddings |
| `QDRANT_URL` | Búsqueda vectorial |
| `POSTGRES_URL` | Hidratación de metadata |

Si alguna falta, `ragEngine = nil` y el flujo funciona exactamente como antes (texto puro, sin productos). El log indica:

```
WARN RAG engine disabled  qdrant_url_set=false openai_key_set=true catalog_repo_set=true
```

Dentro de Phase 3.5, errores individuales de búsqueda son **non-fatal** (logged, no propagados).

## Configuración

Variables de entorno nuevas:

| Variable | Default | Descripción |
|----------|---------|-------------|
| `OPENAI_API_KEY` | — | API key de OpenAI para embeddings |
| `OPENAI_EMBEDDING_MODEL` | `text-embedding-3-small` | Modelo de embeddings |
| `OPENAI_EMBEDDING_DIMS` | `1536` | Dimensiones del vector |
| `QDRANT_COLLECTION` | `products` | Nombre de collection en Qdrant |

## Seed Pipeline

El pipeline de seed (`cmd/qdrantseed`) ahora soporta embeddings reales:

```bash
# Con embeddings reales de OpenAI
go run ./cmd/qdrantseed \
  --input data/products_normalized.csv \
  --qdrant-url http://localhost:6333 \
  --vector-size 1536 \
  --recreate \
  --openai-api-key $OPENAI_API_KEY

# Con vectores fake (para desarrollo/testing)
go run ./cmd/qdrantseed \
  --input data/products_test.csv \
  --qdrant-url http://localhost:6333 \
  --vector-size 64 \
  --fake-vectors \
  --recreate
```

Flags nuevos:
- `--openai-api-key` / `OPENAI_API_KEY`: API key (sin key → auto fake vectors)
- `--embedding-model`: modelo (default `text-embedding-3-small`)
- `--fake-vectors`: vectores determinísticos sin API call
- `--embed-batch-size`: textos por request a OpenAI (default 50)
- `--vector-size`: ahora default 1536 (antes 64)

El pipeline incluye retry con backoff exponencial para rate limiting (HTTP 429).

## Overhead de Latencia

| Paso | Tiempo estimado |
|------|----------------|
| Embed 3 queries (paralelo) | ~200ms |
| Qdrant search x3 (paralelo) | ~50ms |
| PostgreSQL batch hydration | ~20ms |
| **Total RAG** | **~300ms** |

El bottleneck sigue siendo los LLM calls (~5-15s para Mistral). El RAG agrega <5% de latencia.

## Tests

```bash
# Unit tests (sin infra)
go test ./internal/rag/...

# Integration tests (requiere Qdrant corriendo + datos seeded)
RAG_INTEGRATION=1 go test -v -run TestIntegration ./internal/rag/...

# All tests
go test ./...
```

### Cobertura de tests:

| Test | Tipo | Qué verifica |
|------|------|-------------|
| `TestBuildSearchQueries_*` | Unit | Query builder: gaps → queries, budget extraction |
| `TestExtractBudget` | Unit | Regex de extracción de presupuesto (pesos, mxn, usd, $) |
| `TestBuildQdrantFilter_*` | Unit | Construcción de filtros Qdrant (category, price range) |
| `TestIntegration_QdrantSearch` | Integration | Búsqueda end-to-end contra Qdrant real |
| `TestIntegration_QdrantSearchWithCategoryFilter` | Integration | Filtro por categoría |
| `TestIntegration_QdrantSearchWithPriceFilter` | Integration | Filtro por precio máximo |

## Archivos

| Archivo | Tipo |
|---------|------|
| `internal/rag/engine.go` | Interfaces: `Engine`, `Embedder`, `ProductHydrator` |
| `internal/rag/embedder.go` | **Nuevo** — OpenAI embeddings |
| `internal/rag/qdrant_engine.go` | **Nuevo** — Qdrant search + hydration |
| `internal/rag/query_builder.go` | **Nuevo** — GapItem → SearchQuery |
| `internal/rag/query_builder_test.go` | **Nuevo** — Unit tests |
| `internal/rag/qdrant_engine_test.go` | **Nuevo** — Unit tests |
| `internal/rag/integration_test.go` | **Nuevo** — Integration tests |
| `internal/config/config.go` | Modificado — Campos de embedding |
| `internal/session/state.go` | Modificado — ProductIDs en PriorityAction |
| `internal/api/dto.go` | Modificado — Products en ChatResponse |
| `internal/api/chat_handler.go` | Modificado — Phase 3.5 RAG |
| `internal/vision/recommendation_prompt.go` | Modificado — Prompt con productos |
| `internal/vision/style_advisor.go` | Modificado — GenerateRecommendationWithProducts |
| `cmd/server/main.go` | Modificado — Wiring RAG engine |
| `cmd/qdrantseed/main.go` | Modificado — Real embeddings + fallback |
