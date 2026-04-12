---
sidebar_position: 6
title: Configuration
---

# Configuration

pgmemory reads configuration from `~/.pgmemory/config.yaml`. For solo use, no configuration is needed — embedded PostgreSQL starts automatically with sensible defaults.

## Solo use (zero config)

Just run `pgmemory start`. Embedded PostgreSQL starts on port 7434, the proxy listens on 7432, and everything works.

## Team use

The only required setting is the shared PostgreSQL connection string:

```yaml
postgres_url: "postgres://team-user:password@your-host:5432/pgmemory?sslmode=require"
```

Or store it securely in your OS keychain:
```bash
pgmemory credentials set-postgres-url
```

The config file will reference it as `postgres_url: "keychain:pgmemory/postgres_url"`.

## Full reference

```yaml
# PostgreSQL connection string. When omitted, embedded PostgreSQL is used.
# postgres_url: "postgres://user:pass@host:5432/pgmemory?sslmode=require"

# Local proxy port (default: 7432)
port: 7432

# Path to the local embedding model (auto-downloaded on first run)
model_path: "~/.pgmemory/models/voyage-4-nano.gguf"

# Embedding dimensions — must match the vector index (default: 1024)
embedding_dim: 1024

# Number of knowledge items retrieved per query (default: 5)
retrieval_top_k: 5

# Maximum tokens of context returned per search (default: 2048)
retrieval_max_tokens: 2048

# Upstream LLM provider URL (default: https://api.anthropic.com)
upstream_anthropic_url: "https://api.anthropic.com"

# Enable LLM-based synthesis for higher quality memory extraction
# Requires an Anthropic API key (set via tray app or credentials command)
llm_synthesis: false

# Quality maintenance settings
steward:
  interval_minutes: 60       # How often the quality sweep runs
  prune_threshold: 0.1       # Score below which low-value items are removed
  grace_period_hours: 24     # Minimum age before removal is considered
  decay_half_days: 90        # How quickly unused knowledge loses relevance
  merge_threshold: 0.88      # Similarity threshold for deduplication
  batch_size: 500            # Items processed per sweep

# Write pipeline noise filtering
pipeline:
  ingest_min_len: 80              # Responses shorter than this skip LLM synthesis
  content_score_pre_gate: 0.35    # Adaptive noise score threshold (0 = disabled)
```

## Tuning

The defaults work well for most use cases. If you need to adjust:

| Scenario | What to change |
|---|---|
| **Large team (10+ contributors)** | Lower `interval_minutes` to 30, increase `batch_size` to 1000 |
| **Want to retain knowledge longer** | Increase `decay_half_days` beyond 90 |
| **Seeing too many near-duplicates** | Lower `merge_threshold` to 0.85 |
| **Want less aggressive cleanup** | Lower `prune_threshold` to 0.05, increase `grace_period_hours` to 72 |
| **Heavy use of ingested sources** | Increase `batch_size` to 2000, lower `interval_minutes` to 30 |
| **Too much noise getting stored** | Lower `content_score_pre_gate` to 0.30 (more aggressive filtering) |
| **Valuable short responses being dropped** | Lower `ingest_min_len` to 40 |
| **Want to disable adaptive filtering** | Set `content_score_pre_gate` to 0 |

## Environment variables

The only environment variable relevant to pgmemory is the one that connects your AI tool to the proxy:

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:7432
```

This tells Claude Code (or any Anthropic-compatible tool) to route through pgmemory. pgmemory itself reads everything from `config.yaml` and the OS keychain.
