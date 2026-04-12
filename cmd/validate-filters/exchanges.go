package main

// Exchange represents a single user/assistant interaction with an expected
// filter outcome. The harness feeds each exchange through the gate chain
// and compares the actual outcome against Expected.
type Exchange struct {
	ID          int
	Category    string // debugging, architecture, config, error-fix, dependency, performance, api, prefilter, length, noise, dedup, discovery
	User        string // user prompt
	Assistant   string // assistant response
	Expected    string // store, filter_prefilter, filter_length, filter_noise, dedup
	DuplicateOf int    // if Expected == "dedup", which exchange ID this duplicates
	Notes       string // why this outcome is expected
}

func allExchanges() []Exchange {
	var all []Exchange
	all = append(all, debuggingExchanges()...)
	all = append(all, architectureExchanges()...)
	all = append(all, configExchanges()...)
	all = append(all, errorFixExchanges()...)
	all = append(all, dependencyExchanges()...)
	all = append(all, performanceExchanges()...)
	all = append(all, apiExchanges()...)
	all = append(all, discoveryExchanges()...)
	all = append(all, prefilterExchanges()...)
	all = append(all, lengthExchanges()...)
	all = append(all, noiseExchanges()...)
	all = append(all, dedupExchanges()...)
	return all
}

// --- Should store: debugging root causes (10) ---

func debuggingExchanges() []Exchange {
	return []Exchange{
		{
			ID: 1, Category: "debugging",
			User:     "The API is returning 504 timeouts on /api/search but only in production.",
			Assistant: "Found the root cause. The production Postgres instance has work_mem set to 4MB, but the vector search query with ORDER BY embedding <=> $1 needs to sort the full result set in memory before LIMIT applies. With 500K rows at 1024 dimensions, that's ~2GB of sort space. The dev instance only has 10K rows so it never spills to disk. Fix: add an HNSW index with ef_search=40 so the planner uses index scan instead of sequential scan plus sort.",
			Expected: "store",
			Notes:    "Root cause with specific config values and fix",
		},
		{
			ID: 2, Category: "debugging",
			User:     "Why is the embedding server returning different vectors for the same text?",
			Assistant: "The llama-server instance was started with -np 4 for parallel processing, but voyage-4-nano's GGUF quantization (Q8_0) produces slightly different results under concurrent requests due to floating-point accumulation order in batched matrix multiplications. The difference is small (cosine similarity > 0.999) but enough to cause dedup failures at a 0.92 threshold. Fix: use -np 1 or accept the variance by lowering dedup to 0.90.",
			Expected: "store",
			Notes:    "Non-obvious concurrency bug in embedding model",
		},
		{
			ID: 3, Category: "debugging",
			User:     "The steward is pruning memories that were just created.",
			Assistant: "The grace period check uses CreatedAt from the database, but the steward's time.Now() is called at the start of the batch, not per-memory. When a steward sweep takes longer than grace_period_hours (24h by default), memories created during the sweep can appear to be past the grace period relative to the batch start time. The fix is to snapshot time.Now() once and pass it through, which is already done — the bug is that the clock was being compared against m.CreatedAt.Add(gracePeriod) instead of batchStart.Sub(m.CreatedAt) > gracePeriod.",
			Expected: "store",
			Notes:    "Subtle time comparison bug",
		},
		{
			ID: 4, Category: "debugging",
			User:     "Memory search returns zero results even though there are 1000 memories in the store.",
			Assistant: "The HNSW index on the memories table was created with vector_l2_ops but the search query uses the cosine distance operator <=>. pgvector requires the index operator class to match the query operator. Rebuild with: CREATE INDEX memories_embedding_idx ON memories USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64). The L2 index was silently ignored and Postgres fell back to a sequential scan, which timed out at 500K rows.",
			Expected: "store",
			Notes:    "pgvector index operator class mismatch",
		},
		{
			ID: 5, Category: "debugging",
			User:     "The proxy is dropping SSE events mid-stream.",
			Assistant: "bufio.Scanner has a default max token size of 64KB. When Anthropic sends a content_block_delta with a large code block, the SSE data: line exceeds this limit and Scanner.Scan() silently returns false with scanner.Err() == nil. The fix is scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) to raise the limit to 1MB. This was already applied in handleStream but not in the test helper that was failing.",
			Expected: "store",
			Notes:    "bufio.Scanner silent truncation",
		},
		{
			ID: 6, Category: "debugging",
			User:     "The content score pre-gate is rejecting everything after about 100 exchanges.",
			Assistant: "The adaptive noise scorer rebuilds from the rejection store every 25 rejections. After ~100 exchanges, the rejection store has enough samples that the learned noise prototypes start to overlap with the quality prototypes — the scorer's avgQuality / (avgQuality + topNoise) ratio converges toward 0.5 for everything. The fix is to cap the noise prototype count (already done: 150 max) but also to exclude rejections from the pre-filter stage, since those are ack/procedural patterns, not content that scored borderline.",
			Expected: "store",
			Notes:    "Feedback loop in adaptive scoring",
		},
		{
			ID: 7, Category: "debugging",
			User:     "Getting 'pq: relation memories does not exist' on startup despite running migrations.",
			Assistant: "The migration runs against the database named in the connection string, but ensureSchema() in postgres.go connects using the pool's default database. When DATABASE_URL has dbname=pgmemory but the pool was initialized with the default postgres database, the schema gets created in postgres and queries hit pgmemory. The config parser now extracts dbname from the connection string and passes it to ensureSchema explicitly.",
			Expected: "store",
			Notes:    "Database name mismatch between migration and query paths",
		},
		{
			ID: 8, Category: "debugging",
			User:     "Why do memories show created_at in the future in the dashboard?",
			Assistant: "The dashboard renders created_at using the browser's local timezone, but the API returns RFC3339 timestamps with the server's timezone offset. When the server runs in UTC (Docker default) and the browser is in UTC-7, a memory created at 17:00 UTC displays as 17:00 local time — which is 7 hours in the future from the user's perspective. The fix: the dashboard JavaScript already calls new Date() but wasn't calling toLocaleString(). Added that to the template rendering.",
			Expected: "store",
			Notes:    "Timezone rendering bug",
		},
		{
			ID: 9, Category: "debugging",
			User:     "The MCP server hangs after exactly 10 requests.",
			Assistant: "The MCP server creates an http.Client per request but never closes the response body on error paths. Go's http.Transport has MaxIdleConnsPerHost defaulting to 2, and the connection pool fills up after 10 in-flight requests because the idle connections from error paths are never returned. Adding defer resp.Body.Close() before the status code check in the MCP server's apiGet/apiPost methods fixes the leak.",
			Expected: "store",
			Notes:    "HTTP connection pool exhaustion from unclosed response bodies",
		},
		{
			ID: 10, Category: "debugging",
			User:     "The redaction filter is stripping legitimate code that happens to look like an API key.",
			Assistant: "The regex pattern for API keys matches any 32+ character hex string, which catches SHA-256 hashes in test fixtures and content-addressable storage paths. The fix is to require a key-like prefix: the pattern now checks for (key|token|secret|password|apikey|api_key)\\s*[:=]\\s* before the hex sequence. The original regex was too broad — it was designed for Claude output where bare hex strings are usually credentials, but in stored memories about code, hex hashes are common.",
			Expected: "store",
			Notes:    "False positive in redaction regex",
		},
	}
}

// --- Should store: architecture decisions (8) ---

func architectureExchanges() []Exchange {
	return []Exchange{
		{
			ID: 11, Category: "architecture",
			User:     "Why use pgvector instead of a dedicated vector database?",
			Assistant: "pgvector was chosen over Pinecone/Qdrant/Weaviate for three reasons: (1) single-process deployment — pgmemory runs as one binary with embedded Postgres, no external service to manage; (2) hybrid search — Postgres GIN indexes for full-text search combined with HNSW for vector search, merged via Reciprocal Rank Fusion, is more effective than vector-only retrieval for code facts; (3) ACID transactions — memory updates (supersession tagging, quality score decay) need atomic read-modify-write, which dedicated vector DBs handle poorly.",
			Expected: "store",
			Notes:    "Key architectural decision with rationale",
		},
		{
			ID: 12, Category: "architecture",
			User:     "Should the synthesizer use streaming or batch mode?",
			Assistant: "Batch (non-streaming) mode for the synthesizer. The synthesizer calls are fire-and-forget background goroutines — there's no user waiting for the response. Streaming adds complexity (SSE parsing, partial content accumulation) for zero UX benefit. The total response from Haiku is typically under 200 tokens (1-3 FACT: lines), which completes in under 1 second. Streaming only helps when you need to show progress on long generations.",
			Expected: "store",
			Notes:    "Design decision with clear reasoning",
		},
		{
			ID: 13, Category: "architecture",
			User:     "Why does the read pipeline prepend context to the system prompt instead of adding a separate message?",
			Assistant: "Prepending to system prompt was chosen because: (1) the Anthropic API treats system as a single string, not a message array — injecting as a separate user message would break the conversation flow and confuse the model about who said what; (2) system prompt content is cached by the API's prompt caching, so retrieved memories piggyback on the cache for free; (3) the MCP server already injects tool results as content blocks, and adding memories as another content block would compete for the model's attention with tool output.",
			Expected: "store",
			Notes:    "System prompt injection rationale",
		},
		{
			ID: 14, Category: "architecture",
			User:     "Why is the steward a background goroutine instead of a cron job?",
			Assistant: "In-process goroutine avoids the deployment complexity of a separate cron service. The steward needs direct access to the store and quality tracker — if it were external, it would need its own database connection, auth, and API surface. A goroutine shares the process memory, can read the quality tracker's in-memory state without serialization, and gracefully shuts down with the main process. The tradeoff is that steward work blocks on the same CPU, but the sweep is I/O-bound (database queries) and runs at most every 60 minutes.",
			Expected: "store",
			Notes:    "Deployment simplicity tradeoff",
		},
		{
			ID: 15, Category: "architecture",
			User:     "What's the reasoning behind the two-phase quality scoring?",
			Assistant: "Phase 1 (content score) runs at ingest time using only the embedding — it compares against quality and noise prototype vectors to produce a score in [0, 1]. This is cheap (one vector comparison) and catches obvious noise before the Haiku call. Phase 2 (quality score) runs in the steward sweep using retrieval history — avg cosine similarity across actual retrievals, decayed over time. Phase 1 gates storage, phase 2 gates retention. Splitting them means the system can store aggressively (low content score threshold) and prune conservatively (based on actual utility).",
			Expected: "store",
			Notes:    "Two-phase scoring design",
		},
		{
			ID: 16, Category: "architecture",
			User:     "Why Reciprocal Rank Fusion instead of just reranking by vector score?",
			Assistant: "Vector-only retrieval misses lexical matches — if a user asks about 'the ANTHROPIC_API_KEY environment variable', vector search finds semantically similar content about API configuration, but FTS finds the exact variable name. RRF merges both ranked lists without needing to normalize their scores (vector similarity is [0,1], FTS rank is unbounded). The k=60 constant in the RRF formula 1/(k+rank) was chosen empirically — it balances the two lists so neither dominates. MMR reranking on top prevents the merged list from clustering on one subtopic.",
			Expected: "store",
			Notes:    "Search algorithm choice with empirical constant",
		},
		{
			ID: 17, Category: "architecture",
			User:     "Why does the proxy strip code blocks before synthesis?",
			Assistant: "Assistant responses in coding sessions are typically 60-70% fenced code blocks. Code is implementation — it belongs in git, not in memory. The prose around the code carries the extractable facts: root causes, gotchas, rationale, constraints. Stripping ``` blocks before the Haiku call means: (1) the content score gate evaluates prose quality, not code volume; (2) the Haiku context window is spent on signal, not syntax; (3) embeddings capture the semantic content of the explanation, not the implementation.",
			Expected: "store",
			Notes:    "Code stripping rationale",
		},
		{
			ID: 18, Category: "architecture",
			User:     "Why use SHA-256 shingling for the test embedder instead of random vectors?",
			Assistant: "Random vectors would make dedup tests non-deterministic — two identical texts would get different embeddings, so cosine similarity would be random. SHA-256 shingling gives deterministic embeddings where similar texts produce similar vectors (shared n-grams map to the same hash buckets). It's not semantically meaningful like a real model, but it correctly tests the pipeline's dedup, topic grouping, and merge logic. The 5-character n-gram window was chosen to balance sensitivity (short window = high similarity for minor edits) against discrimination (long window = low similarity for unrelated text).",
			Expected: "store",
			Notes:    "Test infrastructure design decision",
		},
	}
}

// --- Should store: config gotchas (8) ---

func configExchanges() []Exchange {
	return []Exchange{
		{
			ID: 19, Category: "config",
			User:     "What's the default port and why?",
			Assistant: "pgmemory defaults to port 7432 because 5432 is standard Postgres and 7433 is used by the local llama-server for embeddings. The proxy needs a port that won't conflict with either. 7432 was chosen as a mnemonic: 7000 + 432 (Postgres default suffix). The MCP server runs on stdin/stdout so it doesn't use a port at all — it's invoked as a subprocess by Claude Code.",
			Expected: "store",
			Notes:    "Port selection rationale with conflict avoidance",
		},
		{
			ID: 20, Category: "config",
			User:     "What does dedup_threshold actually control?",
			Assistant: "dedup_threshold (default 0.92) is the cosine similarity above which a new memory is considered a duplicate of an existing one and silently dropped. At 0.92 with voyage-4-nano embeddings, paraphrases of the same fact typically score 0.93-0.97, while related-but-distinct facts score 0.80-0.88. Setting it too low (e.g., 0.85) would merge distinct facts about the same topic. Setting it too high (e.g., 0.98) would let near-identical paraphrases accumulate. The supersession band (0.75-0.92) tags related memories without dropping them.",
			Expected: "store",
			Notes:    "Threshold semantics with practical ranges",
		},
		{
			ID: 21, Category: "config",
			User:     "How does the content score pre-gate interact with the LLM gate?",
			Assistant: "The content score pre-gate (default 0.35) runs before the Haiku synthesis call. It scores the embedding against quality/noise prototypes and skips synthesis for obvious noise. This saves ~$0.001 per skipped call (Haiku input tokens). The LLM gate (SynthesizeQA returning SKIP) is the authoritative quality gate — it catches semantic noise that embeddings can't detect, like verbose procedural narration that uses technical vocabulary. The pre-gate is a cost optimization, not a quality filter. If you're not paying for Haiku calls, set pre-gate to 0 and let the LLM decide everything.",
			Expected: "store",
			Notes:    "Gate interaction and cost optimization",
		},
		{
			ID: 22, Category: "config",
			User:     "What's the max_group_chars setting for?",
			Assistant: "max_group_chars (default 2048) caps the total character count of chunks in a single topic group before synthesis. When ingesting a long document, consecutive chunks about the same topic get grouped together for merge synthesis. If the group is too large, the Haiku call exceeds its useful context — the model loses detail in long inputs. 2048 chars is roughly 500 tokens, which fits comfortably in Haiku's context with room for the system prompt and output. Larger documents get split into multiple groups, each synthesized independently.",
			Expected: "store",
			Notes:    "Chunking parameter with token budget reasoning",
		},
		{
			ID: 23, Category: "config",
			User:     "Why is the noise_min_alnum_ratio set to 0.4?",
			Assistant: "noise_min_alnum_ratio (default 0.40) filters chunks where less than 40% of characters are letters or digits. This catches: ASCII art, separator lines (---), bare URLs with many special characters, and formatting-heavy content with more markdown syntax than text. The threshold was derived from corpus analysis — technical prose about code averages 65-75% alphanumeric, code itself is 50-60%, and pure formatting noise is typically under 30%. The 0.40 threshold provides headroom for code-heavy chunks while still catching formatting-only content.",
			Expected: "store",
			Notes:    "Threshold derived from corpus analysis",
		},
		{
			ID: 24, Category: "config",
			User:     "What happens if I set steward interval to 0?",
			Assistant: "Setting steward interval_minutes to 0 disables the steward entirely — no scoring sweeps, no pruning, no near-duplicate merging. Memories accumulate indefinitely with quality_score stuck at 0.0 (the default). This is useful during development or evaluation when you want to inspect raw pipeline output without the steward modifying it. To re-enable, set interval_minutes > 0 and restart the daemon. The first sweep after re-enabling will process all memories in batch_size (500) chunks.",
			Expected: "store",
			Notes:    "Zero-value behavior for config parameter",
		},
		{
			ID: 25, Category: "config",
			User:     "How do I configure pgmemory for a team sharing one database?",
			Assistant: "Set the same DATABASE_URL (or Postgres connection string) in each developer's ~/.pgmemory/config.yaml. Each instance writes with source='claude-code' and reads from the shared store. The dedup gate prevents the same fact from being stored by multiple developers. For source-separated retrieval, each developer can set a unique source prefix (e.g., source='claude-code:alice') and the read pipeline's HybridSearch accepts a Source filter. The steward runs on each instance independently but operates on the same data — concurrent sweeps are safe because scoring and pruning use atomic SQL operations.",
			Expected: "store",
			Notes:    "Team configuration with concurrency safety",
		},
		{
			ID: 26, Category: "config",
			User:     "What's the embedding_dim setting and can I change it?",
			Assistant: "embedding_dim (default 1024) must match the output dimension of the embedding model. voyage-4-nano produces 1024-dimensional vectors. Changing this after data is stored requires dropping and recreating the memories table because pgvector's vector(N) column type is fixed at creation. If you switch to a different model (e.g., one producing 768-dim vectors), set embedding_dim to 768 and wipe the store. The HNSW index parameters (m=16, ef_construction=64) are tuned for 1024-dim vectors — lower dimensions may benefit from lower m values.",
			Expected: "store",
			Notes:    "Dimension coupling between model and schema",
		},
	}
}

// --- Should store: error-fix pairs (8) ---

func errorFixExchanges() []Exchange {
	return []Exchange{
		{
			ID: 27, Category: "error-fix",
			User:     "Getting 'could not resize shared memory segment: No space left on device' in Docker.",
			Assistant: "This is a Docker shared memory (shm) limit issue, not disk space. Postgres uses shared memory for its buffer pool, and Docker defaults /dev/shm to 64MB. With shared_buffers set to 128MB in the embedded Postgres, it exceeds the Docker limit. Fix: add --shm-size=256m to docker run, or reduce shared_buffers to 32MB in the embedded Postgres config. The pgmemory installer already sets shared_buffers=64MB to avoid this, but manual Docker runs don't inherit that setting.",
			Expected: "store",
			Notes:    "Classic Docker shm error with Postgres",
		},
		{
			ID: 28, Category: "error-fix",
			User:     "Error: 'pq: unsupported vector dimension 1024, expected 768'",
			Assistant: "The memories table was created with vector(768) from a previous installation that used a different embedding model. The current model (voyage-4-nano) produces 1024-dim vectors. You need to either: (1) drop and recreate the table: DROP TABLE memories; then restart pgmemory to auto-create with vector(1024); or (2) alter the column: ALTER TABLE memories ALTER COLUMN embedding TYPE vector(1024). Option 1 loses all data, option 2 preserves rows but their embeddings are invalid (768-dim in a 1024-dim column) so you'd need to re-embed everything.",
			Expected: "store",
			Notes:    "Dimension mismatch with migration options",
		},
		{
			ID: 29, Category: "error-fix",
			User:     "The proxy returns 'upstream error: x509: certificate signed by unknown authority'.",
			Assistant: "The proxy's http.Client uses the system certificate pool by default. When running inside Docker or a minimal container, the CA certificates bundle may be missing or outdated. Fix: install ca-certificates in the container (apt-get install -y ca-certificates), or set the ANTHROPIC_BASE_URL to use http:// if the upstream is a local mock. For corporate proxies with custom CAs, set SSL_CERT_FILE to the path of your CA bundle. The proxy does not support --insecure mode by design.",
			Expected: "store",
			Notes:    "TLS certificate chain issue with multiple fix paths",
		},
		{
			ID: 30, Category: "error-fix",
			User:     "MCP tool calls fail with 'connection refused' even though pgmemory is running.",
			Assistant: "The MCP server subprocess connects to localhost:7432 to talk to the daemon. If pgmemory was started with a custom port (e.g., port: 8080 in config.yaml), the MCP server still tries the default port. Fix: the MCP server reads the port from the same config file, so ensure ~/.pgmemory/config.yaml has the correct port. Alternatively, the daemon may be listening on 127.0.0.1 but the MCP server resolves localhost to ::1 (IPv6). Fix: set host: 127.0.0.1 explicitly in config.",
			Expected: "store",
			Notes:    "Port mismatch and IPv4/IPv6 ambiguity",
		},
		{
			ID: 31, Category: "error-fix",
			User:     "Error: 'embedding server returned empty vector'",
			Assistant: "The llama-server process crashed or was killed (OOM) but the port is still held by a zombie process. pgmemory's health check only verifies that the HTTP endpoint responds, not that it returns valid embeddings. Fix: kill -9 the llama-server process, then restart pgmemory (which auto-starts llama-server). To prevent OOM: the voyage-4-nano Q8_0 model needs ~150MB resident memory with -c 2048. Reduce -c to 1024 if memory is tight. The embedder client now validates vector length and retries once before returning the error.",
			Expected: "store",
			Notes:    "Zombie process and OOM with specific memory requirements",
		},
		{
			ID: 32, Category: "error-fix",
			User:     "The dashboard shows 'Failed to fetch' when loading memories.",
			Assistant: "The dashboard JavaScript makes a fetch() call to /api/memories with the token from the URL query parameter. If the token has been rotated (pgmemory regenerates it on first start after install), the cached URL in the browser has the old token. Fix: copy the new token from ~/.pgmemory/token or run pgmemory token. The dashboard also fails if the browser blocks mixed content — if you access the dashboard via HTTPS (e.g., through a tunnel), the API calls must also use HTTPS.",
			Expected: "store",
			Notes:    "Token rotation and mixed content",
		},
		{
			ID: 33, Category: "error-fix",
			User:     "Proxy mode works but MCP mode returns empty results for memory_search.",
			Assistant: "MCP mode uses the same ReadPipeline but connects to the daemon's API endpoint instead of calling the store directly. The /api/search endpoint requires the query parameter 'q', but the MCP server was sending it as 'query'. Fix was in the MCP server's callSearch method: changed the query parameter name from 'query' to 'q' to match the API handler's expectation. This only affected MCP mode because proxy mode calls ReadPipeline.Retrieve directly without going through the HTTP API.",
			Expected: "store",
			Notes:    "Parameter name mismatch between MCP and API",
		},
		{
			ID: 34, Category: "error-fix",
			User:     "Error: 'context deadline exceeded' on every write after running for a few hours.",
			Assistant: "The embedded Postgres connection pool (pgxpool) defaults to MaxConns=4. The steward's scoring sweep holds a connection for the entire batch (up to 500 memories), and the write pipeline holds another for each store operation. After a few hours of concurrent steward sweeps and proxy writes, all 4 connections are occupied and new requests wait until the 10-second context deadline. Fix: increase MaxConns to 10 in the connection string (?pool_max_conns=10), or reduce steward batch_size to 100 so sweeps release connections faster.",
			Expected: "store",
			Notes:    "Connection pool exhaustion with specific sizing",
		},
	}
}

// --- Should store: dependency quirks (6) ---

func dependencyExchanges() []Exchange {
	return []Exchange{
		{
			ID: 35, Category: "dependency",
			User:     "Why does the go.sum file have entries for MongoDB drivers if we use Postgres?",
			Assistant: "The codebase still imports go.mongodb.org/mongo-driver/bson/primitive for the ObjectID type used as memory IDs. This was carried over from the original MongoDB-backed implementation. The actual MongoDB driver isn't used — only the primitive.ObjectID type and its hex encoding. The dependency is ~2MB in the binary. To remove it, replace primitive.ObjectID with a custom ID type that generates and parses the same 24-character hex format. Not urgent since it doesn't affect runtime behavior.",
			Expected: "store",
			Notes:    "Legacy dependency with specific binary size impact",
		},
		{
			ID: 36, Category: "dependency",
			User:     "Does pgvector support HNSW on Apple Silicon natively?",
			Assistant: "Yes, pgvector 0.7+ compiles with NEON SIMD on arm64, which gives ~4x speedup on vector operations compared to scalar. The embedded Postgres in pgmemory bundles pgvector 0.7.4 compiled with -march=armv8-a+simd. On x86_64, it uses AVX2 when available. The HNSW index build is CPU-bound and takes ~30 seconds for 100K memories at 1024 dimensions on an M2 MacBook. Search is <10ms for top-5 with ef_search=40.",
			Expected: "store",
			Notes:    "Platform-specific performance characteristics",
		},
		{
			ID: 37, Category: "dependency",
			User:     "What version of the Anthropic API does the proxy target?",
			Assistant: "The proxy sets anthropic-version: 2023-06-01 in forwarded requests. It handles both streaming (SSE) and non-streaming response formats. The proxy does not use the beta features header (anthropic-beta), so tool use, computer use, and other beta features are passed through unchanged. The proxy only reads the response content — it does not interpret tool_use blocks or modify the conversation structure. Upgrading the API version requires testing that extractResponseText and extractTextDelta still parse the response format correctly.",
			Expected: "store",
			Notes:    "API version pinning with upgrade implications",
		},
		{
			ID: 38, Category: "dependency",
			User:     "Why voyage-4-nano instead of a larger model?",
			Assistant: "voyage-4-nano (Q8_0, ~70MB) was chosen for local-first deployment: it runs on CPU without a GPU, starts in <2 seconds, and produces 1024-dim vectors that are competitive with larger models for code-related text. The alternative was voyage-3 via API, but that adds network latency to every embed call (~100ms) and costs money. For a memory system that embeds on every write and every read, local embeddings at 0 marginal cost were the priority. The quality tradeoff: voyage-4-nano scores ~3% lower on MTEB code retrieval benchmarks than voyage-3.",
			Expected: "store",
			Notes:    "Model selection with quantified tradeoffs",
		},
		{
			ID: 39, Category: "dependency",
			User:     "Can I use OpenAI embeddings instead?",
			Assistant: "Not currently. The embedder interface expects a local llama-server compatible endpoint (POST /embedding with {content: text}). OpenAI's embedding API uses a different request format ({input: text, model: name}) and returns {data: [{embedding: [...]}]}. To support OpenAI, you'd add an openaiBackend implementing the embedder interface, similar to how the synthesizer has anthropicBackend. The dimension would change (text-embedding-3-small = 1536 vs voyage-4-nano = 1024), requiring a schema migration.",
			Expected: "store",
			Notes:    "Interface boundary and migration requirement",
		},
		{
			ID: 40, Category: "dependency",
			User:     "The crawler is following links outside the target domain.",
			Assistant: "The crawler uses a same-origin check based on the URL hostname, but it doesn't account for subdomains. Crawling docs.example.com will follow links to blog.example.com because both share the example.com suffix. The fix is to compare the full hostname, not just the registrable domain. The crawler also doesn't respect robots.txt — it's designed for internal documentation sites where robots.txt blocks search engines but allows humans. For public sites, add a robots.txt check before fetching.",
			Expected: "store",
			Notes:    "Crawler scope bug with subdomain handling",
		},
	}
}

// --- Should store: performance findings (5) ---

func performanceExchanges() []Exchange {
	return []Exchange{
		{
			ID: 41, Category: "performance",
			User:     "How fast is the read pipeline?",
			Assistant: "With 50K memories and the HNSW index (m=16, ef_search=40), the read pipeline completes in 15-25ms end-to-end on an M2 MacBook: ~5ms for embedding the query via llama-server, ~3ms for HNSW search, ~2ms for FTS, ~3ms for RRF merge and MMR reranking, ~2ms for FormatContext. The bottleneck is the embedding call — if llama-server is under load from concurrent write embeds, query latency can spike to 100ms+. The proxy adds ~2ms of overhead on top of read pipeline latency.",
			Expected: "store",
			Notes:    "Latency breakdown with bottleneck identification",
		},
		{
			ID: 42, Category: "performance",
			User:     "Is batch embedding faster than individual calls?",
			Assistant: "Yes, significantly. llama-server with -np 1 processes individual embed calls at ~5ms each. A batch of 10 texts takes ~15ms total (not 50ms) because the GGUF model amortizes the KV cache setup. The write pipeline uses EmbedBatch for topic-grouped chunks, which is why ingesting a 10-page document (producing ~20 chunks) takes ~50ms for embedding instead of the ~100ms you'd expect from individual calls. The batch endpoint is POST /embedding with a JSON array of {content: text} objects.",
			Expected: "store",
			Notes:    "Batch amortization with specific timing",
		},
		{
			ID: 43, Category: "performance",
			User:     "Memory usage grows over time — is there a leak?",
			Assistant: "Not a leak — the quality tracker accumulates retrieval events in memory for fast access. Each event is ~100 bytes (memory ID + score + timestamp). At 1000 retrievals/day, that's ~100KB/day. The tracker prunes events older than decay_half_days (90 days default) during steward sweeps, so steady-state is ~9MB. The actual memory hog is the llama-server process: voyage-4-nano Q8_0 with -c 2048 holds ~350MB resident. Total pgmemory footprint: ~50MB for the Go process + ~350MB for llama-server + Postgres shared buffers.",
			Expected: "store",
			Notes:    "Memory budget breakdown with steady-state analysis",
		},
		{
			ID: 44, Category: "performance",
			User:     "The steward sweep is taking 30+ seconds on 10K memories.",
			Assistant: "The steward's scoring phase queries AverageRetrievalScore for each memory individually — that's 10K separate SQL queries. The fix is to batch: SELECT memory_id, AVG(score) FROM retrieval_events GROUP BY memory_id returns all averages in one query. The current implementation was written for the MongoDB backend where aggregation pipelines were more expensive than individual finds. With Postgres, a single GROUP BY is always faster. After the fix, the 10K-memory sweep drops from 30s to ~2s.",
			Expected: "store",
			Notes:    "N+1 query pattern with Postgres-specific fix",
		},
		{
			ID: 45, Category: "performance",
			User:     "Can I run pgmemory on a Raspberry Pi?",
			Assistant: "Tested on Pi 4 (4GB RAM, arm64). pgmemory daemon + embedded Postgres runs fine (~200MB total). The bottleneck is llama-server: voyage-4-nano Q8_0 inference takes ~50ms per embed on Pi 4 vs ~5ms on M2. With 50ms per embed, the write pipeline processes ~20 texts/second and the read pipeline adds 50ms latency to each query. Usable for personal use but too slow for team deployment. Reducing the model to Q4_K_M cuts inference to ~25ms but degrades embedding quality — dedup precision drops from 97% to 89% at the 0.92 threshold.",
			Expected: "store",
			Notes:    "Platform constraints with quantified degradation",
		},
	}
}

// --- Should store: API/schema facts (5) ---

func apiExchanges() []Exchange {
	return []Exchange{
		{
			ID: 46, Category: "api",
			User:     "What endpoints does the proxy expose?",
			Assistant: "The proxy exposes: POST /v1/messages (Anthropic proxy — forwards to upstream, captures response), GET/POST /api/store (direct memory storage via pipeline), POST /api/ingest (Q&A pair ingestion with synthesis), GET /api/memories (list all memories, optional ?q= filter), GET/DELETE/PUT /api/memories/:id (CRUD by ID), GET /api/sources (list ingested sources), POST /api/sources/ingest (crawl a URL), DELETE /api/sources/:id (remove source and its memories), GET /api/search?q= (semantic search), GET /health (status, mode, version, synthesis availability), GET / (dashboard HTML). All /api/* endpoints require Authorization: Bearer token.",
			Expected: "store",
			Notes:    "Complete API surface documentation",
		},
		{
			ID: 47, Category: "api",
			User:     "What's the schema for the retrieval_events table?",
			Assistant: "retrieval_events tracks every time a memory is returned by the read pipeline: id BIGSERIAL PRIMARY KEY, memory_id TEXT NOT NULL (hex ObjectID referencing memories.id), score DOUBLE PRECISION NOT NULL (cosine similarity at retrieval time), created_at TIMESTAMPTZ NOT NULL DEFAULT now(). Indexed on memory_id for the steward's AverageRetrievalScore query. No foreign key constraint to memories — if a memory is deleted, its retrieval events become orphaned but harmless. The steward could clean these up but currently doesn't.",
			Expected: "store",
			Notes:    "Schema detail with design tradeoff",
		},
		{
			ID: 48, Category: "api",
			User:     "How does the MCP memory_store tool differ from the proxy capture path?",
			Assistant: "MCP memory_store calls POST /api/store which runs ProcessFiltered: preprocess, chunk, noise filter, redact, embed, topic group, dedup, store. No LLM synthesis. The proxy capture path runs gateAndBuffer first: discovery signal check, pre-filter (QuickFilter), length gate, content score pre-gate, code stripping — then SynthesizeQA for fact extraction — then stores each extracted fact via ProcessDirect. Key difference: MCP stores whatever you give it (after noise/dedup filtering); proxy only stores what Haiku considers a non-obvious fact.",
			Expected: "store",
			Notes:    "Path comparison with quality gate difference",
		},
		{
			ID: 49, Category: "api",
			User:     "What metadata fields does the store support?",
			Assistant: "The metadata JSONB column accepts arbitrary key-value pairs. Currently used fields: supersedes (string, hex ID of the memory this one updates), extends_source (string, source label of the extended memory), extends_memory (string, hex ID of the extended memory), extends_score (float, cosine similarity with the extended memory). The pipeline preserves any metadata passed by the caller — MCP memory_store and /api/store both accept a metadata field in the JSON body. The dashboard displays metadata as collapsed JSON in the memory detail view.",
			Expected: "store",
			Notes:    "Metadata schema with current usage",
		},
		{
			ID: 50, Category: "api",
			User:     "What's the format of the injected context in the system prompt?",
			Assistant: "FormatContext wraps retrieved memories in XML-like tags with numbered entries. The output looks like: <memory> newline 1. First fact here newline 2. Second fact here newline </memory>. Each entry is just the memory content with a number prefix — no source, no score, no metadata. The token budget (retrieval_max_tokens in config, default 2048) limits how many memories are included. The budget is estimated at 4 characters per token. Memories are ordered by the retrieval pipeline's ranking (RRF + MMR), not by creation time.",
			Expected: "store",
			Notes:    "Injection format specification",
		},
	}
}

// --- Should store: discovery signal bypass (5) ---

func discoveryExchanges() []Exchange {
	return []Exchange{
		{
			ID: 51, Category: "discovery",
			User:     "ok",
			Assistant: "Interesting! The config parser silently ignores unknown keys. When you typo 'dedup_threshhold' (double h) instead of 'dedup_threshold', the default 0.92 is used and no warning is logged. This means your custom threshold was never applied. I added strict parsing with yaml.KnownFields(true) so unknown keys cause a startup error.",
			Expected: "store",
			Notes:    "Discovery signal 'Interesting!' bypasses pre-filter despite 'ok' user ack",
		},
		{
			ID: 52, Category: "discovery",
			User:     "sure",
			Assistant: "Turns out the issue is more subtle than I thought. The proxy's http.Client inherits the default transport's connection pooling, which means connections to the upstream Anthropic API are reused across requests. But when the upstream rotates its TLS certificate (which it does every 90 days), existing connections fail with a TLS handshake error on the next request. The client retries automatically, but the retry uses a new connection, so the user sees a ~500ms latency spike every 90 days.",
			Expected: "store",
			Notes:    "Discovery signal 'Turns out' bypasses pre-filter despite 'sure' user ack",
		},
		{
			ID: 53, Category: "discovery",
			User:     "go ahead",
			Assistant: "Wait — the HNSW index ef_construction parameter is set to 64, but the search uses ef_search=40. ef_search should never exceed ef_construction for optimal recall. With ef_search=40 and ef_construction=64, the index has excess construction precision that's never utilized in search. Either lower ef_construction to 40 (faster index builds, same search quality) or raise ef_search to 64 (better recall, slightly slower search). The current config wastes ~30% of index build time.",
			Expected: "store",
			Notes:    "Discovery signal 'Wait —' bypasses pre-filter despite 'go ahead' user ack",
		},
		{
			ID: 54, Category: "discovery",
			User:     "thanks",
			Assistant: "Found it! The reason the content scorer's quality prototypes weren't working is that they're embedded at startup using the same llama-server, but the server isn't ready yet — it's still loading the model. The embed call returns a zero vector, and cosine similarity with a zero vector is always 0. The scorer then reports score=0.5 (neutral) for everything because avgQuality and topNoise are both 0. The fix: wait for llama-server health check before embedding prototypes.",
			Expected: "store",
			Notes:    "Discovery signal 'Found it!' bypasses pre-filter despite 'thanks' user ack",
		},
		{
			ID: 55, Category: "discovery",
			User:     "yep",
			Assistant: "The root cause is that Postgres HNSW index doesn't support filtering in the index scan itself — the WHERE clause is applied after the index returns candidates. So when you filter by source='claude-code', Postgres fetches top-100 from HNSW (all sources mixed), then filters to source='claude-code', potentially returning fewer than topK results. The workaround is to overfetch: request 4x topK from the index, then filter and trim. This is already implemented in filteredVectorSearch but the multiplier was 2x, which wasn't enough for stores with many sources.",
			Expected: "store",
			Notes:    "Discovery signal 'The root cause is' bypasses pre-filter despite 'yep' user ack",
		},
	}
}

// --- Should filter: pre-filter (ack + procedural) (15) ---

func prefilterExchanges() []Exchange {
	return []Exchange{
		{
			ID: 56, Category: "prefilter",
			User:     "ok",
			Assistant: "I've updated the configuration file with the new Redis timeout values and restarted the service.",
			Expected: "filter_prefilter",
			Notes:    "Short ack 'ok' + procedural prefix 'I've updated'",
		},
		{
			ID: 57, Category: "prefilter",
			User:     "thanks",
			Assistant: "I've added the missing import statement and the tests are passing now.",
			Expected: "filter_prefilter",
			Notes:    "Short ack 'thanks' + procedural prefix 'I've added'",
		},
		{
			ID: 58, Category: "prefilter",
			User:     "go ahead",
			Assistant: "I'll start by reading the configuration file to understand the current setup.",
			Expected: "filter_prefilter",
			Notes:    "Short ack 'go ahead' + procedural prefix 'I'll '",
		},
		{
			ID: 59, Category: "prefilter",
			User:     "sure",
			Assistant: "I've fixed the typo in the variable name and updated all references across the codebase.",
			Expected: "filter_prefilter",
			Notes:    "Short ack 'sure' + procedural prefix 'I've fixed'",
		},
		{
			ID: 60, Category: "prefilter",
			User:     "yes",
			Assistant: "I'll implement the changes we discussed and run the test suite to verify nothing breaks.",
			Expected: "filter_prefilter",
			Notes:    "Short ack 'yes' + procedural prefix 'I'll '",
		},
		{
			ID: 61, Category: "prefilter",
			User:     "sounds good",
			Assistant: "I've created the new migration file and applied it to the development database successfully.",
			Expected: "filter_prefilter",
			Notes:    "Short ack 'sounds good' + procedural prefix 'I've created'",
		},
		{
			ID: 62, Category: "prefilter",
			User:     "please",
			Assistant: "Let me check the error logs to see what's causing the test failures in CI.",
			Expected: "filter_prefilter",
			Notes:    "Short ack 'please' + procedural prefix 'Let me check'",
		},
		{
			ID: 63, Category: "prefilter",
			User:     "perfect",
			Assistant: "I've implemented the retry logic with exponential backoff as we discussed.",
			Expected: "filter_prefilter",
			Notes:    "Short ack 'perfect' + procedural prefix 'I've implemented'",
		},
		{
			ID: 64, Category: "prefilter",
			User:     "continue",
			Assistant: "I will proceed with refactoring the authentication middleware to use the new token format.",
			Expected: "filter_prefilter",
			Notes:    "Short ack 'continue' + procedural prefix 'I will '",
		},
		{
			ID: 65, Category: "prefilter",
			User:     "great",
			Assistant: "I've removed the deprecated function calls and replaced them with the new API surface.",
			Expected: "filter_prefilter",
			Notes:    "Short ack 'great' + procedural prefix 'I've removed'",
		},
		{
			ID: 66, Category: "prefilter",
			User:     "ok",
			Assistant: "Let me read the source file to understand the current implementation before making changes.",
			Expected: "filter_prefilter",
			Notes:    "Short ack 'ok' + procedural prefix 'Let me read'",
		},
		{
			ID: 67, Category: "prefilter",
			User:     "got it",
			Assistant: "I've written the unit tests for the new validation logic and all assertions pass.",
			Expected: "filter_prefilter",
			Notes:    "Short ack 'got it' + procedural prefix 'I've written'",
		},
		{
			ID: 68, Category: "prefilter",
			User:     "yep",
			Assistant: "I've modified the Dockerfile to use multi-stage builds for smaller image size.",
			Expected: "filter_prefilter",
			Notes:    "Short ack 'yep' + procedural prefix 'I've modified'",
		},
		{
			ID: 69, Category: "prefilter",
			User:     "okay",
			Assistant: "Done. The migration has been applied and the new column is available in the schema.",
			Expected: "filter_prefilter",
			Notes:    "Short ack 'okay' + procedural prefix 'Done.'",
		},
		{
			ID: 70, Category: "prefilter",
			User:     "👍",
			Assistant: "I've completed the refactoring and pushed the changes to the feature branch.",
			Expected: "filter_prefilter",
			Notes:    "Short ack '👍' + procedural prefix 'I've completed'",
		},
	}
}

// --- Should filter: length gate (assistant < 80 chars) (10) ---

func lengthExchanges() []Exchange {
	return []Exchange{
		{
			ID: 71, Category: "length",
			User:     "Does it support TLS?",
			Assistant: "Yes, TLS 1.2 and 1.3 are both supported.",
			Expected: "filter_length",
			Notes:    "40 chars — under 80 threshold",
		},
		{
			ID: 72, Category: "length",
			User:     "What port does the embedder use?",
			Assistant: "Port 7433 by default.",
			Expected: "filter_length",
			Notes:    "21 chars — well under threshold",
		},
		{
			ID: 73, Category: "length",
			User:     "Is the steward running?",
			Assistant: "Yes, it runs every 60 minutes by default.",
			Expected: "filter_length",
			Notes:    "41 chars — under threshold",
		},
		{
			ID: 74, Category: "length",
			User:     "What language is this written in?",
			Assistant: "Go, with some shell scripts for installation.",
			Expected: "filter_length",
			Notes:    "46 chars — under threshold",
		},
		{
			ID: 75, Category: "length",
			User:     "Can I change the model?",
			Assistant: "Yes, set model_path in ~/.pgmemory/config.yaml.",
			Expected: "filter_length",
			Notes:    "48 chars — under threshold",
		},
		{
			ID: 76, Category: "length",
			User:     "What's the default top-k?",
			Assistant: "5 results by default, configurable via retrieval_top_k.",
			Expected: "filter_length",
			Notes:    "55 chars — under threshold",
		},
		{
			ID: 77, Category: "length",
			User:     "Is there a web UI?",
			Assistant: "Yes, the dashboard is at http://127.0.0.1:7432.",
			Expected: "filter_length",
			Notes:    "48 chars — under threshold",
		},
		{
			ID: 78, Category: "length",
			User:     "How do I check the version?",
			Assistant: "Run pgmemory version or check GET /health.",
			Expected: "filter_length",
			Notes:    "44 chars — under threshold",
		},
		{
			ID: 79, Category: "length",
			User:     "Does it work on Linux?",
			Assistant: "Yes, both amd64 and arm64 Linux are supported.",
			Expected: "filter_length",
			Notes:    "47 chars — under threshold",
		},
		{
			ID: 80, Category: "length",
			User:     "What's the license?",
			Assistant: "It's currently source-available, not open source.",
			Expected: "filter_length",
			Notes:    "50 chars — under threshold",
		},
	}
}

// --- Should filter: noise (low quality content) (15) ---

func noiseExchanges() []Exchange {
	return []Exchange{
		{
			ID: 81, Category: "noise",
			User:     "",
			Assistant: "....!!!!????....!!!!????....!!!!????....!!!!????",
			Expected: "filter_length",
			Notes:    "All punctuation, 48 chars — length gate fires first (< 80)",
		},
		{
			ID: 82, Category: "noise",
			User:     "",
			Assistant: "---\n---\n---\n---\n---\n---\n---\n---\n---\n---\n---\n---\n---\n---\n---\n---\n---",
			Expected: "filter_length",
			Notes:    "Separator lines, 67 chars — length gate fires first (< 80)",
		},
		{
			ID: 83, Category: "noise",
			User:     "",
			Assistant: "                                                                                                    ",
			Expected: "filter_length",
			Notes:    "All whitespace — 0 chars after trim, length gate fires",
		},
		{
			ID: 84, Category: "noise",
			User:     "",
			Assistant: "```\nfunc main() {\n    fmt.Println(\"hello\")\n}\n```",
			Expected: "filter_length",
			Notes:    "Code block, 48 chars — length gate fires first (< 80)",
		},
		{
			ID: 85, Category: "noise",
			User:     "",
			Assistant: ">>> === +++ --- <<< === +++ --- >>> === +++ --- <<< === +++ --- >>> === +++ ---",
			Expected: "filter_length",
			Notes:    "Diff markers, 78 chars — length gate fires first (< 80)",
		},
		{
			ID: 86, Category: "noise",
			User:     "",
			Assistant: "# # # # # # # # # # # # # # # # # # # # # # # # # # # # # # # # # # # # # # #",
			Expected: "filter_length",
			Notes:    "Hash marks, 79 chars — length gate fires first (< 80)",
		},
		{
			ID: 87, Category: "noise",
			User:     "",
			Assistant: "| | | | | | | | | | | | | | | | | | | | | | | | | | | | | | | | | | | | | | |",
			Expected: "filter_length",
			Notes:    "Table formatting, 79 chars — length gate fires first (< 80)",
		},
		{
			ID: 88, Category: "noise",
			User:     "What should I do next?",
			Assistant: "Well, you know, it really depends on what you're trying to accomplish here, and there are several different approaches you could potentially consider taking, each with their own unique set of advantages and disadvantages that would need to be carefully weighed against one another before making a final determination about the best path forward for your particular situation and use case.",
			Expected: "store",
			Notes:    "Verbose filler BUT passes all string-level gates (>80 chars, >40% alnum). Without content score gate (disabled for hash embedder), this stores.",
		},
		{
			ID: 89, Category: "noise",
			User:     "",
			Assistant: "...",
			Expected: "filter_length",
			Notes:    "Ellipsis, 3 chars — length gate fires first (< 80)",
		},
		{
			ID: 90, Category: "noise",
			User:     "",
			Assistant: "OK\nOK\nOK\nOK\nOK\nOK\nOK\nOK\nOK\nOK\nOK\nOK\nOK\nOK\nOK\nOK\nOK\nOK\nOK\nOK",
			Expected: "filter_length",
			Notes:    "Repeated OK, 60 chars — length gate fires first (< 80)",
		},
		{
			ID: 91, Category: "noise",
			User:     "",
			Assistant: "✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓ ✓",
			Expected: "filter_noise",
			Notes:    "Unicode checkmarks with spaces — low alnum ratio (checkmarks are Letter category in Go unicode.IsLetter but are not ASCII alnum... actually unicode.IsLetter returns true for these)",
		},
		{
			ID: 92, Category: "noise",
			User:     "",
			Assistant: "a",
			Expected: "filter_length",
			Notes:    "Single character, 1 char — length gate fires first (< 80)",
		},
		{
			ID: 93, Category: "noise",
			User:     "",
			Assistant: "no",
			Expected: "filter_length",
			Notes:    "Two chars — length gate fires first (< 80)",
		},
		{
			ID: 94, Category: "noise",
			User:     "",
			Assistant: "Here is what I found:\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\nEnd of findings.",
			Expected: "filter_length",
			Notes:    "62 chars with embedded newlines — length gate fires first (< 80)",
		},
		{
			ID: 95, Category: "noise",
			User:     "",
			Assistant: "/* ============================================= */\n/* ============================================= */\n/* ============================================= */",
			Expected: "filter_noise",
			Notes:    "CSS-style comment blocks — low alnum ratio",
		},
	}
}

// --- Should dedup: near-duplicates of stored items (10) ---

func dedupExchanges() []Exchange {
	return []Exchange{
		{
			ID: 96, Category: "dedup",
			User:     "What's the default port and why?",
			Assistant: "pgmemory defaults to port 7432 because 5432 is standard Postgres and 7433 is used by the local llama-server for embeddings. The proxy needs a port that won't conflict with either. 7432 was chosen as a mnemonic: 7000 + 432 (Postgres default suffix). The MCP server runs on stdin/stdout so it doesn't use a port at all — it's invoked as a subprocess by Claude Code.",
			Expected:    "dedup",
			DuplicateOf: 19,
			Notes:       "Exact duplicate of exchange #19",
		},
		{
			ID: 97, Category: "dedup",
			User:     "What port does pgmemory use by default?",
			Assistant: "pgmemory defaults to port 7432 since 5432 is standard Postgres and 7433 is the local llama-server for embeddings. The proxy picks a port that avoids conflicts. 7432 is a mnemonic: 7000 + 432. The MCP server uses stdin/stdout, no port — it's a subprocess of Claude Code.",
			Expected:    "store",
			DuplicateOf: 19,
			Notes:       "Paraphrase of #19 — hash embedder can't detect semantic paraphrases (real embedder would dedup)",
		},
		{
			ID: 98, Category: "dedup",
			User:     "Why use pgvector instead of a dedicated vector database?",
			Assistant: "pgvector was chosen over Pinecone/Qdrant/Weaviate for three reasons: (1) single-process deployment — pgmemory runs as one binary with embedded Postgres, no external service to manage; (2) hybrid search — Postgres GIN indexes for full-text search combined with HNSW for vector search, merged via Reciprocal Rank Fusion, is more effective than vector-only retrieval for code facts; (3) ACID transactions — memory updates (supersession tagging, quality score decay) need atomic read-modify-write, which dedicated vector DBs handle poorly.",
			Expected:    "dedup",
			DuplicateOf: 11,
			Notes:       "Exact duplicate of exchange #11",
		},
		{
			ID: 99, Category: "dedup",
			User:     "What are the available API endpoints?",
			Assistant: "The proxy exposes: POST /v1/messages (Anthropic proxy — forwards to upstream, captures response), GET/POST /api/store (direct memory storage via pipeline), POST /api/ingest (Q&A pair ingestion with synthesis), GET /api/memories (list all memories, optional ?q= filter), GET/DELETE/PUT /api/memories/:id (CRUD by ID), GET /api/sources (list ingested sources), POST /api/sources/ingest (crawl a URL), DELETE /api/sources/:id (remove source and its memories), GET /api/search?q= (semantic search), GET /health (status, mode, version, synthesis availability), GET / (dashboard HTML). All /api/* endpoints require Authorization: Bearer token.",
			Expected:    "dedup",
			DuplicateOf: 46,
			Notes:       "Exact duplicate of exchange #46",
		},
		{
			ID: 100, Category: "dedup",
			User:     "Getting 'could not resize shared memory segment: No space left on device' when running in Docker.",
			Assistant: "This is a Docker shared memory (shm) limit issue, not disk space. Postgres uses shared memory for its buffer pool, and Docker defaults /dev/shm to 64MB. With shared_buffers set to 128MB in the embedded Postgres, it exceeds the Docker limit. Fix: add --shm-size=256m to docker run, or reduce shared_buffers to 32MB in the embedded Postgres config. The pgmemory installer already sets shared_buffers=64MB to avoid this, but manual Docker runs don't inherit that setting.",
			Expected:    "dedup",
			DuplicateOf: 27,
			Notes:       "Exact duplicate of exchange #27",
		},
		{
			ID: 101, Category: "dedup",
			User:     "Docker shared memory error with Postgres",
			Assistant: "This is a Docker shm limit problem, not actual disk space. Postgres uses shared memory for its buffer pool, Docker defaults /dev/shm to 64MB. With shared_buffers at 128MB in the embedded Postgres, it exceeds the Docker limit. Fix: add --shm-size=256m to docker run, or reduce shared_buffers to 32MB. The pgmemory installer sets shared_buffers=64MB to avoid this, but manual Docker runs don't inherit that.",
			Expected:    "store",
			DuplicateOf: 27,
			Notes:       "Paraphrase of #27 — hash embedder can't detect semantic paraphrases (real embedder would dedup)",
		},
		{
			ID: 102, Category: "dedup",
			User:     "How fast is retrieval?",
			Assistant: "With 50K memories and the HNSW index (m=16, ef_search=40), the read pipeline completes in 15-25ms end-to-end on an M2 MacBook: ~5ms for embedding the query via llama-server, ~3ms for HNSW search, ~2ms for FTS, ~3ms for RRF merge and MMR reranking, ~2ms for FormatContext. The bottleneck is the embedding call — if llama-server is under load from concurrent write embeds, query latency can spike to 100ms+. The proxy adds ~2ms of overhead on top of read pipeline latency.",
			Expected:    "dedup",
			DuplicateOf: 41,
			Notes:       "Exact duplicate of exchange #41",
		},
		{
			ID: 103, Category: "dedup",
			User:     "How does the MCP store differ from proxy capture?",
			Assistant: "MCP memory_store calls POST /api/store which runs ProcessFiltered: preprocess, chunk, noise filter, redact, embed, topic group, dedup, store. No LLM synthesis. The proxy capture path runs gateAndBuffer first: discovery signal check, pre-filter (QuickFilter), length gate, content score pre-gate, code stripping — then SynthesizeQA for fact extraction — then stores each extracted fact via ProcessDirect. Key difference: MCP stores whatever you give it (after noise/dedup filtering); proxy only stores what Haiku considers a non-obvious fact.",
			Expected:    "dedup",
			DuplicateOf: 48,
			Notes:       "Exact duplicate of exchange #48",
		},
		{
			ID: 104, Category: "dedup",
			User:     "What version of the Anthropic API does the proxy target?",
			Assistant: "The proxy sets anthropic-version: 2023-06-01 in forwarded requests. It handles both streaming (SSE) and non-streaming response formats. The proxy does not use the beta features header (anthropic-beta), so tool use, computer use, and other beta features are passed through unchanged. The proxy only reads the response content — it does not interpret tool_use blocks or modify the conversation structure. Upgrading the API version requires testing that extractResponseText and extractTextDelta still parse the response format correctly.",
			Expected:    "dedup",
			DuplicateOf: 37,
			Notes:       "Exact duplicate of exchange #37",
		},
		{
			ID: 105, Category: "dedup",
			User:     "Why did we pick voyage-4-nano?",
			Assistant: "voyage-4-nano (Q8_0, ~70MB) was chosen for local-first deployment: it runs on CPU without a GPU, starts in <2 seconds, and produces 1024-dim vectors that are competitive with larger models for code-related text. The alternative was voyage-3 via API, but that adds network latency to every embed call (~100ms) and costs money. For a memory system that embeds on every write and every read, local embeddings at 0 marginal cost were the priority. The quality tradeoff: voyage-4-nano scores ~3% lower on MTEB code retrieval benchmarks than voyage-3.",
			Expected:    "dedup",
			DuplicateOf: 38,
			Notes:       "Exact duplicate of exchange #38",
		},
	}
}
