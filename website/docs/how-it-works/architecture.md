---
sidebar_position: 5
title: Architecture & Design Decisions
---

# Architecture & Design Decisions

This page is for the technically curious — the design choices behind pgmemory, the specific tools and thresholds selected, and the reasoning behind each.

## System design

pgmemory runs as a local daemon on each developer's machine. It serves two roles simultaneously:

1. **HTTP proxy** — a passthrough proxy between the AI tool and the LLM provider. Forwards requests unmodified and captures responses asynchronously for knowledge storage.
2. **MCP server** — exposes knowledge operations (search, store, delete, ingest) over the Model Context Protocol. This is how AI tools access the knowledge base.

The proxy handles **capture** (write path only). The MCP server handles **retrieval and explicit operations** (read path + write path). Both share the same store and quality system. The daemon binds to `127.0.0.1` only — it's never exposed to the network.

```
┌─────────────────────────────────────────────┐
│              pgmemory daemon                 │
│                                              │
│  ┌──────────┐  ┌───────────┐  ┌──────────┐ │
│  │  Proxy   │  │ MCP Server│  │ Dashboard│ │
│  │ (HTTP)   │  │  (stdio)  │  │  (HTTP)  │ │
│  └────┬─────┘  └─────┬─────┘  └──────────┘ │
│       │               │                      │
│  ┌────▼───────────────▼─────┐               │
│  │    Read Pipeline (MCP)    │               │
│  │  embed → search → return  │               │
│  └──────────────────────────┘               │
│  ┌──────────────────────────┐               │
│  │  Write Pipeline (async)   │               │
│  │  chunk → filter → redact  │               │
│  │  → embed → dedup → store  │               │
│  └──────────────────────────┘               │
│  ┌──────────────────────────┐               │
│  │    Steward (background)   │               │
│  │  score → prune → merge    │               │
│  └──────────────────────────┘               │
│  ┌──────────────────────────┐               │
│  │    Embedder (llama.cpp)   │               │
│  │  voyage-4-nano, port 7433 │               │
│  └──────────────────────────┘               │
└──────────────────┬──────────────────────────┘
                   │
          PostgreSQL + pgvector
     (embedded on 7434, or shared)
```

### Why a local daemon?

- **Latency** — embedding happens locally. No round-trip to a remote embedding service.
- **Privacy** — prompts and responses never leave the machine for embedding. Only post-redaction knowledge enters the store.
- **Simplicity** — for solo use, no infrastructure to manage at all. Embedded PostgreSQL starts automatically.
- **Resilience** — if the daemon is down, the AI tool falls through to the LLM provider directly. No single point of failure.

### Why embedded PostgreSQL?

The default mode runs PostgreSQL as an embedded subprocess (port 7434) with pgvector pre-installed. This means:

- **Zero dependencies** — no Docker, no external database, no cloud account needed
- **Just works** — `pgmemory start` and everything is running
- **Upgrade path** — when you're ready for team sharing, just provide a `postgres_url` and embedded PG is bypassed

The pgvector extension is automatically copied from your Homebrew installation into the embedded PostgreSQL's lib directory.

## Embedding model: voyage-4-nano

pgmemory uses [voyage-4-nano](https://huggingface.co/jsonMartin/voyage-4-nano-gguf) running locally via [llama.cpp](https://github.com/ggerganov/llama.cpp).

**Why this model:**

- **1024-dimensional embeddings** — high enough fidelity for precise similarity matching, small enough for fast local inference
- **~354MB GGUF quantized (Q8_0)** — reasonable download, minimal disk footprint
- **Runs on CPU** — no GPU required. Works on any developer laptop including base-model MacBook Airs
- **Cosine similarity optimized** — trained for retrieval tasks

**Why local, not an API:**

- **Zero marginal cost** — no per-token embedding charges
- **No rate limits** — batch embedding of 50+ chunks doesn't hit API throttles
- **Privacy** — raw conversation text never leaves the machine for embedding

The embedder runs as a subprocess on port 7433, managed by the daemon. Batch embedding sends chunks in HTTP calls to amortize inference overhead.

## Vector similarity thresholds

Three similarity thresholds control how knowledge flows through the system:

### Deduplication threshold: 0.92

When a new chunk's nearest neighbor in the store has cosine similarity ≥ **0.92**, the chunk is skipped as a duplicate.

**Why 0.92:** At 0.90, too many false positives — chunks about the same *topic* but with meaningfully different details were being dropped. At 0.95, near-identical rephrasing slipped through. 0.92 hits the sweet spot.

### Source extension threshold: 0.75

When a new chunk is similar to an existing *source* memory (ingested docs, wikis) at cosine ≥ **0.75**, it's stored as a "source extension" — linked back to the original reference material via metadata.

**Why 0.75:** Intentionally loose. A wiki article about the deploy process and a debugging session about a failed deploy are related at ~0.75-0.85 — different enough to both be valuable, similar enough that the link is meaningful.

### Merge threshold: 0.88

The steward merges near-duplicate items when their cosine similarity is ≥ **0.88**, keeping the one with more retrieval signal.

**Why 0.88:** Lower than the dedup threshold (0.92) because over time, items that were distinct enough to both be stored can converge in meaning. 0.88 catches these convergences while preserving items that cover genuinely different aspects.

## Chunking strategy

Text is split at **paragraph boundaries** (double newlines), with a target of **~512 tokens** (~2048 characters) per chunk.

Logical units stay together — a function explanation, a list of deployment steps, a debugging narrative. Chunks shorter than 20 characters are discarded as noise.

## Quality scoring algorithm

The steward scores each knowledge item using a logarithmic usage signal with exponential time decay:

$$
\text{score} = \frac{\log_2(\text{hitCount} + 1)}{\log_2(\text{maxHits} + 1)} \times 0.5^{\text{daysSinceLastActive} / \text{halfLife}}
$$

The half-life scales with content quality score — high-quality items (as judged at write time) decay more slowly.

## Reciprocal Rank Fusion (RRF)

Vector and text search results are fused using RRF with smoothing constant $k = 60$:

$$
\text{score}(d) = \sum_{L \in \{vector, text\}} \frac{1}{\text{rank}_L(d) + k + 1}
$$

**Why RRF:** It's rank-based, not score-based. Vector similarity (0-1) and full-text relevance (unbounded) live on completely different scales. RRF sidesteps the calibration problem.

## Maximal Marginal Relevance (MMR)

After RRF fusion, results are re-ranked for diversity:

$$
\text{MMR}(d) = \lambda \cdot \text{relevance}(d) - (1 - \lambda) \cdot \max_{d_j \in S} \text{sim}(d, d_j)
$$

with $\lambda = 0.7$ (70% relevance, 30% diversity).

## Secret redaction

13 regex patterns scrub sensitive content *before* embedding. Secrets never enter the vector store. Even if someone extracts the raw embeddings, they can't reverse-engineer redacted content.

| Category | Patterns |
|---|---|
| Cloud credentials | AWS access keys (`AKIA...`), AWS secret keys |
| Platform tokens | GitHub (`ghp_`, `gho_`, PATs), Slack (`xox...`), Stripe (`sk_live_...`) |
| Cryptographic material | Private key blocks, SSH keys, JWTs (`eyJ...`) |
| Connection strings | Database URIs with embedded passwords |
| Generic | Bearer tokens, key-value pairs with `password`, `secret`, `token`, `api_key` |
