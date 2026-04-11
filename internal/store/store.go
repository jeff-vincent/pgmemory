package store

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Memory is a stored chunk of context.
type Memory struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Content       string             `bson:"content" json:"content"`
	Embedding     []float32          `bson:"embedding" json:"-"`
	Source        string             `bson:"source" json:"source"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
	Metadata      map[string]any     `bson:"metadata,omitempty" json:"metadata,omitempty"`
	Score         float64            `bson:"score,omitempty" json:"score,omitempty"`
	HitCount      int                `bson:"hit_count,omitempty" json:"hit_count,omitempty"`
	QualityScore  float64            `bson:"quality_score,omitempty" json:"quality_score,omitempty"`
	ContentScore  float64            `bson:"content_score,omitempty" json:"content_score,omitempty"`
	LastRetrieved time.Time          `bson:"last_retrieved,omitempty" json:"last_retrieved,omitempty"`
}

// RetrievalEvent records when a memory appears in search results.
type RetrievalEvent struct {
	MemoryID  primitive.ObjectID `bson:"memory_id"`
	Score     float64            `bson:"score"`
	CreatedAt time.Time          `bson:"created_at"`
}

// Source represents a crawled data source (wiki, docs site, etc).
type Source struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name        string             `bson:"name" json:"name"`
	BaseURL     string             `bson:"base_url" json:"base_url"`
	Status      string             `bson:"status" json:"status"`
	PageCount   int                `bson:"page_count" json:"page_count"`
	MemoryCount int                `bson:"memory_count" json:"memory_count"`
	MaxDepth    int                `bson:"max_depth" json:"max_depth"`
	MaxPages    int                `bson:"max_pages" json:"max_pages"`
	Headers     map[string]string  `bson:"headers,omitempty" json:"headers,omitempty"`
	LastCrawled time.Time          `bson:"last_crawled,omitempty" json:"last_crawled,omitempty"`
	CreatedAt   time.Time          `bson:"created_at" json:"created_at"`
	Error       string             `bson:"error,omitempty" json:"error,omitempty"`
}

// SourcePage tracks a crawled page for change detection.
type SourcePage struct {
	SourceID    primitive.ObjectID `bson:"source_id"`
	URL         string             `bson:"url"`
	ContentHash string             `bson:"content_hash"`
	LastFetched time.Time          `bson:"last_fetched"`
}

// Store reads and writes memories.
type Store interface {
	// VectorSearch returns the top-k memories most similar to the embedding.
	VectorSearch(ctx context.Context, embedding []float32, topK int) ([]Memory, error)

	// Insert stores a new memory.
	Insert(ctx context.Context, mem Memory) error

	// Delete removes a memory by hex ID.
	Delete(ctx context.Context, id string) error

	// List returns memories, optionally filtered by a text query.
	List(ctx context.Context, query string, limit int) ([]Memory, error)

	// DeleteAll removes every memory.
	DeleteAll(ctx context.Context) error

	// CountBySource counts memories with the given source value.
	CountBySource(ctx context.Context, source string) (int64, error)

	// UpdateContent updates the content and embedding of a memory by hex ID.
	UpdateContent(ctx context.Context, id string, content string, embedding []float32) error

	// ListBySource returns memories whose source field starts with the given prefix.
	ListBySource(ctx context.Context, sourcePrefix string, limit int) ([]Memory, error)

	// Close releases the connection.
	Close() error
}

// RetrievalLog is a retrieval event enriched with memory content.
type RetrievalLog struct {
	MemoryID  primitive.ObjectID `json:"memory_id"`
	Content   string             `json:"content"`
	Source    string             `json:"source"`
	Score     float64            `json:"score"`
	CreatedAt time.Time          `json:"created_at"`
}

// QualityStore tracks retrieval events for adaptive quality learning.
type QualityStore interface {
	RecordRetrievalBatch(ctx context.Context, events []RetrievalEvent) error
	GetRetrievalCount(ctx context.Context) (int64, error)
	IncrementHitCount(ctx context.Context, id primitive.ObjectID) error
	RecentRetrievals(ctx context.Context, limit int) ([]RetrievalLog, error)
	TopMemories(ctx context.Context, limit int) ([]Memory, error)
}

// SourceStore manages ingested data sources.
type SourceStore interface {
	InsertSource(ctx context.Context, src Source) (string, error)
	ListSources(ctx context.Context) ([]Source, error)
	DeleteSource(ctx context.Context, id string) error
	UpdateSourceStatus(ctx context.Context, id string, status string, err string, pageCount int, memoryCount int) error
	GetSourcePage(ctx context.Context, sourceID primitive.ObjectID, url string) (*SourcePage, error)
	UpsertSourcePage(ctx context.Context, page SourcePage) error
	DeleteSourcePages(ctx context.Context, sourceID primitive.ObjectID) error
	DeleteMemoriesBySource(ctx context.Context, source string) error
}

// SearchOptions controls optional search behaviour available on Atlas proper.
type SearchOptions struct {
	// MinQualityScore filters out memories below this quality_score (0 = no filter).
	MinQualityScore float64

	// Source restricts search to memories from this source prefix.
	Source string

	// TextQuery adds a keyword component to the search (hybrid mode).
	TextQuery string

	// DiversityMMR enables Maximal Marginal Relevance re-ranking.
	// When true, returns a more diverse set rather than the most similar cluster.
	DiversityMMR bool

	// MMRLambda controls the relevance vs diversity trade-off (0=max diversity, 1=max relevance).
	// Default 0.7 if zero.
	MMRLambda float64
}

// HybridSearcher is implemented by store backends that support Atlas-level features:
// pre-filtered vector search, full-text search, and hybrid (vector + text) retrieval.
type HybridSearcher interface {
	// HybridSearch combines vector similarity with optional text matching,
	// quality pre-filtering, and MMR diversity re-ranking.
	HybridSearch(ctx context.Context, embedding []float32, topK int, opts SearchOptions) ([]Memory, error)
}
