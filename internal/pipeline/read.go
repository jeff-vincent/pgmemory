package pipeline

import (
	"context"

	"github.com/memory-daemon/memoryd/internal/config"
	"github.com/memory-daemon/memoryd/internal/embedding"
	"github.com/memory-daemon/memoryd/internal/quality"
	"github.com/memory-daemon/memoryd/internal/store"
)

// ReadPipeline handles the pre-prompt path: embed the user message,
// search for relevant memories, and format them for injection.
type ReadPipeline struct {
	embedder embedding.Embedder
	store    store.Store
	cfg      *config.Config
	quality  *quality.Tracker
}

// ReadOption configures the ReadPipeline.
type ReadOption func(*ReadPipeline)

// WithQualityTracker attaches a quality tracker to the read pipeline.
func WithQualityTracker(qt *quality.Tracker) ReadOption {
	return func(rp *ReadPipeline) { rp.quality = qt }
}

func NewReadPipeline(e embedding.Embedder, s store.Store, cfg *config.Config, opts ...ReadOption) *ReadPipeline {
	rp := &ReadPipeline{embedder: e, store: s, cfg: cfg}
	for _, o := range opts {
		o(rp)
	}
	return rp
}

// Retrieve returns formatted context ready for system prompt injection,
// or an empty string if nothing relevant is found.
func (rp *ReadPipeline) Retrieve(ctx context.Context, userMessage string) (string, error) {
	memories, err := rp.search(ctx, userMessage)
	if err != nil {
		return "", err
	}
	return FormatContext(memories, rp.cfg.RetrievalMaxTokens), nil
}

// RetrieveWithScores is like Retrieve but also returns the raw memories so
// callers can inspect retrieval scores for instrumentation and eval.
func (rp *ReadPipeline) RetrieveWithScores(ctx context.Context, userMessage string) (string, []store.Memory, error) {
	memories, err := rp.search(ctx, userMessage)
	if err != nil {
		return "", nil, err
	}
	return FormatContext(memories, rp.cfg.RetrievalMaxTokens), memories, nil
}

// search embeds the query, runs the appropriate search, records quality hits,
// and returns the raw memories with their scores populated.
func (rp *ReadPipeline) search(ctx context.Context, userMessage string) ([]store.Memory, error) {
	if rp.embedder == nil || rp.store == nil {
		return nil, nil
	}

	vec, err := rp.embedder.Embed(ctx, userMessage)
	if err != nil {
		return nil, err
	}

	var memories []store.Memory

	// If the store supports hybrid search (Atlas proper), use it.
	if hs, ok := rp.store.(store.HybridSearcher); ok {
		memories, err = hs.HybridSearch(ctx, vec, rp.cfg.RetrievalTopK, store.SearchOptions{
			MinQualityScore: 0.05,
			TextQuery:       userMessage,
			DiversityMMR:    true,
			MMRLambda:       0.7,
		})
	} else {
		memories, err = rp.store.VectorSearch(ctx, vec, rp.cfg.RetrievalTopK)
	}
	if err != nil {
		return nil, err
	}

	// Record retrieval hits for quality learning.
	if rp.quality != nil && len(memories) > 0 {
		go rp.quality.RecordHits(context.Background(), memories)
	}

	return memories, nil
}
