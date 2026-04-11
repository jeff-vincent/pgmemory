# memoryd — Postgres Migration Spec

## Status Quo

| Component | Current | Notes |
|-----------|---------|-------|
| **Vector store** | MongoDB Atlas | `$vectorSearch` (cosine, 1024-dim), `$search` (Lucene full-text), RRF hybrid, MMR re-ranking |
| **Embedding** | voyage-4-nano via local llama-server | GGUF Q8_0, 1024-dim, llama.cpp subprocess on port 7433. **Keeping this.** |
| **LLM synthesis** | Anthropic Haiku | HTTP calls to `/v1/messages`. Bedrock planned. Azure removed. |
| **Credential storage** | macOS Keychain / Linux libsecret | `mongodb_atlas_uri`, `anthropic_api_key` |
| **Multi-database** | Fan-out search across N MongoDB databases | **Replacing with optional team Postgres.** |

---

## Scope

Replace MongoDB Atlas with PostgreSQL + pgvector. Two modes:

1. **Local (default):** Embedded Postgres via `embedded-postgres-go`. Zero external dependencies — launch memoryd, it works. Solo developer, single machine.
2. **Team:** Connect to any cloud-hosted Postgres with pgvector — AWS RDS, Neon, Supabase, Aiven, etc. Shared knowledge base across a team. The user provides a connection string, memoryd connects to it instead of starting embedded Postgres.

Both modes use the same `PostgresStore` implementation, same schema, same queries. The only difference is connection lifecycle.

---

## Replacement Targets

### 1. Vector Store: MongoDB Atlas → PostgreSQL + pgvector

**Two connection modes:**

| Mode | Config | Lifecycle |
|------|--------|-----------|
| **Local** | No config needed (default) | memoryd starts/stops embedded Postgres automatically. Data in `~/.memoryd/data/pg/`. |
| **Team** | `postgres_url: "postgres://..."` | memoryd connects to external Postgres. User provisions the DB with pgvector enabled. |

**Why Postgres + pgvector:**
- pgvector is mature, supports HNSW indexes with cosine distance.
- Full-text search is built-in (`tsvector` + `ts_rank`), replaces Atlas Lucene.
- Every cloud provider offers managed Postgres with pgvector: RDS, Neon (free tier), Supabase, Aiven, Timescale.
- Embedded mode via `embedded-postgres-go` gives zero-dependency local experience.
- Same SQL, same driver, same code path regardless of where Postgres runs.

**Schema:**

```sql
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE memories (
    id              BIGSERIAL PRIMARY KEY,
    content         TEXT NOT NULL,
    embedding       vector(1024) NOT NULL,
    source          TEXT NOT NULL DEFAULT '',
    quality_score   DOUBLE PRECISION NOT NULL DEFAULT 0,
    content_score   DOUBLE PRECISION NOT NULL DEFAULT 0,
    hit_count       INTEGER NOT NULL DEFAULT 0,
    last_retrieved  TIMESTAMPTZ,
    metadata        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX memories_embedding_idx ON memories
    USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);

CREATE INDEX memories_source_idx ON memories (source);
CREATE INDEX memories_content_fts ON memories USING gin (to_tsvector('english', content));

CREATE TABLE retrieval_events (
    id          BIGSERIAL PRIMARY KEY,
    memory_id   BIGINT REFERENCES memories(id) ON DELETE CASCADE,
    score       DOUBLE PRECISION NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sources (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    base_url      TEXT,
    status        TEXT NOT NULL DEFAULT 'pending',
    page_count    INTEGER NOT NULL DEFAULT 0,
    memory_count  INTEGER NOT NULL DEFAULT 0,
    headers       JSONB,
    last_crawled  TIMESTAMPTZ
);

CREATE TABLE source_pages (
    id            BIGSERIAL PRIMARY KEY,
    source_id     BIGINT REFERENCES sources(id) ON DELETE CASCADE,
    url           TEXT NOT NULL,
    content_hash  TEXT NOT NULL,
    last_fetched  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

**Vector search query:**
```sql
SELECT id, content, source, metadata, created_at,
       1 - (embedding <=> $1) AS score
FROM memories
WHERE quality_score >= $2 OR quality_score = 0
ORDER BY embedding <=> $1
LIMIT $3;
```

**Hybrid search (RRF):** Two SQL queries (vector + full-text), merge in Go with existing RRF/MMR code.

```sql
-- Full-text leg
SELECT id, content, source,
       ts_rank(to_tsvector('english', content), plainto_tsquery('english', $1)) AS text_score
FROM memories
WHERE to_tsvector('english', content) @@ plainto_tsquery('english', $1)
ORDER BY text_score DESC
LIMIT $2;
```

**Go dependencies:**
- `github.com/fergusstrange/embedded-postgres` — embedded Postgres lifecycle
- `github.com/jackc/pgx/v5` — Postgres driver
- `github.com/pgvector/pgvector-go` — vector type support

**Embedded Postgres lifecycle (local mode only):**
```go
// Start on daemon boot, data dir persists across restarts
epg := embeddedpostgres.NewDatabase(
    embeddedpostgres.DefaultConfig().
        Port(7434).
        DataPath(filepath.Join(configDir, "data", "pg")).
        RuntimePath(filepath.Join(configDir, "data", "pg-runtime")).
        BinariesPath(filepath.Join(configDir, "data", "pg-bin")),
)
epg.Start()
defer epg.Stop()
```

**Team mode connection:**
```go
// Connect to external Postgres — user provides the URL
pool, err := pgxpool.New(ctx, cfg.PostgresURL)
```

**Mode selection logic:**
```go
if cfg.PostgresURL != "" {
    // Team mode: connect to external Postgres
    pool, err := pgxpool.New(ctx, cfg.PostgresURL)
} else {
    // Local mode: start embedded Postgres, connect to localhost:7434
    epg := embeddedpostgres.NewDatabase(...)
    epg.Start()
    pool, err := pgxpool.New(ctx, "postgres://localhost:7434/memoryd?sslmode=disable")
}
// Same PostgresStore from here — doesn't know or care where Postgres is running
```

---

### 2. Embedding: No Change

Keep voyage-4-nano via llama-server. Works, fast, local, free.

---

### 3. LLM Synthesis: Anthropic Only (Bedrock planned)

Azure OpenAI backend has been removed. Anthropic Haiku is the only synthesis backend. Bedrock support is planned as a future addition.

---

### 4. Multi-Database: Replace with Team Postgres

The current MongoDB multi-database fan-out is removed. In its place: **optional team Postgres**.

**How it works:**
- By default, memoryd runs in local mode with embedded Postgres. No config needed.
- To share a knowledge base across a team, set `postgres_url` in config (or via the dashboard) to point at any cloud Postgres with pgvector: AWS RDS, Neon, Supabase, Aiven, etc.
- The user is responsible for provisioning the Postgres instance and enabling pgvector. memoryd runs schema migrations on first connect.
- One connection string, one shared database. No fan-out, no role-based multi-db.

**Delete:**
- `internal/store/multi.go` — `MultiStore`, `DatabaseEntry`, fan-out search
- `internal/store/atlas.go` — Atlas-specific `$vectorSearch` / `$search` / hybrid search
- `config.Databases []DatabaseConfig` — multi-db config array
- `DatabaseConfig`, `RoleFull`, `RoleReadOnly` constants

**Rework:**
- Dashboard "Team Databases" page → replace with a simple "Database" section in Settings (shows local vs. team mode, connection string field for team mode)
- `/api/databases`, `/api/databases/` endpoints → remove; database config moves into `/api/settings`

The `store.Store` interface stays. `PostgresStore` implements it directly — no multi-store wrapper.

---

### 5. Credential Storage: Simplify

**Remove:** `mongodb_atlas_uri` keychain entry

**Keep:**
- `anthropic_api_key` (LLM synthesis)
- `postgres_url` (team mode only — stored in keychain since it contains credentials)
- Dashboard API token

**Removed:** `azure_openai_api_key`

Local mode needs no database credentials — embedded Postgres uses localhost with no auth.

---

### 6. Config: Simplify

```yaml
port: 7432
mode: "proxy"                    # "proxy" | "mcp" | "mcp-readonly"
model_path: "~/.memoryd/models/voyage-4-nano.gguf"
embedding_dim: 1024
retrieval_top_k: 5
retrieval_max_tokens: 2048
upstream_anthropic_url: "https://api.anthropic.com"
llm_synthesis: true

# Leave empty for local embedded Postgres (default).
# Set to a connection string for team/cloud Postgres.
postgres_url: ""

steward:
  interval_minutes: 60
  prune_threshold: 0.1
  merge_threshold: 0.88
  decay_half_days: 90

pipeline:
  noise_min_len: 40
  noise_min_alnum_ratio: 0.40
  ingest_min_len: 80
  content_score_pre_gate: 0.35
  content_score_gate: 0.0
  dedup_threshold: 0.92
  source_extension_threshold: 0.75
  topic_boundary_threshold: 0.65
  max_group_chars: 2048

prompts:
  qa: ""
  merge: ""
  conversation: ""
```

Gone: `mongodb_atlas_uri`, `mongodb_database`, `databases`, `atlas_mode`, `azure`.

---

## Migration Sequence

| Phase | Work |
|-------|------|
| **1. Postgres connection layer** | Add `embedded-postgres-go` + `pgxpool`. Mode selection: if `postgres_url` is set → connect to it, else → start embedded Postgres. Auto-run schema migration on first connect. Data in `~/.memoryd/data/pg/` for local mode. |
| **2. PostgresStore** | Implement `store.Store` + `store.SearchStore` + `store.QualityStore` for pgx/pgvector. Port vector search, full-text, RRF/MMR. Same implementation for both local and team mode. |
| **3. Remove multi-db** | Delete `multi.go`, `atlas.go`, database config array, `DatabaseConfig` type. Remove `/api/databases` endpoints. |
| **4. Remove MongoDB** | Delete `mongo.go`, drop `go.mongodb.org/mongo-driver` from `go.mod`, remove `mongodb_atlas_uri` from config/credentials. |
| **5. Dashboard update** | Remove Team Databases page. Add "Database" section to Settings: radio toggle (Local / Team), connection string input for Team mode, connection test button. |
| **6. Migration tool** | `memoryd migrate` — connect to a MongoDB URI, read all memories, write to Postgres (local or team). One-shot, optional. |
| **7. macOS installer** | Replace `curl | bash` with a proper macOS installer app. See below. |

---

## 7. macOS Installer / Onboarding

**Current state:** One-line `curl | bash` script. Works but feels like a dev tool, not a product.

**Goal:** A proper macOS `.app` installer (or `.dmg` with setup wizard) that walks the user through first-run configuration.

**Onboarding flow:**

1. **Welcome screen** — "memoryd — persistent memory for your AI tools"
2. **Database mode** — Two-card picker:
   - **Local** (default, recommended) — "Everything stays on your machine. No account needed." Embedded Postgres, zero config.
   - **Team** — "Share a knowledge base with your team." Prompts for a Postgres connection string. "Test Connection" button. Supports any provider: Neon, RDS, Supabase, etc.
3. **Anthropic API key** — Input field + "Get a key" link. Optional — synthesis works without it, just no quality gating.
4. **Install** — Copies `Memoryd.app` to `/Applications`, installs the `memoryd` CLI to `/usr/local/bin`, downloads the voyage-4-nano model if not present, writes initial config to `~/.memoryd/config.yaml`.
5. **Done** — "memoryd is running." Link to open the dashboard. Quick-start instructions for connecting to VS Code / Claude Code.

**Implementation options:**
- **SwiftUI setup assistant** — Native macOS feel. Bundle as the first-launch flow inside `Memoryd.app`. Runs once, then the app drops into normal tray mode.
- **Electron/Tauri wrapper** — If SwiftUI is too heavy for a setup wizard. Tauri is lighter.
- **Web-based** — Dashboard already exists. Could serve a `/setup` route on first launch that redirects to the dashboard setup wizard. Lowest effort, cross-platform.

**Recommendation:** Web-based `/setup` route. The dashboard is already an embedded SPA — add a setup wizard page that only appears when `~/.memoryd/config.yaml` doesn't exist (first run). No new dependencies, works on Linux too. The `curl | bash` install script still handles binary placement; the setup wizard handles _configuration_.
