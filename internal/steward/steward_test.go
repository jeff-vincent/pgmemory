package steward

import (
	"context"
	"testing"
	"time"

	"github.com/memory-daemon/memoryd/internal/store"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// --- Mocks ---

type mockEmbedder struct {
	dim       int
	calls     int
	lastTexts []string
}

func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	m.calls++
	m.lastTexts = append(m.lastTexts, text)
	vec := make([]float32, m.dim)
	vec[0] = 1.0
	return vec, nil
}

func (m *mockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := m.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		vecs[i] = v
	}
	return vecs, nil
}

func (m *mockEmbedder) Dim() int     { return m.dim }
func (m *mockEmbedder) Close() error { return nil }

type mockStewardStore struct {
	memories       []store.Memory
	deleted        []string
	updatedContent map[string]string
	updatedScores  map[primitive.ObjectID]float64
	searchResults  map[primitive.ObjectID][]store.Memory
}

func newMockStewardStore() *mockStewardStore {
	return &mockStewardStore{
		updatedContent: make(map[string]string),
		updatedScores:  make(map[primitive.ObjectID]float64),
		searchResults:  make(map[primitive.ObjectID][]store.Memory),
	}
}

func (m *mockStewardStore) ListOldest(_ context.Context, limit int) ([]store.Memory, error) {
	if limit >= len(m.memories) {
		return m.memories, nil
	}
	return m.memories[:limit], nil
}

func (m *mockStewardStore) UpdateQualityScore(_ context.Context, id primitive.ObjectID, score float64) error {
	m.updatedScores[id] = score
	return nil
}

func (m *mockStewardStore) VectorSearch(_ context.Context, emb []float32, topK int) ([]store.Memory, error) {
	for id, results := range m.searchResults {
		for _, mem := range m.memories {
			if mem.ID == id && len(mem.Embedding) > 0 && mem.Embedding[0] == emb[0] {
				if topK < len(results) {
					return results[:topK], nil
				}
				return results, nil
			}
		}
	}
	return nil, nil
}

func (m *mockStewardStore) Insert(_ context.Context, _ store.Memory) error { return nil }
func (m *mockStewardStore) Delete(_ context.Context, id string) error {
	m.deleted = append(m.deleted, id)
	return nil
}
func (m *mockStewardStore) List(_ context.Context, _ string, _ int) ([]store.Memory, error) {
	return m.memories, nil
}
func (m *mockStewardStore) DeleteAll(_ context.Context) error                        { return nil }
func (m *mockStewardStore) CountBySource(_ context.Context, _ string) (int64, error) { return 0, nil }
func (m *mockStewardStore) UpdateContent(_ context.Context, id string, content string, _ []float32) error {
	m.updatedContent[id] = content
	return nil
}
func (m *mockStewardStore) ListBySource(_ context.Context, _ string, _ int) ([]store.Memory, error) {
	return nil, nil
}
func (m *mockStewardStore) Close() error { return nil }

// --- combineContent tests ---

func TestCombineContent_DropContainedInKeep(t *testing.T) {
	keep := "Go uses goroutines for concurrency and channels for communication."
	drop := "goroutines for concurrency"
	got := combineContent(keep, drop)
	if got != keep {
		t.Errorf("expected keep unchanged, got %q", got)
	}
}

func TestCombineContent_KeepContainedInDrop(t *testing.T) {
	keep := "vector search"
	drop := "MongoDB Atlas supports vector search with cosine similarity."
	got := combineContent(keep, drop)
	if got != drop {
		t.Errorf("expected drop (richer), got %q", got)
	}
}

func TestCombineContent_SuffixPrefixOverlap(t *testing.T) {
	keep := "The steward scores memories and prunes low-quality ones"
	drop := "prunes low-quality ones and merges near-duplicates"
	got := combineContent(keep, drop)
	want := "The steward scores memories and prunes low-quality ones and merges near-duplicates"
	if got != want {
		t.Errorf("expected splice:\n  got:  %q\n  want: %q", got, want)
	}
}

func TestCombineContent_NoOverlap_ConcatenatesLongerFirst(t *testing.T) {
	keep := "Go is a compiled language."
	drop := "Python is interpreted."
	got := combineContent(keep, drop)
	want := "Go is a compiled language.\n\nPython is interpreted."
	if got != want {
		t.Errorf("expected concatenation:\n  got:  %q\n  want: %q", got, want)
	}
}

func TestCombineContent_NoOverlap_ShorterKeep(t *testing.T) {
	keep := "Short."
	drop := "This is the longer text that should come first."
	got := combineContent(keep, drop)
	want := "This is the longer text that should come first.\n\nShort."
	if got != want {
		t.Errorf("expected longer first:\n  got:  %q\n  want: %q", got, want)
	}
}

// --- isSubstring tests ---

func TestIsSubstring(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"hello world", "hello", true},
		{"hello", "hello world", true},
		{"abc", "xyz", false},
		{"same", "same", true},
		{"", "anything", true},
	}
	for _, tc := range tests {
		got := isSubstring(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("isSubstring(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// --- spliceOverlap tests ---

func TestSpliceOverlap_SuffixPrefix(t *testing.T) {
	a := "The quick brown fox jumps"
	b := "brown fox jumps over the lazy dog"
	got, ok := spliceOverlap(a, b, 10)
	if !ok {
		t.Fatal("expected overlap to be found")
	}
	want := "The quick brown fox jumps over the lazy dog"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSpliceOverlap_NoOverlap(t *testing.T) {
	_, ok := spliceOverlap("abc", "xyz", 2)
	if ok {
		t.Error("expected no overlap")
	}
}

func TestSpliceOverlap_OverlapTooShort(t *testing.T) {
	a := "hello world"
	b := "world peace"
	_, ok := spliceOverlap(a, b, 10)
	if ok {
		t.Error("expected overlap too short to match with minLen=10")
	}
}

func TestSpliceOverlap_ReverseDirection(t *testing.T) {
	a := "over the lazy dog rests"
	b := "The fox jumps over the lazy dog"
	got, ok := spliceOverlap(a, b, 10)
	if !ok {
		t.Fatal("expected reverse-direction overlap")
	}
	want := "The fox jumps over the lazy dog rests"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// --- Steward constructor ---

func TestNew_DefaultConfig(t *testing.T) {
	st := newMockStewardStore()
	s := New(Config{}, st, nil)
	if s.cfg.Interval != 1*time.Hour {
		t.Errorf("expected default interval 1h, got %v", s.cfg.Interval)
	}
	if s.cfg.MergeThreshold != 0.88 {
		t.Errorf("expected default merge threshold 0.88, got %v", s.cfg.MergeThreshold)
	}
}

func TestNew_NilEmbedder(t *testing.T) {
	st := newMockStewardStore()
	s := New(DefaultConfig(), st, nil)
	if s.embedder != nil {
		t.Error("expected nil embedder")
	}
}

func TestNew_WithEmbedder(t *testing.T) {
	st := newMockStewardStore()
	emb := &mockEmbedder{dim: 1024}
	s := New(DefaultConfig(), st, emb)
	if s.embedder == nil {
		t.Error("expected non-nil embedder")
	}
}

// --- Merge integration tests ---

func TestMergeNearDuplicates_CombinesContent(t *testing.T) {
	st := newMockStewardStore()
	emb := &mockEmbedder{dim: 4}

	id1 := primitive.NewObjectID()
	id2 := primitive.NewObjectID()

	mem1 := store.Memory{
		ID:        id1,
		Content:   "Go uses goroutines for concurrency.",
		Embedding: []float32{1.0, 0.0, 0.0, 0.0},
		HitCount:  5,
		CreatedAt: time.Now().Add(-48 * time.Hour),
	}
	mem2 := store.Memory{
		ID:        id2,
		Content:   "Python uses asyncio for concurrency.",
		Embedding: []float32{1.0, 0.0, 0.0, 0.0},
		HitCount:  1,
		CreatedAt: time.Now().Add(-24 * time.Hour),
		Score:     0.95,
	}

	st.memories = []store.Memory{mem1, mem2}
	st.searchResults[id1] = []store.Memory{mem1, mem2}

	s := New(Config{
		Interval:       time.Hour,
		MergeThreshold: 0.88,
		BatchSize:      100,
	}, st, emb)

	merged, err := s.mergeNearDuplicates(context.Background())
	if err != nil {
		t.Fatalf("mergeNearDuplicates error: %v", err)
	}

	if merged != 1 {
		t.Errorf("expected 1 merged, got %d", merged)
	}

	if len(st.deleted) != 1 || st.deleted[0] != id2.Hex() {
		t.Errorf("expected mem2 deleted, got deleted=%v", st.deleted)
	}

	combined, ok := st.updatedContent[id1.Hex()]
	if !ok {
		t.Fatal("expected content update on kept memory")
	}
	if combined == mem1.Content {
		t.Error("expected combined content to differ from original")
	}
	if len(combined) <= len(mem1.Content) {
		t.Errorf("combined content should be longer, got %d chars vs original %d",
			len(combined), len(mem1.Content))
	}

	if emb.calls != 1 {
		t.Errorf("expected 1 embed call for re-embedding, got %d", emb.calls)
	}
}

func TestMergeNearDuplicates_SkipsReEmbedWhenSubstring(t *testing.T) {
	st := newMockStewardStore()
	emb := &mockEmbedder{dim: 4}

	id1 := primitive.NewObjectID()
	id2 := primitive.NewObjectID()

	mem1 := store.Memory{
		ID:        id1,
		Content:   "Go uses goroutines for lightweight concurrency handling.",
		Embedding: []float32{1.0, 0.0, 0.0, 0.0},
		HitCount:  5,
		CreatedAt: time.Now().Add(-48 * time.Hour),
	}
	mem2 := store.Memory{
		ID:        id2,
		Content:   "goroutines for lightweight concurrency",
		Embedding: []float32{1.0, 0.0, 0.0, 0.0},
		HitCount:  1,
		CreatedAt: time.Now().Add(-24 * time.Hour),
		Score:     0.95,
	}

	st.memories = []store.Memory{mem1, mem2}
	st.searchResults[id1] = []store.Memory{mem1, mem2}

	s := New(Config{
		Interval:       time.Hour,
		MergeThreshold: 0.88,
		BatchSize:      100,
	}, st, emb)

	merged, err := s.mergeNearDuplicates(context.Background())
	if err != nil {
		t.Fatalf("mergeNearDuplicates error: %v", err)
	}
	if merged != 1 {
		t.Errorf("expected 1 merged, got %d", merged)
	}

	// Substring case: no re-embedding needed.
	if emb.calls != 0 {
		t.Errorf("expected 0 embed calls (substring case), got %d", emb.calls)
	}
	if _, ok := st.updatedContent[id1.Hex()]; ok {
		t.Error("expected no content update when drop is a substring of keep")
	}
}

func TestMergeNearDuplicates_NilEmbedder_StillDeletes(t *testing.T) {
	st := newMockStewardStore()

	id1 := primitive.NewObjectID()
	id2 := primitive.NewObjectID()

	mem1 := store.Memory{
		ID:        id1,
		Content:   "Architecture pattern for microservices.",
		Embedding: []float32{1.0, 0.0, 0.0, 0.0},
		HitCount:  3,
		CreatedAt: time.Now().Add(-48 * time.Hour),
	}
	mem2 := store.Memory{
		ID:        id2,
		Content:   "Microservices architecture best practices.",
		Embedding: []float32{1.0, 0.0, 0.0, 0.0},
		HitCount:  1,
		CreatedAt: time.Now().Add(-24 * time.Hour),
		Score:     0.90,
	}

	st.memories = []store.Memory{mem1, mem2}
	st.searchResults[id1] = []store.Memory{mem1, mem2}

	s := New(Config{
		Interval:       time.Hour,
		MergeThreshold: 0.88,
		BatchSize:      100,
	}, st, nil)

	merged, err := s.mergeNearDuplicates(context.Background())
	if err != nil {
		t.Fatalf("mergeNearDuplicates error: %v", err)
	}
	if merged != 1 {
		t.Errorf("expected 1 merged, got %d", merged)
	}

	if len(st.deleted) != 1 || st.deleted[0] != id2.Hex() {
		t.Errorf("expected mem2 deleted, got deleted=%v", st.deleted)
	}
	if len(st.updatedContent) != 0 {
		t.Errorf("expected no content updates without embedder, got %d", len(st.updatedContent))
	}
}

func TestMergeNearDuplicates_BelowThreshold_NoMerge(t *testing.T) {
	st := newMockStewardStore()
	emb := &mockEmbedder{dim: 4}

	id1 := primitive.NewObjectID()
	id2 := primitive.NewObjectID()

	mem1 := store.Memory{
		ID:        id1,
		Content:   "Go concurrency patterns.",
		Embedding: []float32{1.0, 0.0, 0.0, 0.0},
		HitCount:  2,
		CreatedAt: time.Now().Add(-48 * time.Hour),
	}
	mem2 := store.Memory{
		ID:        id2,
		Content:   "Completely unrelated topic.",
		Embedding: []float32{0.0, 1.0, 0.0, 0.0},
		Score:     0.3,
	}

	st.memories = []store.Memory{mem1}
	st.searchResults[id1] = []store.Memory{mem1, mem2}

	s := New(Config{
		Interval:       time.Hour,
		MergeThreshold: 0.88,
		BatchSize:      100,
	}, st, emb)

	merged, err := s.mergeNearDuplicates(context.Background())
	if err != nil {
		t.Fatalf("mergeNearDuplicates error: %v", err)
	}
	if merged != 0 {
		t.Errorf("expected 0 merged (below threshold), got %d", merged)
	}
	if len(st.deleted) != 0 {
		t.Errorf("expected 0 deletions, got %d", len(st.deleted))
	}
}

// --- LastStats ---

func TestLastStats_ZeroBeforeSweep(t *testing.T) {
	st := newMockStewardStore()
	s := New(DefaultConfig(), st, nil)
	stats := s.LastStats()
	if stats.Scored != 0 || stats.Pruned != 0 || stats.Merged != 0 {
		t.Errorf("expected zero stats before sweep, got %+v", stats)
	}
}

func TestStats_String(t *testing.T) {
	s := Stats{Scored: 10, Pruned: 2, Merged: 1, Elapsed: 150 * time.Millisecond}
	got := s.String()
	if got != "scored=10 pruned=2 merged=1 elapsed=150ms" {
		t.Errorf("unexpected String(): %q", got)
	}
}
