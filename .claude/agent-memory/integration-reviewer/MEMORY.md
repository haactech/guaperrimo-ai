# guaperrimo backend — Integration Reviewer Memory

## Key files (v2, agent-centric)

- Entry point: `cmd/server/main.go` — wires LLM, shopping provider, session store, VTON
- Config: `internal/config/config.go` — env vars, `.env` loader (`export KEY=VAL` supported)
- Router/DTOs: `internal/api/router.go`, `internal/api/dto.go`
- Agent loop + tools + system prompt: `internal/agent/{runner,tools,prompt}.go`
- Session state: `internal/session/state.go`; stores: `store.go` (memory), `postgres_store.go`
- Products/stores: `internal/shopping/{serpapi,fake,geo}.go`
- LLM client: `internal/llm/openai_compat.go` (Mistral / Moonshot / OpenAI flavours)
- VTON: `internal/tryon/{google_vertex,look_generator,download}.go` — see `vton-integration.md`

## Patterns

- Every chat turn ends with a terminal tool (`ask_user` / `finish_recommendation`).
- Tool results are compact JSON; product ids are validated in Go before finishing.
- Handlers persist via `session.Store.Update` (read-modify-write under lock).
- Without `SERPAPI_KEY` the `shopping.Fake` provider is used; tests never hit the network.
