package store

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// PostgresStore implements Store, QualityStore, SourceStore, and HybridSearcher
// backed by PostgreSQL with pgvector.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore connects to Postgres and returns a ready store.
// It runs schema migrations on first connect.
func NewPostgresStore(ctx context.Context, connString string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	s := &PostgresStore{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return s, nil
}

// migrate creates the schema if it doesn't exist.
func (s *PostgresStore) migrate(ctx context.Context) error {
	ddl := `
	CREATE EXTENSION IF NOT EXISTS vector;

	CREATE TABLE IF NOT EXISTS memories (
		id              TEXT PRIMARY KEY,
		content         TEXT NOT NULL,
		embedding       vector(1024) NOT NULL,
		source          TEXT NOT NULL DEFAULT '',
		quality_score   DOUBLE PRECISION NOT NULL DEFAULT 0,
		content_score   DOUBLE PRECISION NOT NULL DEFAULT 0,
		hit_count       INTEGER NOT NULL DEFAULT 0,
		last_retrieved  TIMESTAMPTZ,
		metadata        JSONB,
		created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
	);

	CREATE INDEX IF NOT EXISTS memories_embedding_idx ON memories
		USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);

	CREATE INDEX IF NOT EXISTS memories_source_idx ON memories (source);
	CREATE INDEX IF NOT EXISTS memories_content_fts ON memories USING gin (to_tsvector('english', content));

	CREATE TABLE IF NOT EXISTS retrieval_events (
		id          BIGSERIAL PRIMARY KEY,
		memory_id   TEXT NOT NULL,
		score       DOUBLE PRECISION NOT NULL,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
	);

	CREATE INDEX IF NOT EXISTS retrieval_events_memory_idx ON retrieval_events (memory_id);

	CREATE TABLE IF NOT EXISTS sources (
		id            TEXT PRIMARY KEY,
		name          TEXT NOT NULL UNIQUE,
		base_url      TEXT,
		status        TEXT NOT NULL DEFAULT 'pending',
		page_count    INTEGER NOT NULL DEFAULT 0,
		memory_count  INTEGER NOT NULL DEFAULT 0,
		max_depth     INTEGER NOT NULL DEFAULT 0,
		max_pages     INTEGER NOT NULL DEFAULT 0,
		headers       JSONB,
		last_crawled  TIMESTAMPTZ,
		created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
		error         TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS source_pages (
		id            BIGSERIAL PRIMARY KEY,
		source_id     TEXT NOT NULL,
		url           TEXT NOT NULL,
		content_hash  TEXT NOT NULL,
		last_fetched  TIMESTAMPTZ NOT NULL DEFAULT now(),
		UNIQUE(source_id, url)
	);
	`
	_, err := s.pool.Exec(ctx, ddl)
	return err
}

// newID generates a new hex ObjectID for compatibility with the rest of the codebase.
func newID() primitive.ObjectID {
	return primitive.NewObjectID()
}

// --- Store interface ---

func (s *PostgresStore) VectorSearch(ctx context.Context, embedding []float32, topK int) ([]Memory, error) {
	return s.filteredVectorSearch(ctx, embedding, topK, 0.05, "")
}

func (s *PostgresStore) Insert(ctx context.Context, mem Memory) error {
	if mem.ID.IsZero() {
		mem.ID = newID()
	}
	if mem.CreatedAt.IsZero() {
		mem.CreatedAt = time.Now()
	}

	metaJSON, err := json.Marshal(mem.Metadata)
	if err != nil {
		return fmt.Errorf("marshaling metadata: %w", err)
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO memories (id, content, embedding, source, quality_score, content_score, hit_count, last_retrieved, metadata, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		mem.ID.Hex(),
		mem.Content,
		pgvector.NewVector(mem.Embedding),
		mem.Source,
		mem.QualityScore,
		mem.ContentScore,
		mem.HitCount,
		nullTime(mem.LastRetrieved),
		metaJSON,
		mem.CreatedAt,
	)
	return err
}

func (s *PostgresStore) Delete(ctx context.Context, id string) error {
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		return fmt.Errorf("invalid memory ID %q: %w", id, err)
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM memories WHERE id = $1`, id)
	return err
}

func (s *PostgresStore) List(ctx context.Context, query string, limit int) ([]Memory, error) {
	var (
		sql  string
		args []any
	)

	if query != "" {
		sql = `SELECT id, content, source, quality_score, content_score, hit_count, last_retrieved, metadata, created_at
		       FROM memories WHERE content ~* $1 ORDER BY created_at DESC`
		args = append(args, query)
		if limit > 0 {
			sql += fmt.Sprintf(" LIMIT %d", limit)
		}
	} else {
		sql = `SELECT id, content, source, quality_score, content_score, hit_count, last_retrieved, metadata, created_at
		       FROM memories ORDER BY created_at DESC`
		if limit > 0 {
			sql += fmt.Sprintf(" LIMIT %d", limit)
		}
	}

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMemories(rows, false)
}

func (s *PostgresStore) DeleteAll(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM memories`)
	return err
}

func (s *PostgresStore) CountBySource(ctx context.Context, source string) (int64, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM memories WHERE source = $1`, source).Scan(&count)
	return count, err
}

func (s *PostgresStore) UpdateContent(ctx context.Context, id string, content string, emb []float32) error {
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		return fmt.Errorf("invalid memory ID %q: %w", id, err)
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE memories SET content = $1, embedding = $2 WHERE id = $3`,
		content, pgvector.NewVector(emb), id,
	)
	return err
}

func (s *PostgresStore) ListBySource(ctx context.Context, sourcePrefix string, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, content, source, quality_score, content_score, hit_count, last_retrieved, metadata, created_at
		 FROM memories WHERE source LIKE $1 ORDER BY created_at DESC LIMIT $2`,
		sourcePrefix+"%", limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMemories(rows, false)
}

func (s *PostgresStore) Close() error {
	s.pool.Close()
	return nil
}

// Ping checks connectivity.
func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// --- Steward support ---

func (s *PostgresStore) ListOldest(ctx context.Context, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, content, source, quality_score, content_score, hit_count, last_retrieved, metadata, created_at, embedding
		 FROM memories ORDER BY created_at ASC LIMIT $1`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMemories(rows, true)
}

func (s *PostgresStore) UpdateQualityScore(ctx context.Context, id primitive.ObjectID, score float64) error {
	_, err := s.pool.Exec(ctx, `UPDATE memories SET quality_score = $1 WHERE id = $2`, score, id.Hex())
	return err
}

// --- QualityStore interface ---

func (s *PostgresStore) RecordRetrievalBatch(ctx context.Context, events []RetrievalEvent) error {
	if len(events) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, e := range events {
		ts := e.CreatedAt
		if ts.IsZero() {
			ts = time.Now()
		}
		batch.Queue(`INSERT INTO retrieval_events (memory_id, score, created_at) VALUES ($1, $2, $3)`,
			e.MemoryID.Hex(), e.Score, ts,
		)
	}
	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()

	for range events {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) GetRetrievalCount(ctx context.Context) (int64, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM retrieval_events`).Scan(&count)
	return count, err
}

func (s *PostgresStore) IncrementHitCount(ctx context.Context, id primitive.ObjectID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE memories SET hit_count = hit_count + 1, last_retrieved = $1 WHERE id = $2`,
		time.Now(), id.Hex(),
	)
	return err
}

func (s *PostgresStore) RecentRetrievals(ctx context.Context, limit int) ([]RetrievalLog, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx,
		`SELECT re.memory_id, COALESCE(LEFT(m.content, 200), ''), COALESCE(m.source, ''), re.score, re.created_at
		 FROM retrieval_events re
		 LEFT JOIN memories m ON m.id = re.memory_id
		 ORDER BY re.created_at DESC LIMIT $1`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []RetrievalLog
	for rows.Next() {
		var l RetrievalLog
		var idHex string
		if err := rows.Scan(&idHex, &l.Content, &l.Source, &l.Score, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.MemoryID, _ = primitive.ObjectIDFromHex(idHex)
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

func (s *PostgresStore) TopMemories(ctx context.Context, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, content, source, quality_score, content_score, hit_count, last_retrieved, metadata, created_at
		 FROM memories WHERE hit_count > 0 ORDER BY hit_count DESC LIMIT $1`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMemories(rows, false)
}

// --- SourceStore interface ---

func (s *PostgresStore) InsertSource(ctx context.Context, src Source) (string, error) {
	if src.ID.IsZero() {
		src.ID = newID()
	}
	if src.CreatedAt.IsZero() {
		src.CreatedAt = time.Now()
	}
	headersJSON, err := json.Marshal(src.Headers)
	if err != nil {
		return "", err
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO sources (id, name, base_url, status, page_count, memory_count, max_depth, max_pages, headers, last_crawled, created_at, error)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		src.ID.Hex(), src.Name, src.BaseURL, src.Status, src.PageCount, src.MemoryCount,
		src.MaxDepth, src.MaxPages, headersJSON, nullTime(src.LastCrawled), src.CreatedAt, src.Error,
	)
	if err != nil {
		return "", err
	}
	return src.ID.Hex(), nil
}

func (s *PostgresStore) ListSources(ctx context.Context) ([]Source, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, base_url, status, page_count, memory_count, max_depth, max_pages, headers, last_crawled, created_at, error
		 FROM sources ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Source
	for rows.Next() {
		var src Source
		var idHex string
		var headersJSON []byte
		var lastCrawled *time.Time

		if err := rows.Scan(&idHex, &src.Name, &src.BaseURL, &src.Status, &src.PageCount, &src.MemoryCount,
			&src.MaxDepth, &src.MaxPages, &headersJSON, &lastCrawled, &src.CreatedAt, &src.Error); err != nil {
			return nil, err
		}
		src.ID, _ = primitive.ObjectIDFromHex(idHex)
		if lastCrawled != nil {
			src.LastCrawled = *lastCrawled
		}
		if len(headersJSON) > 0 {
			_ = json.Unmarshal(headersJSON, &src.Headers)
		}
		results = append(results, src)
	}
	return results, rows.Err()
}

func (s *PostgresStore) DeleteSource(ctx context.Context, id string) error {
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		return fmt.Errorf("invalid source ID: %w", err)
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM sources WHERE id = $1`, id)
	return err
}

func (s *PostgresStore) UpdateSourceStatus(ctx context.Context, id string, status string, errMsg string, pageCount int, memoryCount int) error {
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		return fmt.Errorf("invalid source ID: %w", err)
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE sources SET status = $1, error = $2, page_count = $3, memory_count = $4, last_crawled = $5 WHERE id = $6`,
		status, errMsg, pageCount, memoryCount, time.Now(), id,
	)
	return err
}

func (s *PostgresStore) GetSourcePage(ctx context.Context, sourceID primitive.ObjectID, pageURL string) (*SourcePage, error) {
	var page SourcePage
	var srcIDHex string
	err := s.pool.QueryRow(ctx,
		`SELECT source_id, url, content_hash, last_fetched FROM source_pages WHERE source_id = $1 AND url = $2`,
		sourceID.Hex(), pageURL,
	).Scan(&srcIDHex, &page.URL, &page.ContentHash, &page.LastFetched)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	page.SourceID, _ = primitive.ObjectIDFromHex(srcIDHex)
	return &page, nil
}

func (s *PostgresStore) UpsertSourcePage(ctx context.Context, page SourcePage) error {
	if page.LastFetched.IsZero() {
		page.LastFetched = time.Now()
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO source_pages (source_id, url, content_hash, last_fetched)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (source_id, url) DO UPDATE SET content_hash = EXCLUDED.content_hash, last_fetched = EXCLUDED.last_fetched`,
		page.SourceID.Hex(), page.URL, page.ContentHash, page.LastFetched,
	)
	return err
}

func (s *PostgresStore) DeleteSourcePages(ctx context.Context, sourceID primitive.ObjectID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM source_pages WHERE source_id = $1`, sourceID.Hex())
	return err
}

func (s *PostgresStore) DeleteMemoriesBySource(ctx context.Context, source string) error {
	escaped := regexp.QuoteMeta(source)
	_, err := s.pool.Exec(ctx, `DELETE FROM memories WHERE source ~ $1`, "^"+escaped)
	return err
}

// --- HybridSearcher interface ---

func (s *PostgresStore) HybridSearch(ctx context.Context, embedding []float32, topK int, opts SearchOptions) ([]Memory, error) {
	fetchK := topK * 4
	if fetchK < 20 {
		fetchK = 20
	}

	// Phase 1: filtered vector search.
	vectorResults, err := s.filteredVectorSearch(ctx, embedding, fetchK, opts.MinQualityScore, opts.Source)
	if err != nil {
		return nil, err
	}

	var merged []Memory

	if opts.TextQuery != "" {
		// Phase 2: full-text search.
		textResults, err := s.textSearch(ctx, opts.TextQuery, fetchK)
		if err != nil {
			// Non-fatal: fall back to vector-only.
			merged = vectorResults
		} else {
			merged = reciprocalRankFusion(vectorResults, textResults, 60)
		}
	} else {
		merged = vectorResults
	}

	// Phase 3: MMR re-ranking.
	if opts.DiversityMMR && len(merged) > topK {
		lambda := opts.MMRLambda
		if lambda == 0 {
			lambda = 0.7
		}
		merged = mmrRerank(merged, embedding, topK, lambda)
	}

	if len(merged) > topK {
		merged = merged[:topK]
	}

	return merged, nil
}

// --- Internal search methods ---

func (s *PostgresStore) filteredVectorSearch(ctx context.Context, embedding []float32, topK int, minQuality float64, source string) ([]Memory, error) {
	var conditions []string
	var args []any
	argN := 2 // $1 is the vector

	if minQuality > 0 {
		conditions = append(conditions, fmt.Sprintf("(quality_score >= $%d OR quality_score = 0)", argN))
		args = append(args, minQuality)
		argN++
	}
	if source != "" {
		conditions = append(conditions, fmt.Sprintf("source LIKE $%d", argN))
		args = append(args, source+"%")
		argN++
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	query := fmt.Sprintf(
		`SELECT id, content, source, quality_score, hit_count, metadata, created_at, embedding,
		        1 - (embedding <=> $1) AS score
		 FROM memories %s
		 ORDER BY embedding <=> $1
		 LIMIT %d`, where, topK,
	)

	allArgs := append([]any{pgvector.NewVector(embedding)}, args...)

	rows, err := s.pool.Query(ctx, query, allArgs...)
	if err != nil {
		return nil, fmt.Errorf("postgres vector search: %w", err)
	}
	defer rows.Close()

	var results []Memory
	for rows.Next() {
		var m Memory
		var idHex string
		var metaJSON []byte
		var vec pgvector.Vector
		if err := rows.Scan(&idHex, &m.Content, &m.Source, &m.QualityScore, &m.HitCount, &metaJSON, &m.CreatedAt, &vec, &m.Score); err != nil {
			return nil, err
		}
		m.ID, _ = primitive.ObjectIDFromHex(idHex)
		m.Embedding = vec.Slice()
		if len(metaJSON) > 0 {
			_ = json.Unmarshal(metaJSON, &m.Metadata)
		}
		results = append(results, m)
	}
	return results, rows.Err()
}

func (s *PostgresStore) textSearch(ctx context.Context, query string, topK int) ([]Memory, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, content, source, quality_score, hit_count, metadata, created_at, embedding,
		        ts_rank(to_tsvector('english', content), plainto_tsquery('english', $1)) AS score
		 FROM memories
		 WHERE to_tsvector('english', content) @@ plainto_tsquery('english', $1)
		 ORDER BY score DESC
		 LIMIT $2`, query, topK,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres text search: %w", err)
	}
	defer rows.Close()

	var results []Memory
	for rows.Next() {
		var m Memory
		var idHex string
		var metaJSON []byte
		var vec pgvector.Vector
		if err := rows.Scan(&idHex, &m.Content, &m.Source, &m.QualityScore, &m.HitCount, &metaJSON, &m.CreatedAt, &vec, &m.Score); err != nil {
			return nil, err
		}
		m.ID, _ = primitive.ObjectIDFromHex(idHex)
		m.Embedding = vec.Slice()
		if len(metaJSON) > 0 {
			_ = json.Unmarshal(metaJSON, &m.Metadata)
		}
		results = append(results, m)
	}
	return results, rows.Err()
}

// CheckVectorIndex verifies pgvector is available. For Postgres, this is
// always true after successful migration.
func (s *PostgresStore) CheckVectorIndex(ctx context.Context) error {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'vector')`,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("checking pgvector extension: %w", err)
	}
	if !exists {
		return fmt.Errorf("pgvector extension not installed — run: CREATE EXTENSION vector")
	}
	return nil
}

// --- Helpers ---

// scanMemories reads rows into Memory structs. If withEmbedding is true,
// the query must include the embedding column.
func scanMemories(rows pgx.Rows, withEmbedding bool) ([]Memory, error) {
	var results []Memory
	for rows.Next() {
		var m Memory
		var idHex string
		var metaJSON []byte
		var lastRetrieved *time.Time

		if withEmbedding {
			var vec pgvector.Vector
			if err := rows.Scan(&idHex, &m.Content, &m.Source, &m.QualityScore, &m.ContentScore,
				&m.HitCount, &lastRetrieved, &metaJSON, &m.CreatedAt, &vec); err != nil {
				return nil, err
			}
			m.Embedding = vec.Slice()
		} else {
			if err := rows.Scan(&idHex, &m.Content, &m.Source, &m.QualityScore, &m.ContentScore,
				&m.HitCount, &lastRetrieved, &metaJSON, &m.CreatedAt); err != nil {
				return nil, err
			}
		}

		m.ID, _ = primitive.ObjectIDFromHex(idHex)
		if lastRetrieved != nil {
			m.LastRetrieved = *lastRetrieved
		}
		if len(metaJSON) > 0 {
			_ = json.Unmarshal(metaJSON, &m.Metadata)
		}
		results = append(results, m)
	}
	return results, rows.Err()
}

// nullTime returns nil for zero times (for nullable TIMESTAMPTZ columns).
func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// Ensure PostgresStore compiles against all required interfaces.
var (
	_ Store          = (*PostgresStore)(nil)
	_ QualityStore   = (*PostgresStore)(nil)
	_ SourceStore    = (*PostgresStore)(nil)
	_ HybridSearcher = (*PostgresStore)(nil)
)
