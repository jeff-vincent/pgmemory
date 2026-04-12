package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jeff-vincent/pgmemory/internal/config"
	"github.com/jeff-vincent/pgmemory/internal/pipeline"
	"github.com/jeff-vincent/pgmemory/internal/rejection"
	"github.com/jeff-vincent/pgmemory/internal/store"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ClassifyResult is the outcome of running an exchange through the gate chain.
type ClassifyResult struct {
	Stage       string // store, filter_prefilter, filter_length, filter_noise, dedup
	WriteResult pipeline.WriteResult
}

// classifyExchange replays the proxy ingest gate chain from anthropic.go:gateAndBuffer
// in the same order: discovery → prefilter → length → (content score skipped) → ProcessFiltered.
func classifyExchange(ctx context.Context, wp *pipeline.WritePipeline, pCfg config.PipelineConfig, ex Exchange) ClassifyResult {
	assistantText := ex.Assistant

	// Gate 0: Discovery signal bypass — checked on raw text before any stripping.
	if rejection.DiscoverySignal(assistantText) {
		// Discovery bypasses all pre-filters, goes straight to storage.
		text := formatForStorage(ex.User, assistantText)
		result := wp.ProcessFiltered(text, "eval-filter", nil)
		return ClassifyResult{Stage: "store", WriteResult: result}
	}

	// Gate 1: QuickFilter (pre-filter) — short ack + procedural prefix.
	if ex.User != "" && rejection.QuickFilter(ex.User, assistantText) {
		return ClassifyResult{Stage: "filter_prefilter"}
	}

	// Gate 2: Length gate — assistant response too short.
	if pCfg.IngestMinLen > 0 && len(strings.TrimSpace(assistantText)) < pCfg.IngestMinLen {
		return ClassifyResult{Stage: "filter_length"}
	}

	// Gate 3: Content score pre-gate — SKIPPED for hash embedder (not semantically meaningful).

	// Gate 4: ProcessFiltered — noise filter, chunk, embed, dedup, store.
	text := formatForStorage(ex.User, assistantText)
	result := wp.ProcessFiltered(text, "eval-filter", nil)

	if result.Stored > 0 {
		return ClassifyResult{Stage: "store", WriteResult: result}
	}
	if result.Duplicates > 0 {
		return ClassifyResult{Stage: "dedup", WriteResult: result}
	}
	if result.Filtered > 0 {
		return ClassifyResult{Stage: "filter_noise", WriteResult: result}
	}
	// Nothing stored, nothing explicitly filtered — treat as noise.
	return ClassifyResult{Stage: "filter_noise", WriteResult: result}
}

// formatForStorage formats an exchange for the write pipeline, matching the
// proxy's storeRaw format when synthesis is unavailable.
func formatForStorage(user, assistant string) string {
	if user != "" {
		return fmt.Sprintf("Q: %s\n\nA: %s", user, assistant)
	}
	return assistant
}

// ---------------------------------------------------------------------------
// In-memory store (copied from cmd/validate/memstore.go)
// ---------------------------------------------------------------------------

type memStore struct {
	mu              sync.RWMutex
	memories        map[primitive.ObjectID]*store.Memory
	retrievalEvents []store.RetrievalEvent
}

func newMemStore() *memStore {
	return &memStore{
		memories: make(map[primitive.ObjectID]*store.Memory),
	}
}

func (s *memStore) Insert(_ context.Context, mem store.Memory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mem.ID.IsZero() {
		mem.ID = primitive.NewObjectID()
	}
	if mem.CreatedAt.IsZero() {
		mem.CreatedAt = time.Now()
	}
	cp := mem
	s.memories[cp.ID] = &cp
	return nil
}

func (s *memStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	delete(s.memories, oid)
	return nil
}

func (s *memStore) List(_ context.Context, query string, limit int) ([]store.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []store.Memory
	for _, m := range s.memories {
		if query != "" && !strings.Contains(strings.ToLower(m.Content), strings.ToLower(query)) {
			continue
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *memStore) DeleteAll(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memories = make(map[primitive.ObjectID]*store.Memory)
	return nil
}

func (s *memStore) CountBySource(_ context.Context, source string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var n int64
	for _, m := range s.memories {
		if m.Source == source {
			n++
		}
	}
	return n, nil
}

func (s *memStore) UpdateContent(_ context.Context, id string, content string, emb []float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	if m, ok := s.memories[oid]; ok {
		m.Content = content
		m.Embedding = emb
	}
	return nil
}

func (s *memStore) ListBySource(_ context.Context, prefix string, limit int) ([]store.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []store.Memory
	for _, m := range s.memories {
		if strings.HasPrefix(m.Source, prefix) {
			out = append(out, *m)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *memStore) Close() error { return nil }

func (s *memStore) VectorSearch(_ context.Context, emb []float32, topK int) ([]store.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	type scored struct {
		mem   store.Memory
		score float64
	}
	var results []scored
	for _, m := range s.memories {
		if len(m.Embedding) == 0 {
			continue
		}
		sim := cosine(emb, m.Embedding)
		results = append(results, scored{mem: *m, score: sim})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})
	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	var out []store.Memory
	for _, r := range results {
		r.mem.Score = r.score
		out = append(out, r.mem)
	}
	return out, nil
}

func (s *memStore) count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.memories)
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	d := math.Sqrt(na) * math.Sqrt(nb)
	if d == 0 {
		return 0
	}
	return dot / d
}

// ---------------------------------------------------------------------------
// Hash embedder (copied from cmd/validate/embedder.go)
// ---------------------------------------------------------------------------

type hashEmbedder struct {
	dim   int
	ngram int
}

func newHashEmbedder(dim int) *hashEmbedder {
	return &hashEmbedder{dim: dim, ngram: 5}
}

func (e *hashEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	return e.embed(text), nil
}

func (e *hashEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i, t := range texts {
		vecs[i] = e.embed(t)
	}
	return vecs, nil
}

func (e *hashEmbedder) Dim() int     { return e.dim }
func (e *hashEmbedder) Close() error { return nil }

func (e *hashEmbedder) embed(text string) []float32 {
	vec := make([]float32, e.dim)
	runes := []rune(text)
	if len(runes) < e.ngram {
		h := sha256.Sum256([]byte(text))
		for i := 0; i < e.dim; i++ {
			vec[i] = float32(h[i%32]) / 128.0
		}
		normalize(vec)
		return vec
	}
	for i := 0; i <= len(runes)-e.ngram; i++ {
		gram := string(runes[i : i+e.ngram])
		h := sha256.Sum256([]byte(gram))
		bucket := int(binary.LittleEndian.Uint32(h[:4])) % e.dim
		val := float32(int8(h[4])) / 128.0
		vec[bucket] += val
	}
	normalize(vec)
	return vec
}

func normalize(vec []float32) {
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return
	}
	for i := range vec {
		vec[i] = float32(float64(vec[i]) / norm)
	}
}
