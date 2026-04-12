---
sidebar_position: 4
title: Hybrid Search
---

# Hybrid Search

pgmemory uses a multi-stage search pipeline that combines pgvector cosine similarity with PostgreSQL full-text search for high-quality retrieval.

## How the hybrid pipeline works

```
Query → [Vector Search (pgvector)] + [Full-Text Search (tsvector)] → RRF Fusion → MMR Re-ranking → Top-K
```

### 1. Meaning-based search (semantic)

The query embedding is compared against stored knowledge using pgvector's HNSW index with cosine distance:

- **Quality pre-filtering** — items with `quality_score < 0.05` are excluded (new, unscored items are kept)
- **Source scoping** — optional prefix filter on the `source` field restricts results to specific knowledge sources
- **Oversampling** — fetches extra candidates to give the fusion and diversity stages a rich pool

pgmemory uses an HNSW index (m=16, ef_construction=64) for fast approximate nearest neighbor search.

### 2. Keyword-based search (lexical)

A parallel PostgreSQL full-text search runs using `ts_rank` and `plainto_tsquery` against a GIN-indexed `tsvector` column. This catches exact keyword matches — acronyms, error codes, class names, specific config values — that embedding similarity alone might not rank highly.

### 3. Reciprocal Rank Fusion (RRF)

The two result lists are combined using RRF with smoothing constant $k = 60$:

$$
\text{score}(d) = \sum_{L \in \{vector, text\}} \frac{1}{\text{rank}_L(d) + k + 1}
$$

RRF is rank-based, not score-based, so it works naturally across the different scoring scales of vector similarity (0-1) and full-text relevance (unbounded). An item ranked highly by both searches scores highest; an item found by only one search still appears.

### 4. Maximal Marginal Relevance (MMR)

The fused results are re-ranked to maximize diversity:

$$
\text{MMR}(d) = \lambda \cdot \text{relevance}(d) - (1 - \lambda) \cdot \max_{d_j \in S} \text{sim}(d, d_j)
$$

With $\lambda = 0.7$ — 70% weight on relevance, 30% on diversity. This prevents the top results from being five variations of the same knowledge. Instead, the AI tool gets context from *different angles* — a deployment procedure, a related debugging insight, and an architecture decision.

## Why hybrid?

Consider a developer asking about `ERR_CONN_REFUSED`. A meaning-based search finds knowledge about connection errors in general — useful, but not specific. A keyword-based search finds the exact item that mentions that precise error code. Hybrid search combines both signals to deliver the best of each.

This matters especially for teams: the shared knowledge store contains a mix of high-level architectural context and specific technical details. Hybrid search surfaces both.

## What this means in practice

- **Specific technical details** (error codes, config values, API endpoints) are found even when the question is phrased broadly — thanks to full-text search
- **Conceptual knowledge** (architecture decisions, design rationale) is found even when the question uses different terminology — thanks to vector search
- **Results are diverse** — the AI tool gets context from multiple angles, not five versions of the same thing — thanks to MMR
- **Low-quality noise is filtered out** — knowledge that was captured but never proved useful doesn't clutter results — thanks to quality pre-filtering

## PostgreSQL indexes

pgmemory creates three indexes on the `memories` table:

| Index | Type | Purpose |
|---|---|---|
| `memories_embedding_idx` | HNSW (pgvector, cosine) | Fast approximate nearest neighbor for vector search |
| `memories_content_fts` | GIN (tsvector) | Full-text search on content |
| `memories_source_idx` | B-tree | Source prefix filtering |

These are created automatically during table migration — no manual setup required.

See [Architecture & Design Decisions](architecture) for the technical rationale behind the specific thresholds and algorithms.
