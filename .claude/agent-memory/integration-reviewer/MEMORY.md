# StyleRAG Integration Reviewer Memory

## Key Files

- Entry point: `cmd/server/main.go` — wires all providers, reads config
- Config: `internal/config/config.go` — all env vars, custom loadDotenv()
- Router: `internal/api/router.go` — all HTTP endpoints registered here
- DTOs: `internal/api/dto.go` — all request/response shapes
- Session state: `internal/session/state.go` — full session model incl. Looks, TryOnResults

## LLM Provider Pattern

See `MEMORY.md` in the global project memory. Adapter pattern: `llm.Provider` interface
→ `mistral.go` / `kimi.go` concrete impls → wired in `cmd/server/main.go` via switch on `LLM_PROVIDER`.

## VTON Provider Pattern

See `vton-integration.md` for full details. Summary:
- Interface: `internal/tryon/provider.go` — `VTONProvider` with `Generate()` and `Name()`
- Google impl: `internal/tryon/google_vertex.go`
- FASHN impl: `internal/tryon/fashn.go` (stub, not implemented)
- Handler: `internal/api/tryon_handler.go` — POST /session/{id}/tryon
- Look pipeline: `internal/tryon/look_composer.go` + `look_generator.go`
- CLI test tool: `cmd/tryontest/main.go`

## Storage Pattern

`internal/storage/storage.go` — `ImageStore` interface: Upload, Download, ListKeys.
Cloudflare R2 impl in `internal/storage/r2.go`. Keys follow `sessions/{id}/...` pattern.

## Config Env Vars for VTON

- `VTON_PROVIDER` — "google_vertex" (default) or "fashn"
- `GCP_PROJECT_ID` — required to enable VTON
- `GCP_SA_KEY_JSON` — service account JSON (production); empty = local ADC
- `GCP_REGION` — defaults to "us-central1"
- `VTON_BASE_STEPS` — defaults to 20 (API default is 32)
- `VTON_TIMEOUT` — defaults to "30s" (may need increase to 60s)
- `FASHN_API_KEY`, `FASHN_MODE` — for FASHN provider
- `LOOK_GENERATION_TIMEOUT` — defaults to "90s"
- `LOOK_COUNT` — defaults to 3

## Server Timeouts

- `WriteTimeout`: 120s
- VTON context timeout: 30s (set in tryon_handler.go)
- VTON HTTP client timeout: cfg.VTONTimeout (30s default)
- Look generation timeout: cfg.LookGenerationTimeout (90s default)
- Image download: 10s

## Session Phases

capture → discovery → diagnosis → recommendation
Try-on only available in PhaseRecommendation.

## Codebase Compiles Cleanly

Confirmed: `go build ./...` passes with no errors as of 2026-03-12.
