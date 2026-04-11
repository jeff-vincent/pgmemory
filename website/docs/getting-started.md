---
sidebar_position: 2
title: Getting Started
---

# Getting Started

This guide walks through setting up memoryd for your team. The typical flow: one person (a team lead or platform engineer) provisions the shared database, then each team member installs memoryd and connects.

## What you'll need

- **MongoDB Atlas** — a shared cluster your team connects to ([free tier](https://www.mongodb.com/atlas) works for evaluation)
- **Each team member's machine needs:**
  - memoryd installed (macOS or Linux)
  - An AI coding tool (Claude Code, Cursor, Windsurf, etc.)

## Step 1: Set up the shared database

This is a one-time task, typically done by whoever manages your team's infrastructure.

### Create an Atlas cluster

1. Sign up or log in at [cloud.mongodb.com](https://cloud.mongodb.com)
2. Create a cluster (free tier M0 is fine for getting started)
3. Create a database called `memoryd` with a collection called `memories`
4. Set up database access (username/password) and network access for your team

### Create the search index

In the Atlas UI, go to **Atlas Search** and create a vector search index on the `memories` collection:

```json
{
  "name": "vector_index",
  "type": "vectorSearch",
  "definition": {
    "fields": [{
      "type": "vector",
      "path": "embedding",
      "numDimensions": 1024,
      "similarity": "cosine"
    }]
  }
}
```

### Share the connection string

Copy the connection string from your cluster's "Connect" dialog. It looks like:

```
mongodb+srv://team-user:password@cluster0.mongodb.net/?retryWrites=true
```

Distribute this to your team via your org's secrets management (1Password, Vault, etc.). Each team member will add it to their local config.

## Step 2: Install memoryd (each team member)

**One-line install (macOS):**
```bash
curl -fsSL https://raw.githubusercontent.com/memory-daemon/memoryd/main/install.sh | bash
```

This installs the memoryd binary, downloads the local embedding model (~70MB), and creates a default config file.

**From source:**
```bash
git clone https://github.com/memory-daemon/memoryd.git
cd memoryd
make build    # → bin/memoryd
```

## Step 3: Configure (each team member)

On first run, memoryd creates a config file at `~/.memoryd/config.yaml`. The only required change is adding the shared connection string:

```yaml
mongodb_atlas_uri: "mongodb+srv://team-user:password@cluster0.mongodb.net/?retryWrites=true"
atlas_mode: true
```

Setting `atlas_mode: true` enables the full feature set — hybrid search, quality filtering, and cross-team deduplication. See [Configuration](configuration) for all options.

## Step 4: Start memoryd and connect your tools

```bash
memoryd start
```

The embedding model downloads automatically on first launch. Then connect your AI tools:

**Claude Code (proxy mode — fully automatic):**
```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:7432
```

Now every Claude Code session automatically captures and retrieves knowledge from the shared store. No other changes needed.

**Cursor, Windsurf, or other MCP-compatible tools:**

Add memoryd as an MCP server in your tool's config:

```json
{
  "mcpServers": {
    "memoryd": {
      "command": "memoryd",
      "args": ["mcp"]
    }
  }
}
```

See [Connecting Your Tools](agents/mcp-server) for detailed setup per tool.

## Step 5: Verify it's working

```bash
memoryd status        # confirms the daemon is running
memoryd search "test" # searches the shared knowledge store
```

Visit the built-in dashboard at `http://localhost:7432` to see memories accumulating, quality stats, and knowledge sources.

## What happens next

From this point on, your team works normally. As people use their AI tools:

- **Knowledge accumulates** — debugging sessions, architecture discussions, deployment procedures all get captured
- **Quality improves** — the steward automatically removes noise, merges duplicates, and surfaces the most valuable knowledge
- **Everyone benefits** — one person's debugging insight becomes available to the entire team

### Seeding team knowledge (optional)

Teams can accelerate the process by ingesting existing documentation:

```bash
memoryd ingest "team-wiki" https://wiki.yourcompany.com/engineering
```

This crawls the URL and adds the content to the shared store, where it lives alongside organically captured knowledge. See [Team Knowledge Hub](team-knowledge-hub) for more on seeding and curating team knowledge.

## Evaluating for your team

Want to try it before rolling out? Start with a small group:

1. Set up the shared Atlas cluster
2. Have 3-5 engineers install and use memoryd for a sprint
3. Check the dashboard and `memoryd search` to see what knowledge has accumulated
4. Run `quality_stats` to see retrieval patterns

The value becomes obvious quickly — especially after the first time someone's AI tool surfaces context from a teammate's earlier session.
