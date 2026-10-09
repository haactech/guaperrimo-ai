# CLAUDE.md

Guidance for Claude Code when working in this repository.

## What this is

Backend of **guaperrimo.ai**, a B2C iOS app for men in Mexico who do not know
fashion: the user takes a full-body photo, a stylist agent asks a few
questions by voice or buttons, and returns what favours him, two or three
complete looks, and a **shopping list of real articles within his budget,
prioritising stores at walking distance** (shipping only if he accepts it).
Spanish only, menswear only, Mexico only.

The iOS client lives in a separate repository (`guaperrimo.ai`, Swift/SwiftUI).

## Architecture (agent-centric, no RAG)

```
iOS ──POST /session/{id}/image──▶ R2 (photo)
iOS ──POST /session/{id}/chat───▶ chat handler
                                    │ first turn: vision analysis of the photo (LLM, JSON)
                                    ▼
                                agent.Runner ── tool loop (LLM with tool calling)
                                    ├─ update_profile        facts: occasion, date, budget, radius, shipping
                                    ├─ set_location          geocode "colonia, ciudad" (SerpAPI Google Maps)
                                    ├─ find_nearby_stores    physical stores by distance (SerpAPI Google Maps)
                                    ├─ search_products       real products with price/store/link (SerpAPI Google Shopping)
                                    ├─ ask_user   (terminal) one question, buttons or voice
                                    └─ finish_recommendation (terminal) summary + actions + shopping list + looks
                                    ▼
                                session.Store (memory or Postgres JSONB)
                                    ▼ (optional, background)
                                tryon.Renderer ── Vertex try-on, one cached render per garment-chain prefix
                                    ├─ prewarm: default combination + one-swipe neighbours
                                    ├─ GET /matrix · POST /matrix/render (mix & match rows)
                                    └─ POST /saved-looks (chosen outfit + why + stores)
```

- `internal/llm` — provider-agnostic chat completions with tool calling.
  `OpenAICompat` talks to Mistral, Moonshot/Kimi or OpenAI; flavour quirks live there.
- `internal/vision` — photo → `OutfitAnalysis` (colour season, Kibbe family,
  archetype, fit scores). `Summarize` renders it for the agent's system prompt.
- `internal/agent` — the runner, tool schemas and the system prompt (Spanish).
  Go validates the final recommendation: product ids must come from
  `search_products`, and the list is rejected once if it exceeds the budget.
- `internal/shopping` — `Provider` interface; `SerpAPI` (real) and `Fake`
  (deterministic, used when `SERPAPI_KEY` is empty and in tests). Merchant
  names are matched against nearby stores to flag "available near you".
- `internal/session` — JSON state (profile, agent memory, products seen,
  recommendation, looks) with `MemoryStore` and `PostgresStore`.
  `Update` does read-modify-write under a lock so background jobs never clobber a turn.
- `internal/tryon` — Vertex VTON provider, safe image download, `Renderer` (prefix cache,
  in-flight dedupe, bounded prefetch) and the look generator built on it.
- `internal/api` — handlers, DTOs, optional `X-API-Key` middleware.

## Commands

```bash
go build ./...          # build
go test ./...           # unit tests (no network: fakes + httptest)
go run ./cmd/server     # run (reads .env)
docker compose up       # app + postgres
go run ./cmd/tryontest -person p.jpg -garment g.jpg   # VTON smoke test
python3 scripts/chat.py --photo foto.jpg              # interactive terminal client (no iPhone needed)
LLM_LIVE=1 go test ./internal/llm -run Live -v        # one real tool-calling round trip
```

## Configuration

See `.env.example`. Required: an LLM key and the R2 variables. Optional:
`SERPAPI_KEY` (without it products are fake), `POSTGRES_URL` (without it
sessions are in memory), `GCP_PROJECT_ID` (enables try-on), `API_KEY`.

## Conventions

- Tool results sent back to the model are compact JSON; keep them small.
- Every agent turn must end with `ask_user` or `finish_recommendation`; the
  runner treats plain text as an open question and caps steps and questions.
- Never log user transcripts at Info; never return raw LLM output in HTTP errors.
- Keep `docs/API.md` in sync with `internal/api/dto.go`; the iOS app depends on it.
