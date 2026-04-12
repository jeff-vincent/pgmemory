---
slug: /
sidebar_position: 1
title: What is pgmemory?
---

# pgmemory

**Persistent memory for AI coding agents — backed by PostgreSQL + pgvector.**

Every AI coding session starts cold. The agent doesn't remember the architecture decisions from last sprint, the deployment procedure your team refined over months, or the workaround for that edge case someone debugged last Tuesday. Teams compensate by re-explaining context, maintaining wikis no one updates, and hoping institutional knowledge doesn't walk out the door.

pgmemory changes this. It's a lightweight daemon that sits alongside your AI coding tools, transparently capturing knowledge from every session and making it available in the future. Over time, you build an always-current knowledge base — without anyone stopping to write documentation.

```
Your AI Tools → pgmemory → LLM Provider
                    ↕
           PostgreSQL + pgvector
     (embedded or shared instance)
```

## How it works

By default, pgmemory runs an **embedded PostgreSQL** instance with pgvector — no external database required. Install, start, and work normally. Knowledge accumulates automatically.

For teams, point everyone at a **shared PostgreSQL instance** (any provider: RDS, Neon, Supabase, Aiven, self-hosted). Each person works normally with their preferred AI tool. Knowledge accumulates organically from daily work and is available to everyone.

**No one writes documentation. No one maintains a wiki. Knowledge builds itself.**

| Role | How they benefit |
|------|-----------------|
| **Engineers** | Their AI tools have context from past sessions — theirs and their teammates'. Less re-explaining, fewer repeated mistakes. |
| **Engineering Managers** | Institutional knowledge is retained even through team turnover. Onboarding accelerates as new hires inherit accumulated context. |
| **Platform / DevOps** | Operational knowledge — deployment procedures, incident resolutions, infrastructure quirks — persists and spreads. |

→ [Team Knowledge Hub](team-knowledge-hub) explores the team vision in depth.

## Works with any AI coding tool

pgmemory is **tool-agnostic**. It connects to your tools through two interfaces:

| Interface | Best for | How it works |
|-----------|----------|-------------|
| **[Proxy mode](agents/proxy-mode)** | Claude Code, any Anthropic-based tool | Passthrough — set one environment variable, work normally. Conversations captured automatically in the background. |
| **[MCP server](agents/mcp-server)** | Cursor, Windsurf, Cline, custom tools | Standard protocol — agent searches, stores, and maintains knowledge via tool calls. |

Teams don't need to standardize on one tool. Alice uses Claude Code, Bob uses Cursor, Carol has a custom pipeline — they all feed and draw from the same knowledge store. There's also a **[read-only mode](agents/read-only-mode)** for tools that should consume knowledge without contributing.

## What pgmemory handles automatically

1. **[Knowledge capture](how-it-works/write-path)** — Every AI interaction is automatically filtered through multi-stage noise detection (adaptive content scoring, LLM quality gates), scrubbed of secrets (API keys, tokens, passwords), deduplicated, and stored. The system learns what noise looks like from your specific patterns.

2. **[Knowledge retrieval](how-it-works/read-path)** — AI tools access the knowledge base through MCP tools — searching, storing, and updating knowledge. When a tool receives outdated information, it corrects the record.

3. **[Quality maintenance](how-it-works/quality-loop)** — A background process continuously scores knowledge by how useful it's been, removes noise, and merges near-duplicates. The store stays clean without manual curation.

4. **[Hybrid search](how-it-works/hybrid-search)** — Retrieval combines pgvector cosine similarity with PostgreSQL full-text search, fused via Reciprocal Rank Fusion with diversity re-ranking. Finds both semantically related and exact-match results.

## Getting started

For solo use, install and start — embedded PostgreSQL handles everything:

1. **Install** pgmemory
2. **Run** `pgmemory start`
3. **Connect** your AI tool

For teams, one person provisions a shared PostgreSQL instance, then each team member installs pgmemory and connects.

→ [Getting Started](getting-started) has the full setup guide.
