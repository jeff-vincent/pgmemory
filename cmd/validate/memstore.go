package main

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/memory-daemon/memoryd/internal/store"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// memStore is a fully in-memory implementation of steward.StewardStore
// and store.QualityStore. No MongoDB required.
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

// --- store.Store interface ---

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

// VectorSearch returns top-k by cosine similarity.
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

// --- StewardStore interface ---

func (s *memStore) ListOldest(_ context.Context, limit int) ([]store.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []store.Memory
	for _, m := range s.memories {
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

func (s *memStore) UpdateQualityScore(_ context.Context, id primitive.ObjectID, score float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.memories[id]; ok {
		m.QualityScore = score
	}
	return nil
}

// --- QualityStore interface ---

func (s *memStore) RecordRetrievalBatch(_ context.Context, events []store.RetrievalEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range events {
		if events[i].CreatedAt.IsZero() {
			events[i].CreatedAt = time.Now()
		}
	}
	s.retrievalEvents = append(s.retrievalEvents, events...)
	return nil
}

func (s *memStore) GetRetrievalCount(_ context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.retrievalEvents)), nil
}

func (s *memStore) IncrementHitCount(_ context.Context, id primitive.ObjectID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.memories[id]; ok {
		m.HitCount++
		m.LastRetrieved = time.Now()
	}
	return nil
}

func (s *memStore) RecentRetrievals(_ context.Context, _ int) ([]store.RetrievalLog, error) {
	return nil, nil
}

func (s *memStore) TopMemories(_ context.Context, limit int) ([]store.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []store.Memory
	for _, m := range s.memories {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].HitCount > out[j].HitCount
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// --- helpers ---

func (s *memStore) count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.memories)
}

func (s *memStore) snapshot() []store.Memory {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []store.Memory
	for _, m := range s.memories {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
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
