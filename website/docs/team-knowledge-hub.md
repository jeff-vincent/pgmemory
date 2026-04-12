---
sidebar_position: 5
title: Team Knowledge Hub
---

# Team Knowledge Hub

pgmemory isn't just a tool for individual developers. It can be a **shared knowledge layer for your engineering organization** — one that builds itself from the work your team is already doing.

## The problem pgmemory solves

Every engineering team has hard-won institutional knowledge:

- How the deploy pipeline actually works (not what the stale wiki says)
- Why that config flag exists and when to change it
- What the payment service expects in edge cases
- How to diagnose that intermittent CI failure

This knowledge lives in people's heads, buried Slack threads, outdated Confluence pages, and tribal memory that walks out the door when someone leaves. No one writes documentation — or when they do, it's outdated by the time it's published.

**pgmemory captures this knowledge automatically, keeps it current, and makes it available to everyone's AI tools — without anyone stopping to write docs.**

## How it works

Every team member runs pgmemory locally. All instances connect to a **shared PostgreSQL database** with pgvector. When anyone uses their AI coding tools, the knowledge from those sessions flows into the shared store.

```
Engineer A (Claude Code)  ──→                              ←── Engineer B (Cursor)
                               Shared PostgreSQL + pgvector
Engineer C (read-only)    ←──                              ←── Engineer D (Claude Code)
                                        ↕
                                Quality Maintenance
                            (dedup, scoring, pruning)
```

### Setup is minimal

Each team member adds one line to their config (or stores it in the keychain):

```yaml
postgres_url: "postgres://team-user:password@your-host:5432/pgmemory?sslmode=require"
```

That's it. The knowledge store, quality signals, and cross-team deduplication all flow through the same database.

### Different tools, same knowledge

The store is tool-agnostic. Team members can use whatever they prefer:

| Team member | Their tool | Integration | What happens |
|---|---|---|---|
| Alice | Claude Code | Proxy + MCP | Every session automatically captured; MCP tools for retrieval |
| Bob | Cursor | MCP server | Agent searches and stores via tool calls |
| Carol | Windsurf | MCP (read-only) | Consumes team knowledge, doesn't contribute |
| Dave | Custom pipeline | MCP server | Integrates with internal tooling |

All four benefit from the same knowledge pool.

### Quality at scale

The [quality maintenance system](how-it-works/quality-loop) becomes *more* valuable with a shared store:

- **Cross-contributor dedup** — when three engineers independently learn the same thing, the system consolidates to a single knowledge item
- **Collective signal** — knowledge retrieved across multiple sessions earns a higher quality score faster
- **Natural pruning** — one-off debugging artifacts that are never useful decay and disappear automatically

## Seeding your knowledge base

Teams can accelerate the ramp-up by ingesting existing documentation:

```bash
pgmemory ingest --name "team-wiki" https://wiki.yourcompany.com/engineering
pgmemory ingest --name "api-docs" https://docs.internal.yourcompany.com
```

Or upload files directly. Ingested sources live alongside organically captured knowledge and go through the same quality process.

## What builds over time

After a team has been using pgmemory for a few weeks:

| Knowledge type | How it's captured |
|---|---|
| **Architecture decisions** | From the conversations where they were made — including the "why" |
| **Debugging playbooks** | From actual debugging sessions, not theoretical runbooks |
| **Deployment procedures** | From real deploy sessions — current, not last year's wiki page |
| **Codebase conventions** | From code review discussions and implementation patterns |
| **Integration details** | From sessions working with APIs — edge cases included |
| **Onboarding context** | Accumulated from everyone — new hires inherit months of knowledge on day one |

The knowledge is always current because it's built from current work.

## Team-scoped knowledge (roadmap)

Today, all team members sharing a PostgreSQL database contribute to and read from a single knowledge pool. This works well for teams and small organizations.

**Coming next: overlapping knowledge scopes aligned to teams and business units.**

## Participation is opt-in

| Level | How | Best for |
|---|---|---|
| **Full participation** | Proxy or MCP with writes | Engineers who want maximum value |
| **Read-only** | MCP search only | New hires, evaluators, PMs |
| **Isolated** | Separate database | Teams that need a private store |

There's no forced contribution. The value proposition of the shared store speaks for itself.

## Getting started with your team

1. **Start small** — pick 3-5 engineers for a pilot. Set up a shared PostgreSQL instance, install pgmemory.
2. **Work normally for a sprint** — no behavior changes needed.
3. **Show the results** — search the knowledge base. The value is visible within days.
4. **Expand gradually** — add more team members.
5. **Seed with sources** — ingest team wikis, API docs, runbooks.

→ [Getting Started](getting-started) has the full setup guide.
