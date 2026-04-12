---
sidebar_position: 2
title: Getting Started
---

# Getting Started

pgmemory works out of the box with **embedded PostgreSQL** — no external database needed. For teams sharing a knowledge base, you can point it at a shared PostgreSQL instance.

## Solo use (embedded PostgreSQL)

### What you'll need

- **macOS or Linux** (arm64 or amd64)
- **An AI coding tool** (Claude Code, Cursor, Windsurf, etc.)

### Step 1: Install pgmemory

**One-line install (macOS):**
```bash
curl -fsSL https://raw.githubusercontent.com/jeff-vincent/pgmemory/main/install.sh | bash
```

This installs the pgmemory binary, downloads the local embedding model (~354MB), and creates a default config file.

**From source:**
```bash
git clone https://github.com/jeff-vincent/pgmemory.git
cd pgmemory
make build    # → bin/pgmemory
```

### Step 2: Start pgmemory

```bash
pgmemory start
```

On first launch, pgmemory starts an embedded PostgreSQL instance (port 7434) with pgvector, downloads the embedding model, and begins listening for connections. Everything is automatic.

### Step 3: Connect your AI tools

**Claude Code (proxy mode — fully automatic):**
```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:7432
```

Every Claude Code session now automatically captures and retrieves knowledge. No other changes needed.

**Cursor, Windsurf, or other MCP-compatible tools:**

Add pgmemory as an MCP server in your tool's config:

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

See [Connecting Your Tools](agents/mcp-server) for detailed setup per tool.

### Step 4: Verify it's working

```bash
pgmemory status        # confirms the daemon is running
pgmemory search "test" # searches the knowledge store
```

Visit the built-in dashboard at `http://localhost:7432` to see memories accumulating, quality stats, and knowledge sources.

---

## Team use (shared PostgreSQL)

For teams, everyone connects to the same PostgreSQL instance. One person does the initial setup.

### What you'll need

- **A PostgreSQL instance with pgvector** — any provider works:
  - [Neon](https://neon.tech) (serverless, free tier)
  - [Supabase](https://supabase.com) (free tier)
  - AWS RDS for PostgreSQL
  - [Aiven](https://aiven.io)
  - Self-hosted with pgvector extension
- **Each team member's machine needs:**
  - pgmemory installed
  - An AI coding tool

### Step 1: Set up PostgreSQL

Create a database and enable pgvector:

```sql
CREATE DATABASE pgmemory;
\c pgmemory
CREATE EXTENSION IF NOT EXISTS vector;
```

pgmemory will create its tables (`memories`, `sources`, `source_pages`, `retrieval_events`) automatically on first connection.

Share the connection string with your team:
```
postgres://team-user:password@your-host:5432/pgmemory?sslmode=require
```

### Step 2: Install pgmemory (each team member)

```bash
curl -fsSL https://raw.githubusercontent.com/jeff-vincent/pgmemory/main/install.sh | bash --postgres "postgres://team-user:password@your-host:5432/pgmemory?sslmode=require"
```

Or install first and configure separately:
```bash
curl -fsSL https://raw.githubusercontent.com/jeff-vincent/pgmemory/main/install.sh | bash
pgmemory credentials set-postgres-url
# Enter the connection string when prompted
```

### Step 3: Configure (each team member)

The connection string is stored securely in the OS keychain. The config file at `~/.pgmemory/config.yaml` references it:

```yaml
postgres_url: "keychain:pgmemory/postgres_url"
```

### Step 4: Start and connect

```bash
pgmemory start
```

Then connect your AI tools the same way as solo use (proxy or MCP). All team members read from and write to the same PostgreSQL database.

---

## What happens next

From this point on, work normally. As you use your AI tools:

- **Knowledge accumulates** — debugging sessions, architecture discussions, deployment procedures all get captured
- **Quality improves** — the steward automatically removes noise, merges duplicates, and surfaces the most valuable knowledge
- **Everyone benefits** — one person's debugging insight becomes available to the entire team (in team mode)

### Seeding knowledge (optional)

Accelerate the process by ingesting existing documentation:

```bash
pgmemory ingest --name "team-wiki" https://wiki.yourcompany.com/engineering
```

This crawls the URL and adds the content to the store, where it lives alongside organically captured knowledge. See [Team Knowledge Hub](team-knowledge-hub) for more on seeding and curating team knowledge.
