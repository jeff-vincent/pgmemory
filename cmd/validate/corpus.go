package main

// corpus.go provides a known corpus of technical content for validation.
// The corpus is organized into categories with expected behaviors:
//
//   - "high-value": technical decisions, architecture, debugging — should survive pruning
//   - "noise": greetings, filler, acknowledgments — should get filtered or pruned
//   - "duplicates": near-identical phrasings of the same fact — should get deduped/merged
//   - "go-proverbs": the canonical Go proverbs — high-quality, diverse topics
//   - "mongodb-ops": operational knowledge about MongoDB — realistic domain content

// corpusEntry is a single piece of content with expected behavior metadata.
type corpusEntry struct {
	Content  string
	Category string // high-value, noise, duplicate, go-proverb, mongodb-ops
	Topic    string // sub-topic for grouping
}

// loadCorpus returns the full validation corpus.
func loadCorpus() []corpusEntry {
	var corpus []corpusEntry
	corpus = append(corpus, goProverbs()...)
	corpus = append(corpus, highValueEntries()...)
	corpus = append(corpus, noiseEntries()...)
	corpus = append(corpus, duplicateEntries()...)
	corpus = append(corpus, mongodbOpsEntries()...)
	corpus = append(corpus, securityPatternEntries()...)
	corpus = append(corpus, incidentRunbookEntries()...)
	corpus = append(corpus, apiContractEntries()...)
	return corpus
}

// goProverbs returns the canonical Go proverbs by Rob Pike.
// These are concise, high-signal technical wisdom — they should all survive.
func goProverbs() []corpusEntry {
	proverbs := []string{
		"Don't communicate by sharing memory, share memory by communicating.",
		"Concurrency is not parallelism.",
		"Channels orchestrate; mutexes serialize.",
		"The bigger the interface, the weaker the abstraction.",
		"Make the zero value useful.",
		"interface{} says nothing.",
		"Gofmt's style is no one's favorite, yet gofmt is everyone's favorite.",
		"A little copying is better than a little dependency.",
		"Syscall must always be guarded with build tags.",
		"Cgo must always be guarded with build tags.",
		"Cgo is not Go.",
		"With the unsafe package there are no guarantees.",
		"Clear is better than clever.",
		"Reflection is never clear.",
		"Errors are values.",
		"Don't just check errors, handle them gracefully.",
		"Design the architecture, name the components, document the details.",
		"Documentation is for users.",
		"Don't panic.",
	}
	var entries []corpusEntry
	for _, p := range proverbs {
		entries = append(entries, corpusEntry{
			Content:  p,
			Category: "go-proverb",
			Topic:    "go-philosophy",
		})
	}
	return entries
}

// highValueEntries are technical decisions and architecture notes that
// should be scored highly and survive steward pruning.
func highValueEntries() []corpusEntry {
	return []corpusEntry{
		{
			Content:  "The write pipeline deduplicates at cosine similarity >= 0.92. This threshold was tuned empirically: at 0.90 too many paraphrases got through, at 0.95 legitimate updates were blocked. The sweet spot keeps unique information while preventing near-identical chunks from accumulating.",
			Category: "high-value",
			Topic:    "pipeline-design",
		},
		{
			Content:  "MongoDB Atlas vector search uses the $vectorSearch aggregation stage with numCandidates set to 20x topK. This oversampling ratio was chosen after benchmarking: at 10x recall dropped to 85%, at 20x it's 97%, and 50x only gains 1% more recall at 3x the latency cost.",
			Category: "high-value",
			Topic:    "search-tuning",
		},
		{
			Content:  "The steward's quality scoring formula uses log2(hit_count+1) normalized against the max, multiplied by an exponential decay factor with a 90-day half-life. New memories start at 0.5 base score. This gives recently-useful memories the highest scores while letting stale ones decay gracefully.",
			Category: "high-value",
			Topic:    "quality-scoring",
		},
		{
			Content:  "Security redaction runs BEFORE embedding and storage, never at retrieval time. This was a deliberate design decision: if we only redacted at retrieval, the vector store would contain raw secrets, and any direct database access would expose them. Defense in depth requires cleaning data at the earliest boundary.",
			Category: "high-value",
			Topic:    "security",
		},
		{
			Content:  "The embedding model is voyage-4-nano running locally via llama.cpp, producing 1024-dimensional vectors with cosine similarity. We chose local inference over API calls because: (1) no data leaves the machine, (2) zero marginal cost, (3) ~5ms per embedding vs ~200ms for API calls. The Q8_0 quantization preserves 99.2% of the full model's retrieval accuracy.",
			Category: "high-value",
			Topic:    "embedding",
		},
		{
			Content:  "The chunker detects content structure: headings, code blocks, lists, and tables. It splits at paragraph boundaries under a 512-token budget. Each chunk carries its nearest heading as a context prefix when split. This preserves semantic coherence — a chunk about 'Error Handling' under a '## Configuration' heading keeps that context.",
			Category: "high-value",
			Topic:    "chunking",
		},
		{
			Content:  "Hybrid search on Atlas proper combines $vectorSearch (semantic) with $search (keyword) using Reciprocal Rank Fusion. RRF with k=60 was chosen over simple score averaging because it's rank-based — immune to score distribution differences between the two search types. MMR re-ranking with lambda=0.7 adds diversity.",
			Category: "high-value",
			Topic:    "search-tuning",
		},
		{
			Content:  "The source ingestion pipeline uses BFS crawling with SHA-256 change detection per page. On re-crawl, only pages whose content hash changed get re-chunked and re-embedded. This makes refreshing a 500-page docs site take seconds instead of minutes. The pipe separator in source names (source:NAME|URL) enables page-level tracking.",
			Category: "high-value",
			Topic:    "ingestion",
		},
		{
			Content:  "Connection pooling for MongoDB is set to maxPoolSize=50 after load testing showed the default 100 was excessive. At 50 connections, p99 latency for vector search stays under 15ms with 50K documents. The directConnection=true parameter is required for single-node Atlas Local deployments.",
			Category: "high-value",
			Topic:    "mongodb-tuning",
		},
		{
			Content:  "The MCP server implements stdio transport for Claude Code integration. Tools: memory_search, memory_store, memory_list, memory_delete, source_ingest, source_list, source_remove, quality_stats. Each tool has a JSON schema for input validation. The server runs as a subprocess managed by Claude Code's MCP configuration.",
			Category: "high-value",
			Topic:    "mcp",
		},
		{
			Content:  "Topic boundary detection walks consecutive chunk embeddings and splits where cosine similarity drops below 0.65. Groups are also split at 2048 chars (512 tokens) to fit the embedding model's context window. Multi-chunk groups get joined and re-embedded so the stored vector represents the full merged text.",
			Category: "high-value",
			Topic:    "pipeline-design",
		},
		{
			Content:  "The content quality scorer uses prototype-based classification: it embeds generic descriptions of high-value content (technical decisions, debugging solutions, config instructions) and noise (greetings, filler). New chunks are scored by their ratio of similarity to quality prototypes vs noise prototypes. This requires no domain training.",
			Category: "high-value",
			Topic:    "quality-scoring",
		},
	}
}

// noiseEntries are low-value content that should be filtered by the write
// pipeline (too short, too noisy) or pruned by the steward (never retrieved).
func noiseEntries() []corpusEntry {
	return []corpusEntry{
		{Content: "Sure, I can help with that!", Category: "noise", Topic: "filler"},
		{Content: "Let me know if you need anything else.", Category: "noise", Topic: "filler"},
		{Content: "Great question! Here's what I found.", Category: "noise", Topic: "filler"},
		{Content: "I hope this helps!", Category: "noise", Topic: "filler"},
		{Content: "You're welcome!", Category: "noise", Topic: "filler"},
		{Content: "OK", Category: "noise", Topic: "filler"},
		{Content: "...", Category: "noise", Topic: "filler"},
		{Content: "¯\\_(ツ)_/¯", Category: "noise", Topic: "filler"},
		{Content: "Absolutely, happy to assist you with that request. Please feel free to ask any follow-up questions you might have about this topic or any related subjects.", Category: "noise", Topic: "verbose-filler"},
		{Content: "I understand your concern. Let me take a look at this and get back to you with a comprehensive answer that addresses all aspects of your question.", Category: "noise", Topic: "verbose-filler"},
		{Content: "That's a really interesting observation! I think there are several angles we could explore here.", Category: "noise", Topic: "verbose-filler"},
		{Content: "Before I begin, I want to make sure I understand your question correctly.", Category: "noise", Topic: "verbose-filler"},
	}
}

// duplicateEntries are near-identical phrasings of the same facts.
// The steward should merge these during its sweep.
func duplicateEntries() []corpusEntry {
	return []corpusEntry{
		// Cluster A: embedding dimensions
		{
			Content:  "The voyage-4-nano model produces 1024-dimensional embedding vectors. These are stored as float32 arrays in MongoDB.",
			Category: "duplicate",
			Topic:    "embedding-dim",
		},
		{
			Content:  "voyage-4-nano embeddings are 1024 dimensions. Each dimension is a float32 value stored in the MongoDB memories collection.",
			Category: "duplicate",
			Topic:    "embedding-dim",
		},
		{
			Content:  "Embedding dimensions: 1024 (voyage-4-nano model, float32 vectors, MongoDB storage).",
			Category: "duplicate",
			Topic:    "embedding-dim",
		},
		// Cluster B: dedup threshold
		{
			Content:  "The deduplication threshold is set to cosine similarity 0.92. Chunks scoring above this against existing memories are skipped as duplicates.",
			Category: "duplicate",
			Topic:    "dedup-threshold",
		},
		{
			Content:  "Dedup threshold: 0.92 cosine similarity. When a new chunk's embedding scores >= 0.92 against any stored memory, it's considered a duplicate and not stored.",
			Category: "duplicate",
			Topic:    "dedup-threshold",
		},
		// Cluster C: port config
		{
			Content:  "memoryd listens on port 7432 by default. The llama-server embedding subprocess runs on port 7433. Both bind to 127.0.0.1 only.",
			Category: "duplicate",
			Topic:    "ports",
		},
		{
			Content:  "Default port is 7432 for the memoryd daemon, 7433 for the embedding server (llama-server). Both are localhost-only for security.",
			Category: "duplicate",
			Topic:    "ports",
		},
	}
}

// securityPatternEntries are security-focused architecture decisions and
// patterns that should be treated as high-value and survive pruning.
func securityPatternEntries() []corpusEntry {
	return []corpusEntry{
		{
			Content:  "Authentication tokens must never be logged. The middleware strips Authorization headers from access logs before writing them. Any log aggregation pipeline that ingests raw HTTP request logs will expose tokens if this middleware is bypassed or disabled.",
			Category: "high-value",
			Topic:    "auth-security",
		},
		{
			Content:  "The redaction pipeline runs BEFORE embedding — not at retrieval time. This is defense-in-depth: if we only redacted at retrieval, the vector database would contain raw secret embeddings, and any direct database access (backups, analytics, incident response) would expose them.",
			Category: "high-value",
			Topic:    "redaction-design",
		},
		{
			Content:  "TLS termination happens at the load balancer, not at the application. Internal service-to-service traffic uses mTLS with certificates rotated every 30 days by cert-manager. The memoryd proxy binds to 127.0.0.1 only — never 0.0.0.0 — to prevent unintentional network exposure.",
			Category: "high-value",
			Topic:    "network-security",
		},
		{
			Content:  "MongoDB Atlas connection strings with credentials must be stored in the OS keychain (macOS Keychain or Linux libsecret), not in config.yaml. The credential package handles this transparently. Plain-text URIs in config files are accepted only in development mode with an explicit warning.",
			Category: "high-value",
			Topic:    "credential-storage",
		},
		{
			Content:  "SSRF protection: the source ingestion crawler only follows URLs on the allowlist domain. Before crawling any URL, it checks against a blocklist of private IP ranges (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8) to prevent crawling internal services.",
			Category: "high-value",
			Topic:    "ssrf",
		},
	}
}

// incidentRunbookEntries are operational debugging runbooks — high-signal
// knowledge that engineers want to retrieve during incidents.
func incidentRunbookEntries() []corpusEntry {
	return []corpusEntry{
		{
			Content:  "Symptom: /api/search returns {\"context\":\"\"} for queries that previously returned results. Most likely cause: the vector search index was dropped or is in BUILDING state (happens after container restart). Fix: re-run scripts/create_index.js. Verify with: db.memories.aggregate([{$listSearchIndexes:{}}]).",
			Category: "high-value",
			Topic:    "incident-search-empty",
		},
		{
			Content:  "Symptom: write pipeline logs 'embedding request failed: context deadline exceeded'. Cause: llama-server on port 7433 is not running or is overloaded. Fix: restart llama-server with the same model path. Check memory usage — the model requires ~800MB RAM. If OOM killed, reduce batch size in config.",
			Category: "high-value",
			Topic:    "incident-embedding-down",
		},
		{
			Content:  "Symptom: memoryd starts but /health returns 'connection refused'. Check in order: (1) is port 7432 already in use? (lsof -i :7432), (2) did MongoDB come up before memoryd tried to connect? (startup doesn't retry), (3) is the config file missing or malformed? (memoryd start --v for verbose logs).",
			Category: "high-value",
			Topic:    "incident-startup-fail",
		},
		{
			Content:  "Symptom: source ingestion results in duplicate memories for every re-crawl. Cause: the page content hash is computed on raw HTML including nav bars and timestamps that change every request. Fix: ensure the crawler normalizes content (strips nav, header, footer) before hashing. Workaround: increase the dedup threshold temporarily.",
			Category: "high-value",
			Topic:    "incident-source-dupe",
		},
		{
			Content:  "Symptom: steward sweep reports pruned=0 even with hundreds of old zero-hit memories. Cause: PruneGracePeriod may be set too high, or PruneThreshold is too low. Check steward config in ~/.memoryd/config.yaml under the 'steward' key. Default grace_period_hours=24 and prune_threshold=0.1. Also check that decay_half_days is set to a reasonable value (90 days default).",
			Category: "high-value",
			Topic:    "incident-steward-no-prune",
		},
	}
}

// apiContractEntries are API design and versioning entries — realistic
// content about API contracts that teams would want to preserve in memory.
func apiContractEntries() []corpusEntry {
	return []corpusEntry{
		{
			Content:  "The /api/store endpoint accepts POST with {content: string, source?: string, metadata?: object}. It runs the full write pipeline synchronously and returns {status: 'ok', summary: string}. The summary describes what was stored, filtered, or deduplicated. A 400 is returned only for malformed JSON; pipeline errors are swallowed and reflected in the summary.",
			Category: "high-value",
			Topic:    "api-store",
		},
		{
			Content:  "The /api/search endpoint accepts POST with {query: string, database?: string}. It embeds the query, runs VectorSearch with the configured topK, formats context, and returns {context: string, scores: []float64}. If no relevant memories exist, context is an empty string (not an error). The database field targets a specific named database in multi-database mode.",
			Category: "high-value",
			Topic:    "api-search",
		},
		{
			Content:  "The /api/memories endpoint is GET-only and returns all stored memories as a JSON array. Optional query param 'q' filters by case-insensitive content substring. The response schema matches the store.Memory struct: {id, content, source, created_at, hit_count, quality_score, content_score, last_retrieved}. Embeddings are not included in list responses to keep payload sizes manageable.",
			Category: "high-value",
			Topic:    "api-memories",
		},
		{
			Content:  "Breaking change in v0.4: the /api/store endpoint previously returned {stored: int, duplicates: int, filtered: int}. It now returns {status: string, summary: string} where summary is human-readable text. Callers that parsed the numeric fields must be updated. The change was made because the numeric fields were misleading — one 'stored' could represent multiple chunks merged into one topic group.",
			Category: "high-value",
			Topic:    "api-breaking-change",
		},
	}
}

// mongodbOpsEntries are operational knowledge about MongoDB — realistic
// domain content that exercises the chunker and retrieval.
func mongodbOpsEntries() []corpusEntry {
	return []corpusEntry{
		{
			Content:  "To create the vector index for Atlas Local, run: docker cp scripts/create_index.js memoryd-mongo:/tmp/create_index.js && docker exec memoryd-mongo mongosh memoryd --quiet --file /tmp/create_index.js. The index specifies numDimensions: 1024 and cosine similarity metric.",
			Category: "mongodb-ops",
			Topic:    "index-setup",
		},
		{
			Content:  "Atlas Local (Docker) requires directConnection=true in the connection string. Without it, the driver tries replica set discovery which fails on the single-node container. Full URI: mongodb://localhost:27017/?directConnection=true",
			Category: "mongodb-ops",
			Topic:    "connection",
		},
		{
			Content:  "The $vectorSearch aggregation stage syntax for Atlas Local: { $vectorSearch: { index: 'vector_index', path: 'embedding', queryVector: [...], numCandidates: 100, limit: 5 } }. numCandidates should be 20x the limit for good recall.",
			Category: "mongodb-ops",
			Topic:    "query-syntax",
		},
		{
			Content:  "MongoDB Atlas proper (not Local) supports pre-filtered vector search using the filter parameter in $vectorSearch. This allows restricting search to memories with quality_score above a threshold or from a specific source. Atlas Local does NOT support this — filters silently produce empty results.",
			Category: "mongodb-ops",
			Topic:    "atlas-vs-local",
		},
		{
			Content:  "The retrieval_events collection stores timestamped records of when each memory appeared in search results. Schema: { memory_id: ObjectId, score: float64, created_at: Date }. This drives the quality learning system — after 50 events, the system starts quality-filtering.",
			Category: "mongodb-ops",
			Topic:    "schema",
		},
		{
			Content:  "Backup strategy: mongodump --archive=/path/backup.gz --gzip --db memoryd. For Atlas Local in Docker: docker exec memoryd-mongo mongodump --archive --gzip --db memoryd > backup.gz. Restore with mongorestore --archive=backup.gz --gzip.",
			Category: "mongodb-ops",
			Topic:    "backup",
		},
		{
			Content:  "When the vector index doesn't exist or is in BUILDING state, $vectorSearch returns an empty result set (no error). This is the most common cause of 'search returns nothing' bug reports. Check index status with: db.memories.aggregate([{$listSearchIndexes:{}}]).",
			Category: "mongodb-ops",
			Topic:    "troubleshooting",
		},
		{
			Content:  "The memories collection schema: { _id: ObjectId, content: string, embedding: [float32 x 1024], source: string, created_at: Date, metadata: object, hit_count: int, quality_score: float64, content_score: float64, last_retrieved: Date }. The score field is transient (set by $vectorSearch, not persisted).",
			Category: "mongodb-ops",
			Topic:    "schema",
		},
	}
}
