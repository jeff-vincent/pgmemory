---
sidebar_position: 1
title: MCP Server
---

# MCP Server

The MCP (Model Context Protocol) server is how pgmemory connects to **any AI coding tool** — Cursor, Windsurf, Cline, Claude Code, custom pipelines, or anything that speaks MCP. It's the universal integration point.

## Setup

Add pgmemory to your tool's MCP configuration:

```json
{
  "mcpServers": {
    "pgmemory": {
      "command": "pgmemory",
      "args": ["mcp"]
    }
  }
}
```

This works with Claude Code, Cursor, Windsurf, Cline, and any tool that supports MCP servers. The pgmemory daemon must be running (`pgmemory start`) for MCP tools to work.

## Available tools

The MCP server provides 10 tools:

### Core knowledge operations

| Tool | What it does |
|---|---|
| `memory_search` | Search the knowledge base with a natural language query |
| `memory_store` | Store new knowledge (auto-deduplicated, noise-filtered, secrets scrubbed) |
| `memory_list` | Browse stored knowledge, optionally filtered by text |
| `memory_delete` | Remove a specific knowledge item |

### Source ingestion

| Tool | What it does |
|---|---|
| `source_ingest` | Crawl a URL (wiki, docs site) and add its content to the knowledge base |
| `source_upload` | Upload files directly into memory |
| `source_list` | List all ingested sources and their status |
| `source_remove` | Remove a source and all its associated knowledge |

### Monitoring

| Tool | What it does |
|---|---|
| `quality_stats` | Check the knowledge base health — retrieval counts, learning status |
| `database_list` | List connected databases and roles |

## How it fits together

```
Any AI tool ←→ MCP (stdin/stdout) ←→ pgmemory ←→ PostgreSQL + pgvector
```

The MCP server is the **full knowledge management interface** — read, write, update, and delete:

- **Search** (`memory_search`) — runs the [retrieval pipeline](../how-it-works/read-path) with hybrid search, quality filtering, and diversity optimization
- **Store** (`memory_store`) — adds new knowledge through the [capture pipeline](../how-it-works/write-path) with noise filtering, secret scrubbing, and deduplication
- **Delete** (`memory_delete`) — removes outdated or incorrect knowledge items
- **Browse** (`memory_list`) — lists stored knowledge

The proxy handles **automatic capture** from conversations. MCP handles **everything else**.

## The good citizen pattern

AI tools that connect via MCP should actively maintain knowledge quality. When a tool retrieves knowledge via `memory_search` and discovers that the information is **outdated, inaccurate, or no longer relevant**, it should correct the record:

1. **Delete** the outdated item with `memory_delete`
2. **Store** the corrected version with `memory_store`

This is the expected behavior. AI tools using pgmemory should treat knowledge maintenance as part of their workflow.

## Read-only usage

Any tool can connect via MCP with `--read-only` and **only use `memory_search`** — consuming knowledge without contributing or modifying:

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

See [Read-Only Mode](read-only-mode) for more on this pattern.

## For teams using multiple tools

A common team setup:

| Team member | Tool | Connection | Contribution |
|---|---|---|---|
| Alice | Claude Code | Proxy + MCP | Automatic capture via proxy, retrieval + maintenance via MCP |
| Bob | Cursor | MCP | Agent searches, stores, and maintains knowledge |
| Carol | Custom pipeline | MCP (read-only) | Reads team knowledge, doesn't write or modify |
| Dave | Claude Code + Cursor | Proxy + MCP | Full capture + full knowledge management |

All four draw from and contribute to the same PostgreSQL knowledge store.
