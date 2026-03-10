# Flujo de Análisis de Imagen — iOS ↔ Backend Go

## Diagrama de Flujo

```
┌─────────────┐                    ┌──────────────────┐                    ┌─────────┐
│   iOS App   │                    │   Go Backend     │                    │   R2    │
│             │                    │   :8080          │                    │  Storage │
└──────┬──────┘                    └────────┬─────────┘                    └────┬────┘
       │                                    │                                   │
       │  ① POST /session/{id}/image        │                                   │
       │  (multipart: field "image")        │                                   │
       │───────────────────────────────────►│                                   │
       │                                    │  PutObject                        │
       │                                    │  key: sessions/{id}/welcome_{ts}  │
       │                                    │──────────────────────────────────►│
       │                                    │                    ◄──────────────│
       │     ◄──────────────────────────────│                                   │
       │  {"url":"...","session_id":"..."}  │                                   │
       │                                    │                                   │
       │  ② POST /session/{id}/analyze      │                                   │
       │  (no body)                         │                                   │
       │───────────────────────────────────►│                                   │
       │                                    │                                   │
       │                                    │  ListObjectsV2                    │
       │                                    │  prefix: sessions/{id}/           │
       │                                    │──────────────────────────────────►│
       │                                    │  keys[]                ◄─────────│
       │                                    │                                   │
       │                                    │  GetObject (latest key)           │
       │                                    │──────────────────────────────────►│
       │                                    │  image bytes           ◄─────────│
       │                                    │                                   │
       │                              ┌─────┴──────────────────────────┐        │
       │                              │  ③ LLM Analyzer (potent)       │        │
       │                              │  mistral-large-latest          │        │
       │                              │                                │        │
       │                              │  Input:  base64 image + prompt │        │
       │                              │  Output: OutfitAnalysis (JSON) │        │
       │                              │    ├─ detected_items[]         │        │
       │                              │    ├─ detected_styles[]        │        │
       │                              │    ├─ detected_colors[]        │        │
       │                              │    ├─ overall_fit              │        │
       │                              │    └─ observations (español)   │        │
       │                              └─────┬──────────────────────────┘        │
       │                                    │                                   │
       │                              ┌─────┴──────────────────────────┐        │
       │                              │  ④ Style Advisor (economy)     │        │
       │                              │  mistral-small-latest          │        │
       │                              │                                │        │
       │                              │  Input:  OutfitAnalysis JSON   │        │
       │                              │  Output: StyleAdvice (JSON)    │        │
       │                              │    ├─ message (1-2 oraciones)  │        │
       │                              │    └─ options[4]               │        │
       │                              │        ├─ id (snake_case)      │        │
       │                              │        ├─ title (2 palabras)   │        │
       │                              │        └─ description (corta)  │        │
       │                              └─────┬──────────────────────────┘        │
       │                                    │                                   │
       │                              ┌─────┴──────────────────────────┐        │
       │                              │  ⑤ Mapeo a Response           │        │
       │                              │                                │        │
       │                              │  StyleAnalysisResponse {       │        │
       │                              │    message  ← advice.Message   │        │
       │                              │    analysis ← analysis.Obs.    │        │
       │                              │    options  ← advice.Options   │        │
       │                              │  }                             │        │
       │                              └─────┬──────────────────────────┘        │
       │                                    │                                   │
       │     ◄──────────────────────────────│                                   │
       │  {                                 │                                   │
       │    "message": "Look casual...",    │                                   │
       │    "analysis": "Outfit casual...", │                                   │
       │    "options": [4 opciones]         │                                   │
       │  }                                 │                                   │
       │                                    │                                   │
       │  ⑥ iOS muestra mensaje +           │                                   │
       │     habla analysis via TTS         │                                   │
       │     (ElevenLabs)                   │                                   │
       │                                    │                                   │
       │  ⑦ iOS muestra 4 opciones          │                                   │
       │     de estilo al usuario           │                                   │
       ▼                                    ▼                                   ▼
```

## Transformaciones de Datos

```
Imagen JPG/PNG (bytes)
    │
    ▼
┌──────────────────────────────────────────┐
│ OutfitAnalysis (LLM potent)              │
│                                          │
│ {                                        │
│   "detected_items": [                    │
│     {                                    │
│       "category": "tops",                │
│       "subcategory": "t-shirt",          │
│       "color": "black",                  │
│       "fit": "regular",                  │
│       "style_tags": ["casual","branded"] │
│     }, ...                               │
│   ],                                     │
│   "detected_styles": ["casual","urban"], │
│   "detected_colors": ["black","navy"],   │
│   "overall_fit": "regular",              │
│   "observations": "Outfit casual..."     │  ──► response.analysis (para TTS)
│ }                                        │
└──────────────────┬───────────────────────┘
                   │
                   ▼
┌──────────────────────────────────────────┐
│ StyleAdvice (LLM economy)                │
│                                          │
│ {                                        │
│   "message": "Look casual con...",       │  ──► response.message
│   "options": [                           │  ──► response.options
│     {                                    │
│       "id": "smart_casual",              │
│       "title": "Smart Casual",           │
│       "description": "Eleva el look..."  │
│     }, ... (×4)                           │
│   ]                                      │
│ }                                        │
└──────────────────────────────────────────┘
```

## Archivos Involucrados

| Capa | Archivo | Responsabilidad |
|------|---------|-----------------|
| **API** | `api/router.go` | Registra rutas, inyecta dependencias |
| **API** | `api/image_handler.go` | `POST /session/{id}/image` — upload a R2 |
| **API** | `api/analyze_handler.go` | `POST /session/{id}/analyze` — orquesta el flujo |
| **API** | `api/dto.go` | `StyleAnalysisResponse` — contrato con iOS |
| **Storage** | `storage/storage.go` | Interface `ImageStore` (Upload, Download, ListKeys) |
| **Storage** | `storage/r2.go` | Implementación con Cloudflare R2 (S3-compat) |
| **Vision** | `vision/analyzer.go` | Interface `Analyzer` + structs `OutfitAnalysis` |
| **Vision** | `vision/llm_analyzer.go` | `LLMAnalyzer` — imagen → OutfitAnalysis via LLM |
| **Vision** | `vision/prompt.go` | Prompt del análisis técnico de outfit |
| **Vision** | `vision/style_advisor.go` | `StyleAdvisor` — OutfitAnalysis → StyleAdvice via LLM |
| **Vision** | `vision/advisor_prompt.go` | Prompt del advisor conversacional |
| **LLM** | `llm/provider.go` | Interface `Provider`, tipos compartidos |
| **LLM** | `llm/router.go` | `Router` — selecciona potent/economy por TurnType |
| **LLM** | `llm/mistral.go` | Provider Mistral (image_url como string plano) |
| **LLM** | `llm/kimi.go` | Provider Kimi/Moonshot (image_url como objeto anidado) |
| **Config** | `config/config.go` | Carga `.env` (soporta `export`), env vars |
| **Main** | `cmd/server/main.go` | Wiring: providers → router → analyzer → advisor → API |

## Routing de Modelos

```
TurnType                    → Tier      → Modelo (Mistral)
─────────────────────────────────────────────────────────
TurnTypeImageAnalysis       → potent    → mistral-large-latest
TurnTypePreferenceGather    → economy   → mistral-small-latest
TurnTypeStyleTranslation    → potent    → mistral-large-latest
TurnTypeResultPresent       → potent    → mistral-large-latest
```

## Variables de Entorno Requeridas

```bash
# LLM Provider (default: mistral)
LLM_PROVIDER=mistral

# Mistral
MISTRAL_API_KEY=xxx
MISTRAL_POTENT_MODEL=mistral-large-latest     # opcional
MISTRAL_ECONOMY_MODEL=mistral-small-latest    # opcional

# Kimi (alternativo)
MOONSHOT_API_KEY=xxx
KIMI_POTENT_MODEL=kimi-k2.5                  # opcional
KIMI_ECONOMY_MODEL=kimi-k2.5                 # opcional

# Cloudflare R2
R2_ACCOUNT_ID=xxx
R2_ACCESS_KEY_ID=xxx
R2_ACCESS_KEY_SECRET=xxx
R2_BUCKET_NAME=xxx
R2_PUBLIC_URL=xxx
```

## Descubrimientos y Lecciones

### Kimi (Moonshot)
- `response_format: json_object` causa respuestas vacías — omitir
- `temperature` solo acepta `1` para kimi-k2.5 — omitir
- `MaxTokens` mínimo de 8192 para evitar truncamiento
- Chain-of-thought en `reasoning_content` hace cada llamada ~30-60s
- Total ~60-120s por análisis — **demasiado lento para mobile**

### Mistral
- Los modelos Pixtral están **deprecados**
- `mistral-large-latest` y `mistral-small-latest` ya soportan visión
- Free tier tiene rate limiting agresivo (429)
- `image_url` se envía como string plano, no como objeto anidado

### Infraestructura
- `godotenv` no parsea `export KEY=VAL` — reemplazado con parser custom
- Variables de entorno de sesiones previas (`source .env`) pueden causar 401 si la key cambió — usar `unset` antes
- `WriteTimeout` del server debe ser ≥120s para providers lentos
- iOS `URLRequest.timeoutInterval` debe coincidir

## Test Manual

```bash
# 1. Upload
curl -F "image=@foto.jpg" http://localhost:8080/session/test123/image

# 2. Analyze
curl --max-time 120 -X POST http://localhost:8080/session/test123/analyze
```
