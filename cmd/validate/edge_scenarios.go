package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/memory-daemon/memoryd/internal/pipeline"
)

// scenarioEmptyAndWhitespace verifies that empty strings and whitespace-only
// content are safely rejected without panics or spurious storage.
func scenarioEmptyAndWhitespace(ctx context.Context) error {
	st := newMemStore()
	emb := newHashEmbedder(128)
	wp := pipeline.NewWritePipeline(emb, st)

	cases := []struct {
		label   string
		content string
	}{
		{"empty string", ""},
		{"single space", " "},
		{"tabs and newlines", "\t\n\r\n\t"},
		{"only newlines", "\n\n\n\n\n"},
		{"unicode whitespace", "\u00A0\u2003\u3000"},
		{"null-like", "\x00\x00\x00"},
	}

	for _, tc := range cases {
		r := wp.ProcessFiltered(tc.content, "validate-edge", nil)
		if r.Stored > 0 {
			return fmt.Errorf("case %q: empty/whitespace content was stored (stored=%d)", tc.label, r.Stored)
		}
	}

	if st.count() != 0 {
		return fmt.Errorf("expected 0 memories after all empty/whitespace inputs, got %d", st.count())
	}

	if *verbose {
		fmt.Printf("\n    %d empty/whitespace cases → all rejected ✓\n", len(cases))
	}
	return nil
}

// scenarioUnicodeContent verifies that content containing Unicode characters
// (CJK, emoji, RTL, combining characters) is stored without panic or data loss.
func scenarioUnicodeContent(ctx context.Context) error {
	st := newMemStore()
	emb := newHashEmbedder(128)
	wp := pipeline.NewWritePipeline(emb, st)

	cases := []struct {
		label   string
		content string
	}{
		{
			"CJK",
			"モンゴDBのベクトルインデックスは $vectorSearch 集約パイプラインを使用して高次元ベクトルの類似度検索を実行します。インデックスの次元数は1024に設定されています。",
		},
		{
			"Arabic RTL",
			"يستخدم النظام نموذج التضمين المحلي voyage-4-nano عبر llama.cpp لتوليد متجهات ذات أبعاد 1024 لبحث التشابه الدلالي في قاعدة بيانات MongoDB.",
		},
		{
			"Emoji in technical content",
			"The memoryd pipeline ✅ stores technical decisions 🧠 and retrieves them as context 🔍 for future AI sessions. No secrets 🔐 are stored — the redactor strips them before embedding 🚫.",
		},
		{
			"Mixed scripts",
			"Config port: 7432 (default) / पोर्ट / منفذ / ポート — all refer to the memoryd HTTP server listening address for API and proxy traffic.",
		},
		{
			"Combining characters",
			"The café's naïve résumé of the coöperative's approach: 100% retrieval precision über alles.",
		},
	}

	for _, tc := range cases {
		// Must not panic — the key assertion is completion.
		func() {
			defer func() {
				if r := recover(); r != nil {
					panic(fmt.Sprintf("panic on unicode case %q: %v", tc.label, r))
				}
			}()
			wp.ProcessFiltered(tc.content, "validate-unicode", nil)
		}()
	}

	// Some of the above should have been stored (they're long enough, not noise).
	if st.count() == 0 {
		return fmt.Errorf("expected at least some unicode content to be stored, got 0")
	}

	if *verbose {
		fmt.Printf("\n    %d unicode cases → stored=%d (no panics) ✓\n", len(cases), st.count())
	}
	return nil
}

// scenarioVeryLongDocument verifies that processing a very large document
// (well over the 512-token chunk window) does not panic and produces multiple
// stored memories from a single call.
func scenarioVeryLongDocument(ctx context.Context) error {
	st := newMemStore()
	emb := newHashEmbedder(128)
	wp := pipeline.NewWritePipeline(emb, st)

	// Build a ~10KB document by repeating distinct paragraphs with varied content.
	paragraphs := []string{
		"The write pipeline begins by receiving the full AI response text from the proxy layer. It passes this text to the semantic chunker which detects the content structure — distinguishing between prose paragraphs, fenced code blocks, markdown headings and lists, and conversation-style turn-taking. Each detected structure type gets a different splitting strategy: prose splits on sentence and paragraph boundaries, code splits on function and class boundaries, and headings are used as context prefixes for child chunks.",

		"After chunking, each segment is sent to the embedding server (voyage-4-nano via llama.cpp on port 7433) which returns a 1024-dimensional float32 vector. The embedding model was chosen for its balance of retrieval accuracy and local inference speed — Q8_0 quantization achieves 99.2% of full-precision recall while running at roughly 5ms per chunk on M-series hardware. The embedding vectors are L2-normalized before storage so that cosine similarity reduces to a simple dot product.",

		"The deduplication check queries MongoDB with a $vectorSearch operation limited to 1 result. If the top result has a cosine similarity score above 0.92, the new chunk is considered a near-duplicate of an existing memory and discarded. This threshold was determined empirically: at 0.90 too many paraphrases of the same fact accumulated; at 0.95 legitimate updates were blocked. The dedup check adds one database round trip per chunk but eliminates an entire class of retrieval-degrading pollution.",

		"Following dedup, surviving chunks pass through the ContentScorer which uses prototype-based classification. The scorer embeds generic descriptions of high-value content (technical decisions, architecture patterns, debugging solutions) and noise (greetings, filler, acknowledgments). It scores each new chunk by comparing its embedding against both sets and computing the ratio: score = avgQualitySim / (avgQualitySim + avgNoiseSim). This produces a value in (0, 1) without requiring any domain-specific training data.",

		"Topic boundary detection walks the sequence of chunk embeddings and computes cosine similarity between consecutive pairs. When similarity drops below 0.65, a topic boundary is detected and the current group is closed. Groups are also split at the 2048-character boundary (approximately 512 tokens) to respect the embedding model's context window. Groups with more than one chunk are joined with double-newline separators and re-embedded so the stored vector accurately represents the full merged text.",

		"The steward is a background goroutine that wakes on a configurable interval (default 1 hour) and performs three maintenance operations. First, it fetches the oldest BatchSize memories (default 500) and recomputes their quality scores using the exponential decay formula: score = hitNormalized * decay(age, halfLife) where hitNormalized is log2(hitCount+1) divided by the log2 of the maximum hit count in the batch. Second, memories whose score falls below the PruneThreshold (default 0.1) and have passed the grace period (default 24 hours) are permanently deleted. Third, memory pairs with cosine similarity above MergeThreshold (default 0.88) are candidates for merging; the one with fewer hits is deleted.",

		"The MCP server exposes eight tools via JSON-RPC 2.0 over stdio transport. The memory_search tool embeds the query text, runs VectorSearch with topK=5, and formats the results as a context block. The memory_store tool runs the full write pipeline (chunk, embed, dedup, score, store) and returns a summary. The memory_list tool returns all memories with optional keyword filter. The memory_delete tool removes a specific memory by ID. The source_ingest tool initiates a web crawl. The source_list and source_remove tools manage ingested sources. The quality_stats tool reports learning mode status and event counts.",

		"Multi-database support allows a team to have a primary read-write database plus any number of read-only secondary databases from other teams or services. The MultiStore implements Store and HybridSearcher interfaces and fans out reads across all enabled databases using goroutines with a shared results channel. Writes go only to the primary database. Database entries can be added, toggled (enabled/disabled), and removed at runtime via the /api/databases REST endpoints, with changes persisted back to config.yaml.",

		"The source ingestion pipeline crawls documentation sites using BFS traversal starting from a given root URL. Each page's content is hashed with SHA-256 and compared against the stored hash from the previous crawl. Pages whose content hash has not changed since the last crawl are skipped, making re-crawls of large sites take only seconds rather than minutes. Changed pages are re-chunked, re-embedded, and re-stored; stale memories from removed pages are pruned by the steward during its next sweep.",

		"Security redaction uses 15 compiled regex patterns applied sequentially before embedding and storage. The patterns cover: AWS AKIA access keys and secret keys, GitHub personal access tokens (ghp_, gho_, ghs_, ghr_ prefixes), GitHub fine-grained tokens (github_pat_ prefix), Slack tokens (xox prefixes), Stripe live and test keys, PEM-encoded private keys of all standard types, database connection strings with embedded credentials, generic key=value pairs for common secret field names, JWTs (three base64url segments), SSH public keys, and email addresses. The redaction runs before the embedding call so that secret-contaminated vectors are never generated.",
	}

	doc := strings.Join(paragraphs, "\n\n")

	r := wp.ProcessFiltered(doc, "validate-long-doc", nil)

	if r.Stored == 0 {
		return fmt.Errorf("10KB document produced 0 stored memories: %s", r.Summary())
	}
	// A document this long should definitely produce multiple chunks.
	if r.Stored+r.Merged < 3 {
		return fmt.Errorf("expected ≥3 chunks from ~10KB document, got stored=%d merged=%d filtered=%d",
			r.Stored, r.Merged, r.Filtered)
	}

	if *verbose {
		fmt.Printf("\n    %d chars → %s\n", len(doc), r.Summary())
	}
	return nil
}

// scenarioRepeatedIdenticalWrites verifies dedup holds under many identical
// sequential writes. The store should converge to a small number of memories
// regardless of how many times the same content is written.
func scenarioRepeatedIdenticalWrites(ctx context.Context) error {
	st := newMemStore()
	emb := newHashEmbedder(128)
	wp := pipeline.NewWritePipeline(emb, st)

	content := "The memoryd configuration file lives at ~/.memoryd/config.yaml and is created with default values on first run. The port field defaults to 7432, mode defaults to proxy, and mongodb_database defaults to memoryd."

	const writes = 20
	for i := 0; i < writes; i++ {
		wp.ProcessFiltered(content, "validate-repeat", nil)
	}

	// After 20 identical writes, the store should have at most a handful of memories.
	// We allow up to 3 to account for any chunking edge cases.
	if st.count() > 3 {
		return fmt.Errorf("%d identical writes produced %d stored memories (expected ≤3)", writes, st.count())
	}
	if st.count() == 0 {
		return fmt.Errorf("0 memories after %d writes — something went wrong", writes)
	}

	if *verbose {
		fmt.Printf("\n    %d identical writes → %d final stored memories\n", writes, st.count())
	}
	return nil
}

// scenarioPipelineStability verifies the write pipeline survives a variety of
// pathological inputs without panicking: binary-looking content, deeply nested
// markdown, content with only code fences, and maximally nested headings.
func scenarioPipelineStability(ctx context.Context) error {
	st := newMemStore()
	emb := newHashEmbedder(128)
	wp := pipeline.NewWritePipeline(emb, st)

	pathological := []struct {
		label   string
		content string
	}{
		{
			"only code fences",
			"```\n```\n```go\n```\n```python\n```\n```\n```",
		},
		{
			"deeply nested headings",
			"# L1\n## L2\n### L3\n#### L4\n##### L5\n###### L6\n# Another L1\n## Another L2",
		},
		{
			"repeating delimiter chars",
			strings.Repeat("---\n", 50),
		},
		{
			"all table rows",
			"| A | B | C |\n|---|---|---|\n" + strings.Repeat("| x | y | z |\n", 30),
		},
		{
			"mixed scripts and symbols",
			"// コード /* código */ /* коде */ // κώδικας\nfunc() { /* ⚡ */ return nil }",
		},
		{
			"null bytes in content",
			"Technical note: use \x00 caution with \x00 null bytes in log parsers.",
		},
	}

	for _, tc := range pathological {
		func() {
			defer func() {
				if r := recover(); r != nil {
					panic(fmt.Sprintf("panic on case %q: %v", tc.label, r))
				}
			}()
			wp.ProcessFiltered(tc.content, "validate-stability", nil)
		}()
	}

	// Stability test: no panics = pass. Store count is informational.
	if *verbose {
		fmt.Printf("\n    %d pathological inputs → no panics, stored=%d ✓\n", len(pathological), st.count())
	}
	return nil
}
