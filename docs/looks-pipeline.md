# Look Generation Pipeline

Pipeline de generacion de looks completos con virtual try-on (VTON). Despues de Phase 4 (recomendaciones), el backend compone 3 outfits curados y genera imagenes de prueba virtual en background.

> **Nota:** La generacion de looks es **opcional**. Requiere VTON + RAG configurados. Sin ellos, Phase 4 funciona normalmente sin looks.

---

## Flujo General

```
Phase 4 Response
     |
     v
LookComposer.ComposeLooks()    <-- sincrono (~2-5s, economy LLM)
     |
     v
state.Looks + state.LookResults = pending
     |
     v
ChatResponse { looks_generating: true }   --> iOS recibe respuesta inmediata
     |
     v
go LookGenerator.GenerateAll()  <-- background goroutine
     |
     +---> Look 1: RAG -> VTON(upper) -> VTON(lower) -> R2
     +---> Look 2: RAG -> VTON(upper) -> VTON(lower) -> R2    (en paralelo)
     +---> Look 3: RAG -> VTON(upper) -> VTON(lower) -> R2
     |
     v
state.LookResults = ready

iOS polls GET /session/{id}/looks  --> pending -> generating -> partial -> ready
```

---

## Contratos API

### 1. `POST /session/{id}/chat` — Respuesta de Phase 4 (modificado)

La respuesta de recomendaciones ahora incluye `looks_generating` cuando el pipeline esta activo.

**Request:** Sin cambios. El ultimo mensaje de discovery que triggers Phase 3+4.

**Response (Phase 4):**

```json
{
  "session_id": "abc-123",
  "phase": "recommendation",
  "turn": 5,
  "message": "Resumen hablado de las recomendaciones...",
  "input_mode": "none",
  "options": null,
  "is_final": true,
  "priority_actions": [
    {
      "id": "action_1",
      "title": "Mejorar armonia de color",
      "description": "...",
      "impact": "alto",
      "effort": "medio",
      "product_ids": ["uuid-1", "uuid-2"]
    }
  ],
  "products": [ /* ... */ ],
  "looks_generating": true
}
```

| Campo | Tipo | Descripcion |
|-------|------|-------------|
| `looks_generating` | `bool` | `true` si el pipeline de looks se lanzo en background. Ausente/`false` si VTON no esta configurado o no hay imagen. |

**Logica iOS:**
- Si `looks_generating == true`: comenzar polling de `GET /session/{id}/looks`
- Si `looks_generating` es `false` o ausente: no hay looks disponibles

---

### 2. `GET /session/{id}/looks` — Polling de estado de looks

Endpoint para que iOS consulte el progreso de generacion de looks. Diseñado para polling con intervalo de 3-5 segundos.

**Request:**

```
GET /session/{id}/looks
```

Sin body. Sin query params.

**Response:**

```json
{
  "session_id": "abc-123",
  "status": "partial",
  "looks": [
    {
      "id": "look_1",
      "name": "Casual Elevado",
      "description": "Look urbano con equilibrio entre comodidad y estilo",
      "vibe": "casual elevado",
      "pieces": [
        {
          "slot": "upper_body",
          "description": "Camisa de lino azul marino con cuello italiano",
          "category": "upper_body"
        },
        {
          "slot": "lower_body",
          "description": "Pantalon chino slim en beige arena",
          "category": "lower_body"
        }
      ]
    },
    {
      "id": "look_2",
      "name": "Smart Casual Refinado",
      "description": "...",
      "vibe": "smart casual",
      "pieces": [ /* ... */ ]
    },
    {
      "id": "look_3",
      "name": "Urbano Moderno",
      "description": "...",
      "vibe": "urbano moderno",
      "pieces": [ /* ... */ ]
    }
  ],
  "look_results": [
    {
      "look_id": "look_1",
      "status": "ready",
      "pieces": [
        {
          "slot": "upper_body",
          "product_id": "uuid-prod-1",
          "product_name": "Blue Linen Shirt",
          "product_image_url": "https://r2.example.com/products/blue-linen.jpg",
          "tryon_image_url": "https://r2.example.com/sessions/abc-123/look_look_1_upper_body_1710000000.jpg",
          "category": "upper_body",
          "generation_time_ms": 12400
        },
        {
          "slot": "lower_body",
          "product_id": "uuid-prod-2",
          "product_name": "Beige Chino Pants",
          "product_image_url": "https://r2.example.com/products/beige-chino.jpg",
          "tryon_image_url": "https://r2.example.com/sessions/abc-123/look_look_1_lower_body_1710000012.jpg",
          "category": "lower_body",
          "generation_time_ms": 11800
        }
      ],
      "final_image_url": "https://r2.example.com/sessions/abc-123/look_look_1_lower_body_1710000012.jpg",
      "generation_time_ms": 24200
    },
    {
      "look_id": "look_2",
      "status": "generating",
      "pieces": [],
      "generation_time_ms": 0
    },
    {
      "look_id": "look_3",
      "status": "pending",
      "pieces": [],
      "generation_time_ms": 0
    }
  ],
  "all_ready": false,
  "ready_count": 1,
  "total_count": 3
}
```

#### Campos del response

| Campo | Tipo | Descripcion |
|-------|------|-------------|
| `session_id` | `string` | ID de la sesion |
| `status` | `string` | Estado global: `"none"` \| `"pending"` \| `"generating"` \| `"partial"` \| `"ready"` |
| `looks` | `LookDTO[]` | Definiciones de los looks (composicion LLM). Disponibles inmediatamente. |
| `look_results` | `LookResultDTO[]` | Resultados de generacion VTON por look. Se van llenando conforme termina cada look. |
| `all_ready` | `bool` | `true` cuando todos los looks terminaron (ready o failed) |
| `ready_count` | `int` | Cantidad de looks con status `ready` o `failed` |
| `total_count` | `int` | Total de looks compuestos |

#### Status global

| Valor | Significado |
|-------|-------------|
| `none` | No hay looks compuestos (VTON no configurado o sin imagen) |
| `pending` | Looks compuestos pero VTON aun no arranca |
| `generating` | Al menos un look esta en generacion VTON |
| `partial` | Al menos un look termino, otros siguen en proceso |
| `ready` | Todos los looks terminaron (ready o failed) |

#### Status por look (`look_results[].status`)

| Valor | Significado |
|-------|-------------|
| `pending` | En cola, esperando turno |
| `generating` | VTON en progreso |
| `ready` | Generacion exitosa, imagen disponible |
| `failed` | Generacion fallo (ver `error_message`) |

#### LookDTO

| Campo | Tipo | Descripcion |
|-------|------|-------------|
| `id` | `string` | ID unico del look (`"look_1"`, `"look_2"`, etc.) |
| `name` | `string` | Nombre del look (ej: "Casual Elevado") |
| `description` | `string` | Descripcion breve del outfit completo |
| `vibe` | `string` | Estilo/vibe del look |
| `pieces` | `LookPieceDTO[]` | Prendas que componen el look |

#### LookPieceDTO

| Campo | Tipo | Descripcion |
|-------|------|-------------|
| `slot` | `string` | `"upper_body"` o `"lower_body"` |
| `description` | `string` | Descripcion de la prenda (usada para RAG search) |
| `category` | `string` | Categoria VTON: `"upper_body"` o `"lower_body"` |

#### LookResultDTO

| Campo | Tipo | Descripcion |
|-------|------|-------------|
| `look_id` | `string` | Referencia al `LookDTO.id` |
| `status` | `string` | Estado de generacion de este look |
| `pieces` | `PieceResultDTO[]` | Resultados por pieza (vacio si pending/generating) |
| `final_image_url` | `string` | URL de la imagen final con ambas prendas (la del ultimo VTON encadenado) |
| `error_message` | `string` | Mensaje de error si `status == "failed"` |
| `generation_time_ms` | `int64` | Tiempo total de generacion en ms |

#### PieceResultDTO

| Campo | Tipo | Descripcion |
|-------|------|-------------|
| `slot` | `string` | `"upper_body"` o `"lower_body"` |
| `product_id` | `string` | UUID del producto encontrado por RAG |
| `product_name` | `string` | Nombre del producto |
| `product_image_url` | `string` | URL de la imagen original del producto |
| `tryon_image_url` | `string` | URL de la imagen generada por VTON |
| `category` | `string` | Categoria VTON usada |
| `generation_time_ms` | `int64` | Tiempo de generacion de esta pieza |

---

## Logica de Polling (iOS)

```swift
// Pseudocodigo para iOS
func pollLooks(sessionID: String) {
    guard looksGenerating else { return }

    Timer.scheduledTimer(withTimeInterval: 4.0, repeats: true) { timer in
        let response = GET("/session/\(sessionID)/looks")

        // Mostrar looks parciales conforme van llegando
        for result in response.look_results where result.status == "ready" {
            displayLook(result)
        }

        // Parar polling cuando todos terminen
        if response.all_ready {
            timer.invalidate()
        }
    }
}
```

**Recomendaciones:**
- Intervalo de polling: **4 segundos** (cada look tarda ~20-30s con VTON encadenado)
- Mostrar looks parciales conforme van terminando (UX progresiva)
- Usar `looks[]` para mostrar nombres/vibes como placeholders mientras se generan
- Usar `final_image_url` como la imagen principal del look (tiene ambas prendas)
- `pieces[].tryon_image_url` muestra el paso intermedio (solo upper_body en la primera pieza)

---

## Errores

| HTTP Status | Escenario |
|-------------|-----------|
| `400` | `id` faltante en path |
| `404` | Sesion no encontrada o expirada |
| `200` | Siempre retorna 200 con `status: "none"` si no hay looks |

No hay errores 500 — el pipeline de looks es best-effort. Si falla la composicion LLM, `looks_generating` sera `false`. Si falla el VTON de un look individual, ese look tendra `status: "failed"` pero los demas continuaran.

---

## Degradacion Graceful

| Escenario | Comportamiento |
|-----------|---------------|
| VTON no configurado | Phase 4 responde normalmente, `looks_generating` ausente |
| RAG no disponible | Igual — looks deshabilitados |
| LLM composition falla | `looks_generating: false`, warning en logs |
| VTON falla para 1 look | Ese look `status: "failed"`, otros continuan |
| VTON falla para 1 pieza | Pieza sin `tryon_image_url`, look puede ser parcial |
| Session expira durante generacion | Generacion se detiene, resultados se pierden |

---

## Configuracion (env vars)

| Variable | Default | Descripcion |
|----------|---------|-------------|
| `LOOK_COUNT` | `3` | Numero de looks a generar |
| `LOOK_GENERATION_TIMEOUT` | `90s` | Timeout por look individual (VTON + RAG + upload) |

Requiere que esten configurados: `GCP_PROJECT_ID` (o `FASHN_API_KEY`), `QDRANT_URL`, `OPENAI_API_KEY`, y el catalogo en PostgreSQL.

---

## Arquitectura Interna

```
cmd/server/main.go
  └─ Crea LookComposer + LookGenerator si VTON + RAG disponibles
  └─ Los inyecta en ChatDeps

internal/tryon/
  ├─ look_composer.go            LLM compose N looks (economy model)
  ├─ look_composition_prompt.go  Prompt en español para composicion
  └─ look_generator.go           VTON background con goroutines paralelas

internal/session/state.go        Tipos: Look, LookResult, LookPiece, etc.
internal/api/looks_handler.go    GET /session/{id}/looks
internal/api/chat_handler.go     Trigger al final de Phase 4
internal/api/dto.go              DTOs de respuesta
internal/llm/router.go           TurnTypeLookComposition -> economy
internal/config/config.go        LOOK_COUNT, LOOK_GENERATION_TIMEOUT
```

### VTON Chaining

Cada look tiene 2 piezas (upper + lower). La generacion es **encadenada**:

1. VTON genera upper_body sobre la foto del usuario → imagen intermedia
2. VTON genera lower_body sobre la **imagen intermedia** → imagen final con ambas prendas

Esto produce un resultado donde el usuario aparece vistiendo ambas prendas del look. `final_image_url` apunta a esta imagen encadenada.

### Concurrencia

- Los 3 looks se generan en **paralelo** (3 goroutines)
- Dentro de cada look, las piezas son **secuenciales** (por el chaining)
- Un `sync.Mutex` local serializa las actualizaciones de estado (Get → modify → Save) para evitar lost writes
- Tiempo estimado total: ~30s (vs ~90s si fueran secuenciales)
