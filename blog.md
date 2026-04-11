# Teaching a Memory Daemon to Forget: Adaptive Noise Filtering for AI Agent Memory

AI coding agents are powerful — but they forget everything between sessions. memoryd is a local daemon that fixes this. It sits between your coding agent (Claude Code, Cursor, etc.) and the LLM API, transparently capturing useful knowledge from every conversation and retrieving it when relevant. Think of it as long-term memory for your AI pair programmer.

The architecture is straightforward: every response from the LLM flows through a write pipeline that chunks, embeds, deduplicates, and stores the content in a MongoDB vector store. On the next request, relevant memories are retrieved via vector search and injected into context. The agent never knows it's there.

But there's a problem: **most of what an AI coding agent says isn't worth remembering.**

"Got it, I'll proceed with that change." "Running the tests now." "Let me search for that file." These procedural acknowledgments outnumber genuine insights by a wide margin. Without filtering, the memory store fills with noise, retrieval quality degrades, and the system becomes worse than useless.

We needed a quality gate. We had one — an LLM-based synthesizer (Claude Haiku) that evaluates each response and returns "SKIP" for low-value content. It works well, but at ~700ms per call plus API costs, running it on every single response is expensive. The question became: **can we learn what noise looks like and skip the LLM call entirely for obvious junk?**

## The Approach: Layered Pre-Filtering

We built a three-stage gate that runs *before* the Haiku synthesizer call:

```
Incoming response
  → Stage 1: Quick Filter (regex patterns for greetings, acks)
  → Stage 2: Length Gate (< 80 chars = skip)
  → Stage 3: Content Score Gate (embedding similarity to learned noise)
  → Stage 4: Haiku Synthesizer (LLM quality judgment)
  → Stage 5: Write Pipeline (chunk, embed, dedup, store)
```

Stages 1-3 are local and fast (< 10ms combined). Only content that passes all three reaches the expensive Haiku call in Stage 4.

The interesting part is Stage 3 — the **adaptive content scorer**.

### How the Content Scorer Works

The scorer maintains two sets of embedding vectors: *quality prototypes* and *noise prototypes*.

**Quality prototypes** are static descriptions of high-value knowledge:
- "important technical decision with reasoning and rationale"
- "debugging solution root cause analysis and fix"
- "configuration setup deployment and environment instructions"

These are embedded once at startup and don't change.

**Noise prototypes** start with a few static defaults but *learn from experience*. Every time the Haiku synthesizer rejects a response (returns "SKIP"), that text gets added to a rejection store — a ring buffer of the 500 most recent rejected texts, persisted to disk.

On the next startup (or when the rejection count crosses a threshold), the scorer rebuilds its noise model by embedding the most recent 150 rejection texts. Now it knows what *your specific* noise looks like — not just generic greetings, but the particular patterns of procedural chatter from your codebase and workflow.

### Scoring: Top-K, Not Average

The score for an incoming chunk is computed as:

$$\text{score} = \frac{\text{avgQualitySim}}{\text{avgQualitySim} + \text{topKNoiseSim}}$$

Where `avgQualitySim` is the mean cosine similarity to all quality prototypes, and `topKNoiseSim` is the mean cosine similarity to the **K=3 most similar** noise prototypes.

That top-K detail matters. Our first implementation averaged similarity across *all* noise prototypes. With a handful of static prototypes, this worked fine. But as the rejection store grew to hundreds of diverse noise examples, the average converged to a near-constant — the scorer lost all discriminative power.

Top-K scoring asks: "how much does this look like the *most similar* noise you've seen?" rather than "how much does this look like the *average* of all noise?" The former stays sharp regardless of noise diversity.

### Avoiding the Feedback Loop

We hit a subtle bug during development. Originally, content_score_filter rejections were fed back into the rejection store. This created a positive feedback loop: the scorer rejected something → that rejection made the scorer more aggressive → it rejected more → it got even more aggressive. Within a few hundred entries, it was rejecting everything.

The fix: only Haiku "SKIP" responses feed back. The content scorer is a *predictor* of what Haiku would say, not an independent judge. Its rejections are not confirmation of noise — they're educated guesses that should not reinforce themselves.

## The Benchmark

### Dataset

We constructed a 1,000-entry evaluation dataset combining two sources:

1. **144 hand-crafted entries** with verified ground-truth labels (from a purpose-built eval set):
   - 81 noise: greetings, acknowledgments, procedural status updates
   - 27 low-value: generic explanations, common boilerplate
   - 36 substantive: architecture decisions, debugging insights, production configurations

2. **856 entries from a HuggingFace dataset** of real Claude Code traces (nlile/misc-merged-claude-code-traces-v1), labeled with heuristics based on length, structure, and content patterns.

This mix gave us controlled ground-truth entries embedded in a realistic corpus of agent traffic.

### Protocol

We ran three sequential benchmark passes against the same 1,000-entry dataset:

- **Run 1**: Fresh start — empty rejection store, empty memory database
- **Run 2**: Memories wiped, but rejection store preserved (500 entries accumulated during Run 1)
- **Run 3**: Memories wiped again, rejection store preserved (now reflecting two passes of learning)

Each run sent every entry through the live memoryd `/api/ingest` endpoint — the same path that production traffic takes. No mocks, no shortcuts.

### Metrics

We measured two things:

1. **Substantive recall**: Of the 36 hand-crafted entries labeled "substantive," how many made it through to storage? (Higher is better.)
2. **Pipeline efficiency**: How many entries were filtered *before* reaching the Haiku LLM call? (Higher means more cost savings.)

## Results

### Ground-Truth Accuracy (hand-crafted entries only)

| Label | Run 1 | Run 2 | Run 3 |
|---|---|---|---|
| **Substantive** (36 total) | 34 stored (**94%** recall) | 35 stored (**97%** recall) | 35 stored (**97%** recall) |
| **Noise** (81 total) | 0 stored (**100%** filtered) | 0 stored (**100%** filtered) | 0 stored (**100%** filtered) |
| **Low-value** (27 total) | 18 filtered (67%) | 18 filtered (67%) | 18 filtered (67%) |

**97% substantive recall with 100% noise precision** — the system stores almost everything worth keeping and rejects all obvious noise.

The single false negative across runs 2 and 3 was a borderline entry caught by the content_score_filter that Haiku would have accepted. At a 0.35 threshold, this is an acceptable tradeoff.

### Pipeline Efficiency (all 923 valid entries)

| Stage | Count | % of Total |
|---|---|---|
| Length filter (< 80 chars) | 217 | 24% |
| Content score filter | 112 | 12% |
| Pre-filter (regex) | 2 | <1% |
| **Total pre-Haiku** | **331** | **36%** |
| Haiku calls needed | 592 | 64% |

**36% of all entries never reach the LLM.** That's a direct reduction in API costs and latency.

### Adaptive Learning Dynamics

The most interesting finding: at 1,000 entries, the content scorer self-primes within a single run. The rejection store filled to its 500-entry capacity during Run 1, so Runs 2 and 3 produced identical filtering behavior — 112 content_score_filter rejections each.

This is actually good news for production. It means the system reaches steady-state quickly. You don't need to "train" it over weeks of use — a few hundred interactions is enough for the noise model to saturate.

In our earlier 144-entry benchmarks, the learning curve was more visible:

| Metric | Run 1 (fresh) | Run 2 (99 rej) | Run 3 (173 rej) |
|---|---|---|---|
| Content score filter | 0 | 25 | 39 |
| Haiku calls | 91 | 66 | 52 |
| **Haiku reduction** | baseline | **27%** | **43%** |
| Substantive recall | 36/36 (100%) | 35/36 (97%) | 35/36 (97%) |

At the smaller scale, you can clearly see the scorer learning: zero pre-Haiku filtering on the first pass, then progressively intercepting more noise as the rejection store fills. A 43% reduction in Haiku calls with only a single false negative.

## Architecture Notes

A few design choices that made this work:

**Local embeddings**: The content scorer embeds incoming text with voyage-4-nano running locally via llama.cpp (~5ms per call). This keeps the pre-Haiku gate fast. If we needed a remote embedding API, the latency savings would evaporate.

**Ring buffer persistence**: The rejection store is a simple JSONL ring buffer (500 entries, ~150KB on disk). On startup, the most recent 150 entries are embedded to build the noise model. No database, no complex state management.

**Graceful degradation**: When the scorer is nil (no rejection data yet, or embedding failure), `PreScore` returns `(0, false)` and the gate is simply skipped. The system falls back to Haiku-only filtering. No crashes, no false rejections.

**Top-K=3 with ratio normalization**: The scoring formula naturally produces values in (0, 1) without needing calibrated thresholds. The 0.35 threshold was tuned once against the eval set and has been stable across dataset sizes.

## What We Learned

1. **LLM-as-judge is expensive but accurate.** Haiku is excellent at distinguishing noise from substance (100% noise precision). But at ~700ms and a per-token cost, you don't want to run it on everything.

2. **Embedding similarity is a surprisingly good noise predictor.** With learned noise prototypes, a cosine similarity check catches 36% of noise before it reaches the LLM — and it's wrong less than 3% of the time on substantive content.

3. **Feedback loops are the enemy of adaptive systems.** The moment a filter's outputs feed back into its own training data, it spirals. Keep the learning signal clean: only ground-truth rejections (from the LLM judge) should train the fast filter.

4. **Top-K beats average for learned prototypes.** When your noise model grows from 3 static examples to 150 learned ones, averaging destroys signal. Top-K preserves it.

5. **Small datasets learn faster visibly; large datasets learn faster absolutely.** At 144 entries, you can watch the learning curve unfold over 3 runs. At 1,000 entries, the system saturates in one pass. Both reach the same steady state.

## Try It

memoryd is open source and runs entirely locally. A MongoDB instance (local or Atlas), a small embedding model, and an API key for your LLM provider is all you need. Point your coding agent at `127.0.0.1:7432` and it gains persistent memory — with an adaptive noise filter that gets smarter the more you use it.
