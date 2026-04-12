---
sidebar_position: 3
title: Read-Only Mode
---

# Read-Only Mode

Not every tool or team member needs to write to the knowledge base. Some should just benefit from what's already there.

## When to use read-only

- **New team members** — their AI tools immediately have access to accumulated knowledge, without contributing until they're comfortable
- **Evaluation** — trying pgmemory before committing? Connect read-only and see what the retrieval quality looks like
- **Security-sensitive contexts** — some environments have policies about what AI tools can store
- **Non-engineering tools** — a PM's AI assistant can search the technical knowledge base without writing back
- **Cross-team consumers** — teams that want to consume another team's knowledge without mixing their own context in

## Setup

```json
{
  "mcpServers": {
    "pgmemory": {
      "command": "pgmemory",
      "args": ["mcp", "--read-only"]
    }
  }
}
```

The `--read-only` flag restricts the MCP server to search-only tools. Write operations (`memory_store`, `source_ingest`, etc.) are disabled.

## What the tool sees

When the AI searches the knowledge base, it gets formatted results:

```
[1] (source: claude-code, relevance: 0.87)
The payment service validates webhook signatures using HMAC-SHA256...

[2] (source: source:internal-wiki, relevance: 0.82)
Deployments to production require approval from the #releases channel...
```

Knowledge from proxy sessions, MCP writes, and ingested sources — all surfaced through a single search.

## Participation spectrum

pgmemory supports a range of participation levels:

| Level | How | Best for |
|---|---|---|
| **Full (proxy)** | Automatic capture + retrieval | Engineers using Claude Code who want zero-effort contribution |
| **Full (MCP)** | Agent-controlled search, store, and maintenance | Engineers using Cursor, Windsurf, custom tools |
| **Read-only** | Search only, no writes | New hires, evaluators, PMs, cross-team consumers |
| **Isolated** | Separate database | Anyone who needs a private knowledge store |

There's no forced contribution. The value of the shared store is strong enough that most people opt into full participation voluntarily.
