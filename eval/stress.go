package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

// StressConfig controls the stress test parameters.
type StressConfig struct {
	MemorydURL      string
	TotalMemories   int           // how many memories to generate and store (default 1000)
	BatchSize       int           // memories per batch (default 50)
	Checkpoints     []int         // dataset sizes at which to measure retrieval (default [100,250,500,1000])
	QueryCount      int           // queries per checkpoint (default 20)
	Concurrency     int           // parallel writers (default 4)
}

// StressCheckpoint captures retrieval quality at a given dataset size.
type StressCheckpoint struct {
	MemoryCount     int           `json:"memory_count"`
	StoreLatencyP50 time.Duration `json:"store_latency_p50_ms"`
	StoreLatencyP99 time.Duration `json:"store_latency_p99_ms"`
	QueryLatencyP50 time.Duration `json:"query_latency_p50_ms"`
	QueryLatencyP99 time.Duration `json:"query_latency_p99_ms"`
	AvgResultCount  float64       `json:"avg_result_count"`
	PrecisionAt5    float64       `json:"precision_at_5"`
	EmptyResults    int           `json:"empty_results"`
	DedupRate       float64       `json:"dedup_rate"`
	Timestamp       time.Time     `json:"timestamp"`
}

// StressResult is the full output of a stress run.
type StressResult struct {
	Config      StressConfig      `json:"config"`
	Checkpoints []StressCheckpoint `json:"checkpoints"`
	TotalStored int               `json:"total_stored"`
	TotalDeduped int              `json:"total_deduped"`
	Duration    time.Duration     `json:"duration_ms"`
}

// stressMemory is a generated memory with known topic for precision testing.
type stressMemory struct {
	content string
	topic   string
}

var topics = []string{
	"go-patterns",
	"mongodb-ops",
	"api-design",
	"testing",
	"deployment",
	"security",
	"performance",
	"error-handling",
	"concurrency",
	"configuration",
}

// fragments are composable pieces that get combined to create unique memories.
// Each topic has subject, action, detail, and context fragments that combine
// combinatorially to produce thousands of unique memories from ~40 fragments per topic.
var fragments = map[string]struct{
	subjects []string
	actions  []string
	details  []string
	contexts []string
}{
	"go-patterns": {
		subjects: []string{
			"The UserService struct",
			"Our OrderProcessor handler",
			"The PaymentGateway interface",
			"The CacheManager component",
			"The MetricsCollector module",
			"The ConfigLoader utility",
			"The EventBus implementation",
			"The RateLimiter middleware",
			"The ConnectionPool manager",
			"The TaskScheduler service",
		},
		actions: []string{
			"uses table-driven tests with t.Run() subtests for each validation rule",
			"accepts context.Context as its first parameter and propagates cancellation",
			"wraps errors with fmt.Errorf using the %%w verb for chain inspection",
			"implements a small focused interface defined near the call site",
			"uses struct embedding to delegate logging methods from zerolog.Logger",
			"initializes the DB connection with sync.Once for thread-safe lazy setup",
			"communicates between goroutines via a buffered channel of Event structs",
			"defers resource cleanup inside an anonymous func to handle loop iterations",
			"returns a named error type implementing the error interface for type assertions",
			"uses generics with type constraints to handle multiple payload types",
		},
		details: []string{
			"Each subtest covers: valid input, empty string, unicode, max length, and nil pointer cases.",
			"The context carries request-scoped values like trace ID and user ID through the call stack.",
			"errors.Is checks against ErrNotFound and ErrConflict at the HTTP handler boundary.",
			"The interface has just two methods: Get(ctx, id) and Save(ctx, entity) which is all the caller needs.",
			"This avoids importing the full logging package in every file that needs basic log output.",
			"The sync.Once pattern replaced an init() function that was causing test isolation issues.",
			"The channel buffer size is set to 2x the expected burst rate to avoid blocking producers.",
			"Without the anonymous func wrapper, all deferred Close() calls would use the last loop variable.",
			"The custom error type includes StatusCode() int to map directly to HTTP response codes.",
			"The generic version replaced 4 near-identical functions that only differed in payload type.",
		},
		contexts: []string{
			"This pattern was established during the v2 rewrite in the billing module last quarter.",
			"We discovered this approach when debugging a context leak in the webhook processor.",
			"The team agreed on this convention during the March architecture review meeting.",
			"This came from a production incident where swallowed errors hid a database timeout.",
			"Adopted after the code review feedback on PR #342 about interface pollution.",
			"We switched to this pattern when the race detector flagged the old init-based approach.",
			"This was the fix for the dropped-events bug reported by the monitoring team.",
			"Found this issue during the performance audit when profiling the batch import job.",
			"This matches the project style guide section 4.2 on error type definitions.",
			"Introduced during the migration from Go 1.18 to 1.21 to leverage type parameters.",
		},
	},
	"mongodb-ops": {
		subjects: []string{
			"The memories collection vector index",
			"Our retrieval_events aggregation pipeline",
			"The sessions TTL index on expires_at",
			"The bulk import for source ingestion",
			"The change stream on the memories collection",
			"The connection pool for the write pipeline",
			"The compound index on (source, created_at)",
			"The text search index on content field",
			"The migration script for schema v3",
			"The backup cron job using mongodump",
		},
		actions: []string{
			"uses $vectorSearch with numCandidates set to 20x topK for balanced recall vs speed",
			"runs on Atlas Local inside Docker container mongodb/mongodb-atlas-local:8.0",
			"was created declaratively via createSearchIndex() with cosine similarity metric",
			"uses ordered:false bulk writes so partial failures dont stop the entire batch",
			"sets expireAfterSeconds to 86400 so sessions auto-delete after 24 hours",
			"places $match as the first pipeline stage to reduce docs before $group and $sort",
			"provides at-least-once delivery using resume tokens stored in a checkpoint collection",
			"configured maxPoolSize to 50 after load testing showed the default 100 was excessive",
			"uses a compound index on source+created_at for efficient filtered time-range queries",
			"runs mongodump with --oplog for point-in-time consistency of the full database",
		},
		details: []string{
			"At numCandidates=100 with topK=5, p99 latency stays under 15ms for 50K documents.",
			"The directConnection=true parameter is required for single-node Atlas Local deployments.",
			"The index definition specifies numDimensions: 1024 matching the voyage-4-nano embedding size.",
			"Of the 500 bulk inserts, typically 3-5 fail validation and the rest succeed normally.",
			"The TTL index runs its cleanup pass every 60 seconds so actual deletion has some lag.",
			"Moving $match before $lookup reduced pipeline execution time from 200ms to 12ms.",
			"If the resume token expires before processing resumes, we fall back to a full resync.",
			"Connection pool metrics showed 80%% of connections idle, so we reduced to 50 max.",
			"This index supports the dashboard query that filters memories by source with date sorting.",
			"The oplog capture adds about 15%% to backup size but enables restore to any point in time.",
		},
		contexts: []string{
			"Tuned during the retrieval quality optimization sprint when we benchmarked different values.",
			"Documented in the local dev setup guide under 'Prerequisites > MongoDB'.",
			"The index had to be recreated when we upgraded from 512-dim to 1024-dim embeddings.",
			"Error handling for partial failures was added after a user reported missing memories.",
			"Session expiry was originally 7 days but users rarely needed sessions that old.",
			"Found during a dashboard performance investigation when the sources page was slow.",
			"One operators on-call runbook includes steps for manually advancing the resume token.",
			"The pool size change was part of the resource optimization work for smaller instances.",
			"Added when we implemented the source_list MCP tool that returns memories per source.",
			"Backup schedule runs at 3 AM UTC daily with 7-day retention in the S3 bucket.",
		},
	},
	"api-design": {
		subjects: []string{
			"The /api/memories endpoint",
			"The /api/search route handler",
			"The /api/sources CRUD endpoints",
			"The /api/quality stats endpoint",
			"The /api/store ingestion endpoint",
			"The webhook callback handler",
			"The MCP tool registration API",
			"The health and readiness probes",
			"The dashboard SSE stream endpoint",
			"The rate limiter on /api/store",
		},
		actions: []string{
			"uses URL path versioning at /v1/ for the public-facing memory API",
			"returns 201 with Location header when a new memory is stored successfully",
			"implements cursor-based pagination using the last document _id as cursor",
			"enforces token bucket rate limiting at 60 requests per minute per client",
			"returns structured errors with code field error_type and details array",
			"accepts an Idempotency-Key header to prevent duplicate memory ingestion",
			"sets Access-Control-Allow-Origin to the specific dashboard origin not wildcard",
			"exposes /health for liveness and /ready that checks MongoDB connectivity",
			"streams real-time memory count updates using Server-Sent Events with retry hint",
			"returns 429 with Retry-After header when the per-client rate limit is exceeded",
		},
		details: []string{
			"Version prefix allows routing v1 to the current binary and v2 to the new service independently.",
			"The Location header format is /api/memories/{id} and response body includes the full Memory object.",
			"Cursor pagination handles deletions gracefully since it doesn't depend on a count offset.",
			"The token bucket refills at 1 request per second with burst capacity of 60 tokens.",
			"Each error includes: type (e.g. validation_error), message (human text), and fields (per-field details).",
			"Idempotency keys expire after 24 hours and are stored in a lightweight in-memory LRU cache.",
			"CORS is configured with credentials: true which requires explicit origin, not wildcard.",
			"The readiness probe queries MongoDB with a 2-second timeout before returning healthy.",
			"SSE events include id: field with a counter so clients can resume from the last seen event.",
			"Retry-After value is computed from the token bucket state: seconds until next token available.",
		},
		contexts: []string{
			"Versioning was added proactively before any breaking changes had been made to the API.",
			"The 201+Location pattern aligns with the HTTP spec for resource creation endpoints.",
			"Switched from offset pagination after users reported duplicate results on the second page.",
			"Rate limit of 60/min was set based on Claude Desktop sending about 30 requests in a busy minute.",
			"Error format was standardized across all endpoints in the API consistency review.",
			"Idempotency was critical after users reported occasional double-stored memories over flaky WiFi.",
			"CORS config was tightened after a security review flagged the wildcard as a risk for credential-bearing requests.",
			"Readiness probe was added when Kubernetes rolled out pods before MongoDB connection was established.",
			"SSE replaced polling for the dashboard, reducing server load from 2 req/sec/client to near zero.",
			"Rate limiting was implemented during the MCP integration when Claude could fire rapid bursts.",
		},
	},
	"testing": {
		subjects: []string{
			"The WritePipeline integration test",
			"The vector search accuracy benchmark",
			"The MCP tool end-to-end test suite",
			"The deduplication threshold fuzz test",
			"The API handler HTTP test helpers",
			"The embedding model golden file tests",
			"The redaction pipeline property tests",
			"The store package mock interface",
			"The crawler link extraction tests",
			"The quality tracker unit tests",
		},
		actions: []string{
			"spins up a real MongoDB Atlas Local container using testcontainers-go",
			"defines properties like cosine(embed(text), embed(text)) must equal 1.0 within epsilon",
			"uses testdata/golden/ directory with -update flag for snapshot comparisons",
			"fuzzes the input text to find panics in the chunker and embedding pipeline",
			"provides httptest.NewServer wrappers that return pre-configured test fixtures",
			"compares embedding output against golden vectors stored in testdata/embeddings/",
			"generates random PII strings and verifies the redactor masks them correctly",
			"defines a MockStore interface with just Get, Save, Search and Delete methods",
			"parses HTML fixtures from testdata/ and validates extracted links against expected",
			"isolates each test with t.Cleanup() that resets the quality score counters",
		},
		details: []string{
			"The container starts in about 3 seconds and is shared across all tests in the package via TestMain.",
			"Epsilon of 0.0001 accounts for float32 rounding in the GGUF quantized model output.",
			"Golden files are committed to git and CI runs with -update=false to catch regressions.",
			"The fuzzer found a panic when input contained only whitespace followed by a null byte.",
			"Test helpers return both the server URL and a cleanup func to avoid resource leaks.",
			"Golden vectors are regenerated when the embedding model version changes using a Make target.",
			"Rapid-check generates email addresses, SSNs, credit card numbers, and phone number formats.",
			"The mock records all method calls for assertion and returns configurable responses per test.",
			"HTML fixtures cover: relative links, absolute links, fragment-only links, and javascript: URIs.",
			"t.Cleanup is preferred over defer because it runs even when the test calls t.Fatal early.",
		},
		contexts: []string{
			"Integration tests replaced the old mock-heavy approach after bugs slipped through mock boundaries.",
			"The property test caught a precision issue that only manifested with certain CJK character inputs.",
			"Golden file testing was adopted from the Go standard library testing patterns.",
			"Fuzz testing was added after the chunker panic brought down the daemon in production.",
			"HTTP test helpers were extracted after 6 different test files duplicated the same setup code.",
			"Golden vectors need updating roughly twice a year when we upgrade the embedding model.",
			"Property tests for redaction were required as part of the security compliance review.",
			"The MockStore interface matches the real Store interface using Go interface embedding.",
			"Crawler tests were written alongside the source ingestion feature for web documentation.",
			"Quality tracker tests run in parallel since each creates its own isolated state.",
		},
	},
	"deployment": {
		subjects: []string{
			"The memoryd Docker image",
			"The systemd unit file for memoryd",
			"The Kubernetes Deployment manifest",
			"The GitHub Actions release workflow",
			"The goreleaser cross-compilation config",
			"The install.sh bootstrap script",
			"The MongoDB Atlas Local sidecar container",
			"The log shipper for structured JSON output",
			"The database migration init container",
			"The feature flag config for beta features",
		},
		actions: []string{
			"uses a multi-stage build with Go 1.22 builder and gcr.io/distroless/static runtime",
			"traps SIGTERM to run graceful shutdown draining in-flight requests within 10 seconds",
			"sets readinessProbe initialDelaySeconds to 5 based on measured cold start time",
			"overrides config via environment variables with MEMORYD_ prefix following 12-factor",
			"outputs structured JSON logs with zerolog including request_id and trace_id fields",
			"applies versioned migration files (001_create_memories.js) in an init container",
			"evaluates feature flags per-request from the config file not at process startup",
			"stores secrets as references to environment variables not hardcoded in config files",
			"builds darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 with CGO_ENABLED=0",
			"downloads the correct binary archive for the detected OS and architecture",
		},
		details: []string{
			"The distroless image brings the total image size from 800MB (golang:1.22) to 12MB.",
			"The 10-second drain timeout matches the Kubernetes terminationGracePeriodSeconds setting.",
			"Cold start takes 2.3 seconds for model loading plus 0.5 seconds for MongoDB connection.",
			"MEMORYD_PORT=7432 and MEMORYD_MONGODB_URI override their config.yaml equivalents.",
			"Every log line includes: timestamp, level, message, request_id, latency_ms, and status.",
			"Migrations are idempotent: each checks if the index/collection already exists before creating.",
			"Flag values are read from a ConfigMap that can be updated without restarting any pods.",
			"Runtime resolves MEMORYD_ANTHROPIC_KEY from the environment at startup not from the file.",
			"Cross-compilation works because memoryd has zero CGo dependencies in the core binary.",
			"The install script detects arm64 vs x86_64 using uname -m and maps to the archive suffix.",
		},
		contexts: []string{
			"Image size reduction was a priority for edge deployment on customer infrastructure.",
			"Graceful shutdown was implemented after dropped requests during rolling deploys.",
			"Readiness delay was measured by adding timing logs to the startup sequence.",
			"Environment variable config follows the same pattern used by the rest of our services.",
			"Structured logging was required for integration with our centralized logging pipeline.",
			"Migration init container was chosen over startup migration after a deploy race condition.",
			"Feature flags were needed for rolling out the new quality scoring system gradually.",
			"Secret handling follows the security teams Vault integration guidelines.",
			"Cross-compilation was blocked until we removed the systray dependency from the core binary.",
			"The install script was written to support the onboarding guide for new developers.",
		},
	},
	"security": {
		subjects: []string{
			"The API input validation middleware",
			"The JWT authentication handler",
			"The SQL parameterization layer",
			"The TLS configuration for HTTPS",
			"The gitleaks pre-commit hook",
			"The auth endpoint rate limiter",
			"The Content-Security-Policy header",
			"The dependency vulnerability scanner",
			"The PII redaction pipeline",
			"The CORS configuration module",
		},
		actions: []string{
			"validates all request body fields against an allowlist schema before processing",
			"issues short-lived access tokens (15 min) with long-lived refresh tokens (7 days)",
			"uses parameterized queries exclusively and rejects any raw string concatenation in review",
			"enforces TLS 1.3 minimum with HSTS header max-age set to one year including subdomains",
			"runs gitleaks in the pre-commit hook and trufflehog in CI to catch leaked credentials",
			"applies stricter rate limits of 5 attempts per minute on login and token refresh endpoints",
			"sets script-src to self only and blocks inline scripts with a nonce-based CSP policy",
			"runs govulncheck in CI and blocks PRs with known vulnerabilities in direct dependencies",
			"detects and masks email addresses SSNs credit card numbers and API keys before storage",
			"configures CORS with explicit origin allowlist and credentials:true for cookie-based auth",
		},
		details: []string{
			"The allowlist rejects unknown fields and enforces max lengths to prevent oversized payloads.",
			"Refresh tokens are stored hashed in the database and can be individually revoked.",
			"A lint rule flags any usage of fmt.Sprintf in SQL query construction as a critical error.",
			"HSTS preload submission ensures browsers always connect via HTTPS even on first visit.",
			"Gitleaks patterns cover AWS keys, GitHub tokens, JWT secrets, and database connection strings.",
			"After 5 failed login attempts, the account is locked for 15 minutes with exponential backoff.",
			"CSP nonces are generated per-request using crypto/rand and injected into script tags by the template.",
			"Govulncheck is preferred over nancy because it only flags vulnerabilities in actually-called code paths.",
			"The redaction pipeline uses regex patterns with post-validation to reduce false positives on numbers.",
			"CORS preflight responses are cached for 1 hour (Access-Control-Max-Age: 3600) to reduce OPTIONS requests.",
		},
		contexts: []string{
			"Input validation was tightened after a pentester submitted a 50MB request body causing OOM.",
			"Token lifetimes were shortened from 1 hour to 15 minutes after the security audit recommendation.",
			"Parameterized queries were enforced project-wide following the OWASP SQL injection finding.",
			"TLS 1.3 was mandated when we dropped support for clients older than 2018.",
			"Gitleaks caught a committed AWS key during onboarding of a new developer last quarter.",
			"Login rate limiting was added after detecting credential stuffing attempts in access logs.",
			"CSP was tightened from unsafe-inline to nonce-based after the XSS vulnerability report.",
			"Govulncheck replaced our previous scanner that had too many false positives for indirect deps.",
			"PII redaction was implemented as a compliance requirement for GDPR data minimization.",
			"CORS was reconfigured when the dashboard moved from same-origin to a separate subdomain.",
		},
	},
	"performance": {
		subjects: []string{
			"The embedding generation pipeline",
			"The MongoDB connection pool",
			"The in-memory LRU cache for hot queries",
			"The batch insert operation",
			"The vector search query path",
			"The response compression middleware",
			"The sync.Pool for embedding buffers",
			"The composite index on memories collection",
			"The HTTP client for external API calls",
			"The chunker splitting large documents",
		},
		actions: []string{
			"was profiled with pprof showing 73%% of CPU time spent in embedding matrix multiplication",
			"reuses connections instead of creating new TCP+TLS handshakes for each query",
			"caches the top 1000 most frequent search queries with a 5-minute TTL",
			"combines 50 individual inserts into a single bulk write reducing round trips by 49x",
			"avoids N+1 by fetching all candidate vectors in a single $vectorSearch aggregation",
			"enables gzip compression for API responses over 1KB saving 60-80%% bandwidth on JSON",
			"uses sync.Pool to reuse float32 slice buffers eliminating 1024-element allocations per embed",
			"has a compound index on (source, quality_score, created_at) matching the dashboard query pattern",
			"keeps a persistent HTTP client with connection pooling and 30-second idle timeout",
			"pre-allocates the output slice with make([]Chunk, 0, estimatedCount) to avoid growing",
		},
		details: []string{
			"Profiling showed alloc_space dominated by 1024xfloat32 vectors at 4KB each during high throughput.",
			"Connection reuse reduced p99 latency from 45ms to 8ms by eliminating the TLS handshake overhead.",
			"Cache hit rate stabilized at 34%% which still saved significant embedding computation for repeated lookups.",
			"Bulk write throughput: 50 individual inserts took 2.3s, single bulk write took 47ms.",
			"The single aggregation returns scored results directly, avoiding separate score computation queries.",
			"Gzip at compression level 6 adds 0.3ms to a 10KB response but saves 7KB of network transfer.",
			"sync.Pool reduced GC pause time from 4.2ms to 0.8ms under sustained 100 req/sec load.",
			"The compound index eliminated a COLLSCAN that was causing 200ms queries on the sources dashboard.",
			"Idle connection timeout of 30s balances between reuse benefit and resource consumption.",
			"Pre-allocation saved 3 realloc+copy cycles for a typical 2000-word document split into 12 chunks.",
		},
		contexts: []string{
			"Performance investigation started when users reported the daemon using 100%% CPU during ingest.",
			"Connection pooling fix was the single biggest latency improvement in the v2 release.",
			"Cache was added after profiling showed the same 50 queries accounted for a third of all searches.",
			"Bulk write optimization was needed for the source ingestion pipeline processing 500+ chunks.",
			"The single-aggregation approach was documented in the MongoDB Atlas vector search best practices guide.",
			"Compression was enabled after monitoring showed 2GB/day of JSON API traffic from the dashboard.",
			"sync.Pool optimization was part of the memory usage investigation for running on 512MB VMs.",
			"Index creation was prompted by the slow query log showing full collection scans on every dashboard load.",
			"HTTP client pooling was found during a goroutine leak investigation showing 3000+ idle connections.",
			"Chunk pre-allocation reduced the memory high-water mark for large document ingestion by 40%%.",
		},
	},
	"error-handling": {
		subjects: []string{
			"The store.ErrNotFound sentinel error",
			"The memory ingestion error wrapper",
			"The HTTP handler panic recovery middleware",
			"The MongoDB retry-with-backoff helper",
			"The embedding service circuit breaker",
			"The API error classifier middleware",
			"The MCP tool error response builder",
			"The context timeout propagation chain",
			"The webhook delivery retry queue",
			"The batch import partial failure handler",
		},
		actions: []string{
			"is checked with errors.Is() at the HTTP handler to return 404 instead of 500",
			"wraps each pipeline stage error with fmt.Errorf adding the stage name and chunk index",
			"uses defer/recover at the goroutine boundary to log the stack trace and return 500",
			"retries with exponential backoff starting at 100ms with 3 max attempts and jitter",
			"opens after 5 consecutive failures and half-opens after a 30-second cooldown period",
			"maps internal errors to HTTP status codes: ErrNotFound->404 ErrConflict->409 ErrValidation->422",
			"returns JSON-RPC error responses with code and message matching the MCP specification",
			"sets a 5-second timeout on the context that leaves room for 2 retry attempts within the budget",
			"queues failed webhook deliveries for retry with exponential backoff up to 1 hour max delay",
			"collects all chunk-level errors and returns a summary with counts of stored vs failed vs skipped",
		},
		details: []string{
			"Without the errors.Is check, a missing memory returned 500 which confused the MCP client.",
			"The wrapped error reads like: 'ingest stage embed chunk 3: model inference: context deadline exceeded'.",
			"Panic recovery logs the goroutine ID and full stack trace to structured JSON for post-mortem analysis.",
			"Jitter adds random 0-50ms to prevent thundering herd when MongoDB recovers from a failover.",
			"The circuit breaker tracks failure rate over a sliding 60-second window not just consecutive failures.",
			"The error classifier runs before the response writer so it can set both status code and error body.",
			"MCP spec requires error code -32603 for internal errors and -32602 for invalid params.",
			"The 5-second outer timeout with 1.5-second per-attempt timeout allows exactly 2 retries.",
			"Webhook retry delays follow 1m, 5m, 15m, 1h progression before marking delivery as permanently failed.",
			"The partial failure summary includes per-chunk error details so callers can retry only failed chunks.",
		},
		contexts: []string{
			"The sentinel error pattern was adopted project-wide after inconsistent 500 errors for missing resources.",
			"Wrapping was added during the debugging session where a bare 'context canceled' gave no clue which stage failed.",
			"Panic recovery was mandatory after an unrecovered panic in a goroutine took down the entire daemon.",
			"Backoff parameters were tuned during MongoDB rolling restart testing to avoid unnecessary retries.",
			"Circuit breaker was added after the embedding model download hung indefinitely blocking all writes.",
			"Error classification replaced per-handler switch statements with a single middleware for consistency.",
			"MCP error codes were verified against the specification using the protocol compliance test suite.",
			"Timeout budget was designed by working backward from the 10-second HTTP handler timeout.",
			"Webhook retry queue was implemented when customers reported missing notifications during outages.",
			"Partial failure reporting replaced the all-or-nothing behavior that dropped valid chunks on any error.",
		},
	},
	"concurrency": {
		subjects: []string{
			"The memory write worker pool",
			"The embedding batch processor goroutine",
			"The search request fan-out handler",
			"The source ingestion background task",
			"The quality score updater goroutine",
			"The dashboard SSE broadcaster",
			"The MCP request multiplexer",
			"The crawler goroutine pool",
			"The metrics collection goroutine",
			"The graceful shutdown coordinator",
		},
		actions: []string{
			"uses 4 fixed goroutines reading from a buffered channel of WriteRequest structs",
			"processes embedding requests in batches of 32 collected over a 50ms window",
			"fans out search queries to 3 index shards using errgroup and returns merged results",
			"runs as a background goroutine that accepts work via channel and stops on context cancel",
			"uses select with a ticker channel to flush accumulated scores every 10 seconds",
			"maintains a sync.Map of subscriber channels and broadcasts events with non-blocking sends",
			"uses sync.RWMutex to allow concurrent reads of the tool registry with exclusive write locks",
			"limits concurrent HTTP fetches to 10 using a semaphore channel",
			"uses atomic.Int64 counters for request count bytes processed and active goroutines",
			"cancels the root context then waits on a WaitGroup with a 10-second deadline",
		},
		details: []string{
			"The buffered channel of size 100 absorbs write bursts without blocking the HTTP handler.",
			"Batching 32 embeddings at once reduces per-item overhead from 12ms to 0.8ms amortized.",
			"errgroup cancels remaining shard queries on first error to avoid wasting resources on doomed searches.",
			"The background goroutine logs its lifecycle: started, items processed count, and clean shutdown confirmation.",
			"select also listens on ctx.Done() so the ticker loop exits immediately when the daemon shuts down.",
			"Non-blocking send (select with default) drops events for slow subscribers rather than blocking the broadcaster.",
			"RWMutex allows 50+ concurrent MCP tool lookups while tool registration happens only at startup.",
			"The semaphore prevents exhausting file descriptors when crawling sites with thousands of pages.",
			"Atomic counters avoid mutex contention on the hot path where every request increments multiple counters.",
			"The WaitGroup deadline handles hung goroutines: after 10 seconds, exit anyway and log which ones leaked.",
		},
		contexts: []string{
			"Worker pool replaced an unbounded goroutine-per-request model that caused OOM under load.",
			"Batch processing was the key optimization that made real-time embedding feasible on CPU-only hardware.",
			"Fan-out search was implemented when we sharded the vector index across 3 MongoDB collections.",
			"Background ingestion replaced synchronous processing that blocked the API for 30+ seconds on large documents.",
			"Score flushing was batched to reduce MongoDB write load from per-retrieval to every 10 seconds.",
			"SSE broadcasting was refactored from per-client polling goroutines to a single broadcaster pattern.",
			"RWMutex replaced a regular Mutex after profiling showed tool lookups dominating the lock wait time.",
			"Crawler concurrency was reduced from 50 to 10 after the target sites started rate-limiting us.",
			"Atomic counters replaced a mutex-guarded struct after benchmarks showed 40%% improvement on the hot path.",
			"Shutdown WaitGroup timeout was added after a stuck goroutine prevented clean restarts in production.",
		},
	},
	"configuration": {
		subjects: []string{
			"The MEMORYD_PORT environment variable",
			"The config.yaml defaults for local development",
			"The hot-reload watcher on config.yaml",
			"The retrieval_top_k feature flag",
			"The embedding_dim config validation",
			"The config precedence chain",
			"The secret reference for ANTHROPIC_KEY",
			"The multi-environment config overlay",
			"The model_path config with auto-download",
			"The mongodb_atlas_uri connection string config",
		},
		actions: []string{
			"overrides the config file port setting following the convention of env vars taking precedence",
			"provides working defaults for all fields so a fresh clone can start immediately with zero config",
			"watches config.yaml with fsnotify and atomically swaps the active config on change",
			"can be changed from 5 to 10 via the config file without restarting the daemon process",
			"fails fast at startup if embedding_dim is not 512 or 1024 logging the invalid value clearly",
			"resolves CLI flags first then environment variables then config file then compiled defaults",
			"references the env var name not the actual key so the config file can be safely committed",
			"uses a base config.yaml with environment-specific overrides in config.production.yaml",
			"checks if the model file exists at the configured path and downloads it if missing on first start",
			"accepts either a full connection string or shorthand localhost that expands to the default URI",
		},
		details: []string{
			"The MEMORYD_ prefix namespace prevents collisions with other tools in the same environment.",
			"Default config sets port 7432 mongodb_atlas_uri localhost:27017 embedding_dim 1024 and top_k 5.",
			"Atomic swap uses a sync.RWMutex so in-flight requests see either old or new config never partial.",
			"Changing top_k takes effect on the next search request with no gap in service availability.",
			"Validation also checks that model_path is readable and mongodb_atlas_uri is a valid connection string.",
			"The precedence chain is implemented as a simple waterfall: each layer fills only unset fields.",
			"In production the ANTHROPIC_KEY is injected via Kubernetes secret mounted as an env var.",
			"Production overlay only overrides model_path log_level and mongodb_atlas_uri leaving all else as base.",
			"Model auto-download uses a sha256 checksum file to verify integrity after download.",
			"The localhost shorthand expands to mongodb://localhost:27017/?directConnection=true automatically.",
		},
		contexts: []string{
			"Port configurability was needed when users ran memoryd alongside other services on 7432.",
			"Zero-config defaults significantly reduced the number of setup issues reported during onboarding.",
			"Hot reload was implemented for the quality tuning phase so we could adjust thresholds live.",
			"Feature flags allowed A/B testing different retrieval depths without separate deployments.",
			"Startup validation was added after a user spent hours debugging with embedding_dim set to 128.",
			"The precedence chain matches the de facto Go convention used by viper and similar libraries.",
			"Secret handling follows the 12-factor app methodology for configuration management.",
			"Multi-env config replaced per-environment complete config files that drifted out of sync.",
			"Auto-download was the most requested feature from the install experience survey.",
			"Connection string shorthand was added after localhost:27017 was the source of 60%% of config issues.",
		},
	},
}

// queries maps topics to realistic search queries.
var queries = map[string][]string{
	"go-patterns":    {"how to write table driven tests in go", "context propagation patterns", "error wrapping best practices", "interface design in go"},
	"mongodb-ops":    {"mongodb vector search configuration", "atlas local docker setup", "mongodb bulk write operations", "aggregation pipeline optimization"},
	"api-design":     {"rest api versioning strategy", "pagination approaches for apis", "rate limiting implementation", "api error response format"},
	"testing":        {"integration testing with containers", "go fuzz testing", "benchmark tests in go", "test isolation patterns"},
	"deployment":     {"docker multi stage build", "graceful shutdown go", "kubernetes readiness probe", "structured logging best practices"},
	"security":       {"api authentication jwt", "sql injection prevention", "secret scanning in ci", "content security policy"},
	"performance":    {"go profiling with pprof", "database connection pooling", "caching strategy layers", "n+1 query prevention"},
	"error-handling": {"sentinel errors in go", "retry with exponential backoff", "circuit breaker pattern", "timeout propagation context"},
	"concurrency":    {"goroutine lifecycle management", "worker pool pattern go", "sync waitgroup usage", "errgroup fan out pattern"},
	"configuration":  {"config validation at startup", "hot reload config file", "feature flags configuration", "secret rotation strategy"},
}

// StressRunner executes stress tests against memoryd.
type StressRunner struct {
	cfg    StressConfig
	client *http.Client
	rng    *rand.Rand
}

func NewStressRunner(cfg StressConfig) *StressRunner {
	if cfg.MemorydURL == "" {
		cfg.MemorydURL = "http://127.0.0.1:7432"
	}
	if cfg.TotalMemories == 0 {
		cfg.TotalMemories = 1000
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 50
	}
	if len(cfg.Checkpoints) == 0 {
		cfg.Checkpoints = []int{100, 250, 500, 1000}
	}
	if cfg.QueryCount == 0 {
		cfg.QueryCount = 20
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = 4
	}
	return &StressRunner{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (s *StressRunner) Run(ctx context.Context, w io.Writer) (*StressResult, error) {
	start := time.Now()

	fmt.Fprintf(w, "\n=== memoryd stress test ===\n")
	fmt.Fprintf(w, "  target:      %s\n", s.cfg.MemorydURL)
	fmt.Fprintf(w, "  memories:    %d\n", s.cfg.TotalMemories)
	fmt.Fprintf(w, "  concurrency: %d\n", s.cfg.Concurrency)
	fmt.Fprintf(w, "  checkpoints: %v\n\n", s.cfg.Checkpoints)

	// Generate all memories upfront.
	memories := s.generateMemories(s.cfg.TotalMemories)

	result := &StressResult{
		Config: s.cfg,
	}

	totalAttempted := 0
	stored := 0
	deduped := 0
	nextCheckpoint := 0
	var storeLatencies []time.Duration

	// Insert memories in batches. Track total attempted so the loop always terminates.
	for totalAttempted < len(memories) {
		// Check if we've hit a checkpoint (based on stored count).
		if nextCheckpoint < len(s.cfg.Checkpoints) && stored >= s.cfg.Checkpoints[nextCheckpoint] {
			fmt.Fprintf(w, "  [checkpoint] %d memories stored — measuring retrieval ...\n", stored)
			cp, err := s.measureCheckpoint(ctx, stored, storeLatencies)
			if err != nil {
				return nil, fmt.Errorf("checkpoint at %d: %w", stored, err)
			}
			if stored+deduped > 0 {
				cp.DedupRate = float64(deduped) / float64(stored+deduped)
			}
			result.Checkpoints = append(result.Checkpoints, *cp)
			s.printCheckpoint(w, cp)
			nextCheckpoint++
		}

		// Insert next batch.
		end := totalAttempted + s.cfg.BatchSize
		if end > len(memories) {
			end = len(memories)
		}
		batch := memories[totalAttempted:end]

		batchStored, batchDeduped, latencies, err := s.insertBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("insert batch at %d: %w", totalAttempted, err)
		}

		storeLatencies = append(storeLatencies, latencies...)
		stored += batchStored
		deduped += batchDeduped
		totalAttempted += len(batch)

		fmt.Fprintf(w, "  [insert] stored=%d deduped=%d attempted=%d/%d\n",
			stored, deduped, totalAttempted, len(memories))
	}

	// Final checkpoint.
	fmt.Fprintf(w, "  [checkpoint] %d memories stored — final measurement ...\n", stored)
	cp, err := s.measureCheckpoint(ctx, stored, storeLatencies)
	if err != nil {
		return nil, fmt.Errorf("final checkpoint: %w", err)
	}
	if stored+deduped > 0 {
		cp.DedupRate = float64(deduped) / float64(stored+deduped)
	}
	result.Checkpoints = append(result.Checkpoints, *cp)
	s.printCheckpoint(w, cp)

	result.TotalStored = stored
	result.TotalDeduped = deduped
	result.Duration = time.Since(start)

	dedupPct := 0.0
	if stored+deduped > 0 {
		dedupPct = float64(deduped) / float64(stored+deduped) * 100
	}
	fmt.Fprintf(w, "\n=== done in %s ===\n", result.Duration.Round(time.Millisecond))
	fmt.Fprintf(w, "  attempted: %d\n", totalAttempted)
	fmt.Fprintf(w, "  stored:    %d\n", stored)
	fmt.Fprintf(w, "  deduped:   %d (%.1f%%)\n\n", deduped, dedupPct)

	return result, nil
}

// generateMemories produces n unique memories by combining fragments combinatorially.
// With 10 topics x 10 subjects x 10 actions x 10 details x 10 contexts = 100K combos,
// each generated memory is structurally unique and should beat the 0.92 dedup threshold.
func (s *StressRunner) generateMemories(n int) []stressMemory {
	var out []stressMemory
	topicList := make([]string, 0, len(topics))
	topicList = append(topicList, topics...)

	for i := 0; i < n; i++ {
		topic := topicList[i%len(topicList)]
		f := fragments[topic]

		subj := f.subjects[s.rng.Intn(len(f.subjects))]
		act := f.actions[s.rng.Intn(len(f.actions))]
		det := f.details[s.rng.Intn(len(f.details))]
		ctx := f.contexts[s.rng.Intn(len(f.contexts))]

		content := fmt.Sprintf("%s %s. %s %s", subj, act, det, ctx)
		out = append(out, stressMemory{
			content: content,
			topic:   topic,
		})
	}

	// Shuffle to interleave topics.
	s.rng.Shuffle(len(out), func(i, j int) {
		out[i], out[j] = out[j], out[i]
	})
	return out
}

func (s *StressRunner) insertBatch(ctx context.Context, batch []stressMemory) (stored, deduped int, latencies []time.Duration, err error) {
	type result struct {
		stored  int
		deduped int
		latency time.Duration
		err     error
	}

	results := make(chan result, len(batch))
	sem := make(chan struct{}, s.cfg.Concurrency)

	var wg sync.WaitGroup
	for _, m := range batch {
		wg.Add(1)
		go func(mem stressMemory) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			body, _ := json.Marshal(map[string]string{
				"content": mem.content,
				"source":  "eval-stress-" + mem.topic,
			})
			req, _ := http.NewRequestWithContext(ctx, "POST", s.cfg.MemorydURL+"/api/store", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			start := time.Now()
			resp, e := s.client.Do(req)
			lat := time.Since(start)

			if e != nil {
				results <- result{err: e}
				return
			}
			defer resp.Body.Close()

			var res struct {
				Summary string `json:"summary"`
			}
			json.NewDecoder(resp.Body).Decode(&res)

			r := result{latency: lat}
			if strings.Contains(res.Summary, "skipped") {
				r.deduped = 1
			} else {
				r.stored = 1
			}
			results <- r
		}(m)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		if r.err != nil {
			return stored, deduped, latencies, r.err
		}
		stored += r.stored
		deduped += r.deduped
		latencies = append(latencies, r.latency)
	}
	return stored, deduped, latencies, nil
}

func (s *StressRunner) measureCheckpoint(ctx context.Context, memCount int, storeLatencies []time.Duration) (*StressCheckpoint, error) {
	cp := &StressCheckpoint{
		MemoryCount: memCount,
		Timestamp:   time.Now(),
	}

	// Store latency percentiles.
	if len(storeLatencies) > 0 {
		cp.StoreLatencyP50 = percentile(storeLatencies, 50)
		cp.StoreLatencyP99 = percentile(storeLatencies, 99)
	}

	// Run queries across all topics.
	var queryLatencies []time.Duration
	var totalResults int
	var emptyCount int
	var topicHits int
	var totalQueries int

	allQueries := s.selectQueries(s.cfg.QueryCount)
	for _, q := range allQueries {
		start := time.Now()
		body, _ := json.Marshal(map[string]string{"query": q.query})
		req, _ := http.NewRequestWithContext(ctx, "POST", s.cfg.MemorydURL+"/api/search", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.client.Do(req)
		lat := time.Since(start)
		if err != nil {
			return nil, err
		}

		var searchResult struct {
			Context string `json:"context"`
		}
		json.NewDecoder(resp.Body).Decode(&searchResult)
		resp.Body.Close()

		queryLatencies = append(queryLatencies, lat)
		totalQueries++

		if searchResult.Context == "" {
			emptyCount++
		} else {
			// Count results (separated by "---" or newlines with "score:").
			parts := strings.Split(searchResult.Context, "---")
			resultCount := 0
			for _, p := range parts {
				if strings.TrimSpace(p) != "" {
					resultCount++
				}
			}
			totalResults += resultCount

			// Check if results are topically relevant.
			lower := strings.ToLower(searchResult.Context)
			if s.topicRelevant(lower, q.topic) {
				topicHits++
			}
		}
	}

	if len(queryLatencies) > 0 {
		cp.QueryLatencyP50 = percentile(queryLatencies, 50)
		cp.QueryLatencyP99 = percentile(queryLatencies, 99)
	}
	if totalQueries > 0 {
		cp.AvgResultCount = float64(totalResults) / float64(totalQueries)
		cp.PrecisionAt5 = float64(topicHits) / float64(totalQueries)
	}
	cp.EmptyResults = emptyCount

	return cp, nil
}

type taggedQuery struct {
	query string
	topic string
}

func (s *StressRunner) selectQueries(n int) []taggedQuery {
	var all []taggedQuery
	for topic, qs := range queries {
		for _, q := range qs {
			all = append(all, taggedQuery{query: q, topic: topic})
		}
	}
	s.rng.Shuffle(len(all), func(i, j int) {
		all[i], all[j] = all[j], all[i]
	})
	if n < len(all) {
		all = all[:n]
	}
	return all
}

func (s *StressRunner) topicRelevant(text, topic string) bool {
	keywords := map[string][]string{
		"go-patterns":    {"goroutine", "interface", "context.context", "t.run", "error wrap", "defer", "channel", "struct embed"},
		"mongodb-ops":    {"mongo", "atlas", "vectorsearch", "aggregat", "collection", "bulk write", "index", "pipeline"},
		"api-design":     {"endpoint", "rest", "pagination", "rate limit", "http status", "cors", "health check", "idempoten"},
		"testing":        {"test", "benchmark", "fuzz", "fixture", "assert", "mock", "golden", "snapshot"},
		"deployment":     {"docker", "kubernetes", "deploy", "shutdown", "container", "distroless", "migration", "feature flag"},
		"security":       {"auth", "jwt", "injection", "tls", "secret", "csp", "redact", "vulnerability", "cors"},
		"performance":    {"pprof", "cache", "pool", "allocat", "latenc", "bulk", "compress", "index"},
		"error-handling": {"error", "retry", "circuit", "timeout", "panic", "backoff", "sentinel", "recover"},
		"concurrency":    {"goroutine", "channel", "mutex", "waitgroup", "atomic", "worker", "semaphore", "errgroup"},
		"configuration":  {"config", "environment", "flag", "reload", "default", "precedence", "secret ref", "validation"},
	}
	for _, kw := range keywords[topic] {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func (s *StressRunner) printCheckpoint(w io.Writer, cp *StressCheckpoint) {
	fmt.Fprintf(w, "    memories:     %d\n", cp.MemoryCount)
	fmt.Fprintf(w, "    store p50/99: %s / %s\n", cp.StoreLatencyP50.Round(time.Millisecond), cp.StoreLatencyP99.Round(time.Millisecond))
	fmt.Fprintf(w, "    query p50/99: %s / %s\n", cp.QueryLatencyP50.Round(time.Millisecond), cp.QueryLatencyP99.Round(time.Millisecond))
	fmt.Fprintf(w, "    avg results:  %.1f\n", cp.AvgResultCount)
	fmt.Fprintf(w, "    precision@5:  %.0f%%\n", cp.PrecisionAt5*100)
	fmt.Fprintf(w, "    empty:        %d\n", cp.EmptyResults)
	fmt.Fprintf(w, "    dedup rate:   %.1f%%\n\n", cp.DedupRate*100)
}

func percentile(latencies []time.Duration, pct int) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	for i := 1; i < len(sorted); i++ {
		key := sorted[i]
		j := i - 1
		for j >= 0 && sorted[j] > key {
			sorted[j+1] = sorted[j]
			j--
		}
		sorted[j+1] = key
	}
	idx := int(math.Ceil(float64(pct)/100.0*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// StressReportJSON writes stress results as JSON.
func StressReportJSON(w io.Writer, result *StressResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}
