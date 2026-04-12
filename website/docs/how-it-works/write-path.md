---
sidebar_position: 2
title: How Knowledge is Captured
---

# How Knowledge is Captured

Every AI interaction is automatically processed into reusable knowledge — with zero impact on response speed.

## The flow

```
AI response → Pre-filter → Length check → Noise score → LLM quality gate → Atomic fact extraction → Scrub secrets → Deduplicate → Store
```

### 1. Pre-filter cheap noise

Before any expensive processing, a fast string-matching filter checks if the exchange is purely procedural — a user saying "ok" and an assistant responding "I'll update that for you." These are rejected instantly with no LLM cost.

Rejected exchanges are logged and used to **teach the system what noise looks like** (see adaptive learning below).

### 2. Check response length

Very short responses (under 80 characters by default) almost never contain durable knowledge. These are skipped before making any LLM call.

### 3. Score against noise prototypes

The raw response text is embedded and compared against learned noise prototypes — patterns of content that previous rejections have established as low-value. If the score falls below the threshold (0.35 by default), the response is skipped.

This gate is **adaptive**: as the system sees more noise, it gets better at recognizing it. The prototypes are rebuilt every 25 rejections from a ring buffer of the 500 most recent rejected exchanges.

### 4. LLM quality gate & atomic fact extraction

Responses that pass the automated filters go to an LLM (Claude Haiku) for synthesis. The model extracts **atomic facts** — individual, self-contained pieces of knowledge — using a `FACT:` prefix format. If there's no durable value, it returns "SKIP".

Each extracted fact is stored as its own memory, making retrieval more precise. A single conversation turn that covers deployment procedures and API conventions produces separate memories for each topic.

### 5. Code block stripping

Before processing, fenced code blocks are stripped from the content. Raw code dumps are noise for the knowledge store — what matters is the explanation and reasoning around the code.

### 6. Scrub secrets

**Before anything is stored**, pgmemory scrubs sensitive content using 13 detection patterns:

| What's detected | Examples |
|---|---|
| Cloud credentials | AWS access keys, AWS secret keys |
| API tokens | GitHub tokens, Slack tokens, Stripe keys |
| Authentication secrets | JWTs, Bearer tokens, private keys, SSH keys |
| Connection strings | Database URIs with embedded passwords |
| Generic secrets | Key-value pairs containing `password`, `secret`, `token`, `api_key` |

Detected values are replaced with safe placeholders (e.g., `[REDACTED:AWS_KEY]`). **Secrets never enter the knowledge store.**

### 7. Deduplicate & supersession tagging

Each new piece of knowledge is compared against what's already in the store:

| Similarity | What happens |
|---|---|
| **Very high** (≥ 92%) | Already known — skip |
| **High** (75-92%, from a source) | Related to existing reference material — stored with a link back. This is "supersession tagging" — real-world learnings extend reference docs |
| **Low** | Novel knowledge — stored normally |

### 8. Store

Each surviving fact becomes a knowledge item in PostgreSQL, available immediately for retrieval.

## Why async matters

The entire capture pipeline runs in the background. The AI response streams back to the developer in real-time — pgmemory processes it after delivery. There's never any slowdown from knowledge capture.

## Adaptive noise learning

The pre-LLM gates aren't static — they improve over time:

1. **Rejected exchanges accumulate** — Every exchange rejected by the pre-filter or the LLM quality gate is logged to a ring buffer (500 most recent).
2. **Noise prototypes are rebuilt** — Every 25 rejections, the assistant texts are re-embedded as noise prototypes.
3. **The content scorer improves** — New prototypes are hot-swapped into the scoring system, adapting to your specific patterns.
4. **Persistence across restarts** — The rejection log is saved to disk, so the system doesn't lose its learned noise patterns.

## What builds over time

After using pgmemory for a few weeks, the store typically contains:

- **Architecture decisions** — why specific patterns were chosen, from the conversations where those decisions were made
- **Debugging playbooks** — how to diagnose common issues, from actual debugging sessions
- **Deployment knowledge** — environment config, migration procedures, rollback steps
- **Codebase conventions** — naming patterns, error handling approaches, testing strategies
- **Integration details** — how services connect, what APIs expect, edge cases discovered in practice
