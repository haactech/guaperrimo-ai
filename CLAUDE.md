# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

StyleRAG is a B2B AI-powered image styling advisor for fashion e-commerce. It replaces traditional catalog search with a conversational advisor that analyzes user style via photos, understands emotional intent, and recommends complete outfits from retailer inventory.

**Current state:** Project scaffolding complete. Core interfaces defined, implementation in progress.

## Build Commands

```bash
# Build the server
go build ./cmd/server

# Run the server
go run ./cmd/server

# Run tests
go test ./...

# Build Docker image
docker build -t stylerag .
```

## Tech Stack

- **Language:** Go (monolith)
- **Vector DB:** Qdrant (product embeddings, semantic search)
- **Session Store:** Redis (user style profiles with TTL)
- **Relational DB:** PostgreSQL (product metadata, retailer config, metrics)
- **LLM:** Anthropic/OpenAI via adapter pattern (provider-agnostic)
- **Speech-to-Text:** Avalon API (Aqua Voice)
- **Deployment:** Docker + fly.io or Railway

## Project Structure

```
stylerag/
├── cmd/server/main.go              # Entry point
├── internal/
│   ├── agent/                      # Orchestrator, tools, prompts
│   ├── rag/                        # Vector search + re-ranking engine
│   ├── vision/                     # Proxy to multimodal models
│   ├── voice/                      # STT (Avalon) + WebSocket streaming
│   ├── llm/                        # Provider interface + adapters
│   ├── session/                    # Redis-backed user profiles
│   ├── catalog/                    # PostgreSQL repo + importers
│   └── api/                        # HTTP handlers, middleware, DTOs
├── knowledge/                      # Fashion knowledge base (Markdown)
├── config/
└── Dockerfile
```

## Architecture Patterns

- **Adapter pattern** for LLM providers - `LLMProvider` interface with `Complete()` and `StreamComplete()` methods
- **Tool-use pattern** - Agent calls: `analyze_outfit()`, `search_catalog()`, `get_style_profile()`
- **Smart routing** - Model selection by task type:
  - Image analysis: Potent model (Sonnet/GPT-4o)
  - Preference collection: Economy model (Haiku/4o-mini)
  - Style translation & results presentation: Potent model

## Key Design Decisions

- Voice-first interaction (via Avalon STT) for lower friction
- Target: p95 latency < 2 seconds
- Estimated cost per session: ~$0.033 USD (LLM ~$0.020 + STT ~$0.013)
- PoC scope: Men 25-35, smart casual style, using Kaggle Fashion Product Images dataset (~44K images)
- Embedding model TBD (needs benchmark: CLIP vs text-embedding-3 vs Cohere)

## Next Implementation Steps

1. Download Kaggle Fashion dataset & analyze attributes
2. Benchmark embedding models
3. Implement LLM provider adapters (Anthropic, OpenAI)
4. Implement session manager with Redis
5. Ingest pipeline: Load dataset into Qdrant
6. First functional query: Text-based search with embeddings
