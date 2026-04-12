# pgmemory — Codebase Reference for Claude Code

Module: `github.com/jeff-vincent/pgmemory`
Go version: 1.26+
Config: `~/.pgmemory/config.yaml`

## CLI Commands

```
pgmemory start      Start daemon (foreground). Embedded PostgreSQL starts automatically.
pgmemory mcp        Start as MCP stdio server (for Claude Code MCP integration)
pgmemory status     Ping health endpoint
pgmemory search     Regex search on memory content
pgmemory forget     Delete one memory by hex ID
pgmemory wipe       Delete all memories (confirmation required)
pgmemory env        Print ANTHROPIC_BASE_URL export
pgmemory version    Print version
pgmemory ingest     Crawl a URL and store as source
pgmemory upload     Upload files/directory to seed memory
pgmemory sources    List ingested sources
pgmemory export     Export memories to markdown
pgmemory credentials  Manage stored credentials (set-postgres-url, set-api-key, clear, check)
pgmemory token      Print the dashboard API token
```

## Build & Test

```bash
make build              # → bin/pgmemory
make app                # → bin/Pgmemory.app (macOS tray app)
go test ./...           # all unit tests (no external deps needed)
go vet ./...            # static analysis
```

## Architecture

- **Storage:** PostgreSQL + pgvector (embedded on port 7434, or shared instance via `postgres_url`)
- **Embeddings:** voyage-4-nano Q8_0 GGUF (1024-dim), local llama.cpp on port 7433
- **Proxy:** port 7432, passthrough to Anthropic API, async capture
- **MCP:** 10 tools (search, store, list, delete, ingest, upload, source_list, source_remove, quality_stats, database_list)
- **Search:** Hybrid — pgvector HNSW cosine + PostgreSQL full-text (GIN tsvector) → RRF fusion → MMR diversity re-ranking

## Conventions

- Standard Go: `gofmt`, `go vet`, no external linters
- Interfaces defined in the package that uses them
- Functional options pattern for configuration (e.g., `proxy.WithStore()`)
- Errors logged at the boundary, not propagated through async paths
- Unit tests use in-memory mocks, test files live next to their code
- Write pipeline runs in goroutines — errors logged, never returned to caller
- `redact.Clean()` strips secrets BEFORE embedding — secrets never enter the vector store
- Daemon binds to 127.0.0.1 only

## Gotchas

1. **Embedding dim is 1024, not 512.** voyage-4-nano produces 1024-dim vectors. The vector index must match.
2. **New memories have quality_score 0.** Search uses a 0.05 min threshold but keeps unscored (new) items.
3. **SSE streaming.** The proxy buffers the full response for the write path while streaming to the client. Don't break the streaming path for write-path changes.
4. **Config path expansion.** `~` in `model_path` is expanded by the config loader. Use the config package's path handling.
5. **Content score pre-gate does NOT feed rejection store.** Only QuickFilter and synthesizer rejections feed back. Prevents positive feedback loop.
6. **pgvector extension.** Embedded PostgreSQL requires pgvector copied from Homebrew. `ensurePgvector()` handles this automatically.
7. **Credentials.** Anthropic API key and Postgres URL stored in OS keychain, referenced from config as `keychain:pgmemory/...`.
