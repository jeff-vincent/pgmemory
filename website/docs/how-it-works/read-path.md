---
sidebar_position: 1
title: How Knowledge is Retrieved
---

# How Knowledge is Retrieved

AI tools access the knowledge base through pgmemory's **MCP server**. When an AI tool calls `memory_search`, pgmemory finds the most relevant knowledge and returns it directly to the tool.

## The flow

```
AI tool calls memory_search → pgmemory finds relevant knowledge → results returned to the tool
```

### 1. Understand the query

When an AI tool calls `memory_search` with a query, pgmemory converts it into a mathematical representation (an embedding) that captures its meaning — not just keywords, but concepts.

This happens locally on every machine using a lightweight model. No data leaves the machine for this step.

### 2. Search the store

pgmemory runs **hybrid search** — combining pgvector cosine similarity with PostgreSQL full-text search, fused via Reciprocal Rank Fusion (RRF), and re-ranked for diversity via MMR. This works the same whether you're using embedded PostgreSQL or a shared instance.

The search also filters out low-quality items automatically — noise that was captured but never proved useful doesn't clutter the results.

See [Hybrid Search](hybrid-search) for how the search pipeline works.

### 3. Return results

The most relevant knowledge items are returned to the AI tool via MCP. The tool receives the actual content — past debugging sessions, architecture decisions, deployment procedures — and can use it as context for its current task.

A result limit (default: 5 items, configurable via `retrieval_top_k`) controls how many items are returned per search.

### 4. Quality feedback

When knowledge items are retrieved, pgmemory records that they were useful. Over time, items that are retrieved frequently earn higher quality scores. Items that are never retrieved eventually decay and are cleaned up by the [quality maintenance](quality-loop) process.

This creates a virtuous cycle: the more you use pgmemory, the better the knowledge quality becomes.
