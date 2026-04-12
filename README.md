<div align="center">

# pgmemory

**Persistent memory for AI coding agents — backed by PostgreSQL + pgvector.**

[![CI](https://github.com/jeff-vincent/pgmemory/actions/workflows/ci.yml/badge.svg)](https://github.com/jeff-vincent/pgmemory/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/jeff-vincent/pgmemory)](https://goreportcard.com/report/github.com/jeff-vincent/pgmemory)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Docs](https://img.shields.io/badge/docs-pgmemory-orange)](https://jeff-vincent.github.io/pgmemory/)

</div>

---

`pgmemory` is a local memory daemon for AI coding agents. It connects to your AI coding tools (Claude Code, Cursor, Windsurf, etc.) and transparently captures knowledge from every session — architecture decisions, debugging insights, deployment procedures, codebase conventions. Everything is stored in PostgreSQL with pgvector, so your tools remember what you've worked on across sessions.

By default, pgmemory runs an **embedded PostgreSQL** instance — no external database required. For teams, point it at a shared PostgreSQL instance and everyone's tools draw from the same knowledge base.

```
Your AI Tools → pgmemory → LLM Provider
                    ↕
           PostgreSQL + pgvector
     (embedded or shared instance)
```

**[Read the full documentation →](https://jeff-vincent.github.io/pgmemory/)**

## Quick Start

```bash
# Install (macOS)
curl -fsSL https://raw.githubusercontent.com/jeff-vincent/pgmemory/main/install.sh | bash

# Run — embedded PostgreSQL starts automatically
pgmemory start
export ANTHROPIC_BASE_URL=http://127.0.0.1:7432
```

Work normally with Claude Code — knowledge builds in the background and persists across sessions.

For Cursor, Windsurf, or any MCP-compatible tool:

```json
{
  "mcpServers": {
    "pgmemory": { "command": "pgmemory", "args": ["mcp"] }
  }
}
```

See the **[Getting Started](https://jeff-vincent.github.io/pgmemory/getting-started)** guide for full setup.

## Why pgmemory?

| Challenge | How pgmemory helps |
|---|---|
| **Sessions start cold** | Knowledge from past sessions is automatically available |
| **Repeated context** | Stop re-explaining your codebase to the AI every time |
| **Knowledge silos** | Point a team at a shared PostgreSQL instance — everyone benefits |
| **Tool fragmentation** | Works with Claude Code, Cursor, Windsurf, Cline — same knowledge store |
| **Zero infrastructure** | Embedded PostgreSQL + pgvector — nothing to install or manage |

## How It Works

### [Knowledge Capture](https://jeff-vincent.github.io/pgmemory/how-it-works/write-path)

Every AI response is captured asynchronously (zero latency impact), passed through multi-stage quality filters (length gate, adaptive content scoring, LLM synthesis gate), scrubbed of secrets (API keys, tokens, passwords — 13 detection patterns), deduplicated, and stored. The system learns what noise looks like from rejected exchanges, improving filtering accuracy over time.

### [Context Retrieval](https://jeff-vincent.github.io/pgmemory/how-it-works/read-path)

AI tools access the knowledge base through MCP tools — searching, storing, and updating memories. Prior context surfaces automatically without anyone asking for it.

### [Quality Maintenance](https://jeff-vincent.github.io/pgmemory/how-it-works/quality-loop)

A background process scores knowledge by usefulness and recency, prunes noise, and merges near-duplicates. The store self-maintains.

### [Hybrid Search](https://jeff-vincent.github.io/pgmemory/how-it-works/hybrid-search)

Retrieval combines pgvector cosine similarity with PostgreSQL full-text search via Reciprocal Rank Fusion (RRF) and Maximal Marginal Relevance (MMR) diversity re-ranking. Finds both conceptually related and exact-match results.

### [Tool Integration](https://jeff-vincent.github.io/pgmemory/agents/mcp-server)

Connects via transparent proxy (Claude Code) or MCP server (Cursor, Windsurf, any MCP tool). Different team members can use different tools — they all share the same knowledge.

## Architecture

```
cmd/pgmemory/              CLI (cobra commands)
internal/
  config/                  YAML config with defaults
  chunker/                 Text chunking at natural boundaries
  embedding/               Local embeddings (voyage-4-nano, 1024-dim)
  pipeline/
    read.go                Search → format context → return
    write.go               Chunk → filter → scrub secrets → dedup → store
    inject.go              Context formatting with token budget
  proxy/
    proxy.go               HTTP server, REST API, dashboard
    anthropic.go           Anthropic proxy with streaming support
  store/
    store.go               Store interfaces
    postgres.go            PostgreSQL + pgvector (hybrid search, RRF, MMR)
    embedded.go            Embedded PostgreSQL lifecycle
  redact/                  Secret scrubbing (13 patterns)
  quality/                 Usage tracking, content scoring, adaptive noise learning
  rejection/               Rejection store — ring buffer for adaptive noise learning
  steward/                 Background maintenance (score → prune → merge)
  ingest/                  Source ingestion and change detection
  crawler/                 Web crawler with change detection
  synthesizer/             LLM synthesis (atomic fact extraction)
  mcp/                     MCP stdio server (10 tools)
  export/                  Markdown export
website/                   Documentation site
```

## CLI

```
pgmemory start                    Start the daemon
pgmemory mcp                      Start as MCP server
pgmemory status                   Check daemon health
pgmemory search "query"           Search the knowledge base
pgmemory ingest <url>             Ingest a docs site or wiki
pgmemory upload <path>            Upload files to seed memory
pgmemory sources                  List ingested sources
pgmemory export                   Export knowledge to markdown
pgmemory forget <id>              Delete a specific item
pgmemory wipe                     Clear the entire store
pgmemory version                  Print version
pgmemory credentials              Manage stored credentials
```

## Configuration

Everything has sensible defaults. Embedded PostgreSQL starts automatically — no configuration required for solo use.

```yaml
# ~/.pgmemory/config.yaml

# For team use, provide a shared PostgreSQL connection string:
# postgres_url: "postgres://user:pass@host:5432/pgmemory?sslmode=require"
# When omitted, embedded PostgreSQL is used automatically.

port: 7432                                # Local proxy port
retrieval_top_k: 5                        # Knowledge items per query
retrieval_max_tokens: 2048                # Context budget

steward:
  interval_minutes: 60
  prune_threshold: 0.1
  merge_threshold: 0.88
  decay_half_days: 90

pipeline:
  ingest_min_len: 80                      # Skip short responses before LLM call
  content_score_pre_gate: 0.35            # Adaptive noise score threshold
```

See the full **[Configuration Reference](https://jeff-vincent.github.io/pgmemory/configuration)**.

## Development

```bash
# Build & test
make build
go test -race ./...

# Build macOS tray app
make app

# Docs site
cd website && npm start
```

## Roadmap

- [x] Transparent Anthropic proxy with streaming
- [x] Local embeddings (voyage-4-nano via llama.cpp)
- [x] MCP server (10 tools, any agent)
- [x] Source ingestion (crawl docs/wikis)
- [x] Quality maintenance (scoring, pruning, merging)
- [x] Hybrid search (pgvector + full-text + RRF + MMR)
- [x] Secret scrubbing (13 detection patterns)
- [x] Adaptive noise filtering (pre-LLM gates, rejection-based learning)
- [x] Atomic fact extraction via LLM synthesis
- [x] Documentation site
- [x] macOS menu bar app
- [x] Embedded PostgreSQL (zero-config local storage)
- [ ] Team-scoped knowledge (overlapping layers per team/BU)
- [ ] OpenAI-compatible endpoint support
- [ ] Multi-provider support (beyond Anthropic)

## License

[MIT](LICENSE)
