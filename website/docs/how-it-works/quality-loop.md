---
sidebar_position: 3
title: Quality Maintenance
---

# Quality Maintenance

pgmemory doesn't just accumulate knowledge — it curates it. A background process called the **steward** continuously scores, cleans, and deduplicates the knowledge base.

## Why this matters

Without curation, a knowledge store degrades quickly. Outdated patterns, one-off debugging conversations, and redundant explanations crowd out the signal. The steward solves this by treating quality as a feedback loop: knowledge that helps answer questions survives and rises to the top. Knowledge that doesn't, fades away.

## Learning period

For the first 50 retrieval events, the steward stays hands-off. Everything is kept while the system learns what's actually useful. Once enough usage data accumulates, quality-aware filtering activates automatically.

## The maintenance cycle

Every 60 minutes (configurable), the steward runs a three-phase sweep:

### Phase 1: Score

Every knowledge item gets a quality score based on two factors:

- **Usage** — How often has this been retrieved? Items that are frequently surfaced score higher.
- **Recency** — How recently was this last useful? Unused knowledge decays over time (90-day half-life by default).

Items with higher content quality scores (as judged at write time by the LLM synthesis gate) decay more slowly — the system trusts that well-scored content remains relevant longer.

New items start with a neutral score and aren't penalized until they've had a chance to prove their value.

### Phase 2: Clean up

A knowledge item is removed only when **all three** conditions are met:

1. **Old enough** — exists for more than 24 hours (grace period)
2. **Low quality** — score below the pruning threshold
3. **Never retrieved** — zero evidence anyone found it useful

This is deliberately conservative. Even a single retrieval saves an item from cleanup.

### Phase 3: Deduplicate

Near-duplicate items (≥ 88% cosine similarity) are identified and the one with more usage signal is kept. This is especially valuable for teams: three engineers debug the same issue in the same week — the steward merges the redundant entries into one high-quality item.

When merging, the steward also considers **average retrieval similarity scores** — items that consistently appear as highly relevant matches are preferred over items with lower retrieval quality.

## Adaptive noise filtering

Beyond the steward's post-storage maintenance, pgmemory also filters noise **before** knowledge enters the store:

1. **Pre-filter** — Fast string matching rejects obviously procedural exchanges (no LLM cost)
2. **Length gate** — Very short responses (< 80 chars) are skipped
3. **Content score gate** — Responses are scored against learned noise prototypes. Below the threshold, they're skipped before any LLM call
4. **LLM quality gate** — A lightweight model (Claude Haiku) judges whether the exchange has durable value and extracts atomic facts

The content scorer **adapts over time**: rejected exchanges are accumulated in a ring buffer, and every 25 rejections the noise prototypes are rebuilt. The system learns your specific noise patterns.

## Configuration

All steward settings are tunable in `config.yaml`. See [Configuration](../configuration) for the full reference and tuning recommendations.
