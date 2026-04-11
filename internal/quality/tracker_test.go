package quality

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/memory-daemon/memoryd/internal/store"
)

type mockQualityStore struct {
	events   []store.RetrievalEvent
	hitCount map[primitive.ObjectID]int
	count    int64
}

func newMockQS() *mockQualityStore {
	return &mockQualityStore{hitCount: make(map[primitive.ObjectID]int)}
}

func (m *mockQualityStore) RecordRetrievalBatch(_ context.Context, events []store.RetrievalEvent) error {
	m.events = append(m.events, events...)
	m.count += int64(len(events))
	return nil
}

func (m *mockQualityStore) GetRetrievalCount(_ context.Context) (int64, error) {
	return m.count, nil
}

func (m *mockQualityStore) IncrementHitCount(_ context.Context, id primitive.ObjectID) error {
	m.hitCount[id]++
	return nil
}

func (m *mockQualityStore) RecentRetrievals(_ context.Context, _ int) ([]store.RetrievalLog, error) {
	return nil, nil
}

func (m *mockQualityStore) TopMemories(_ context.Context, _ int) ([]store.Memory, error) {
	return nil, nil
}

func TestNewTracker_NilStore(t *testing.T) {
	tracker := NewTracker(nil, 50)
	if tracker != nil {
		t.Error("expected nil tracker for nil store")
	}
}

func TestNewTracker_DefaultThreshold(t *testing.T) {
	qs := newMockQS()
	tracker := NewTracker(qs, 0)
	if tracker.Threshold() != DefaultThreshold {
		t.Errorf("threshold = %d, want %d", tracker.Threshold(), DefaultThreshold)
	}
}

func TestTracker_RecordHits(t *testing.T) {
	qs := newMockQS()
	tracker := NewTracker(qs, 50)

	id1 := primitive.NewObjectID()
	id2 := primitive.NewObjectID()

	memories := []store.Memory{
		{ID: id1, Content: "test1", Score: 0.9},
		{ID: id2, Content: "test2", Score: 0.8},
	}

	tracker.RecordHits(context.Background(), memories)

	if len(qs.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(qs.events))
	}
	if qs.hitCount[id1] != 1 {
		t.Errorf("hit count for id1 = %d, want 1", qs.hitCount[id1])
	}
	if qs.hitCount[id2] != 1 {
		t.Errorf("hit count for id2 = %d, want 1", qs.hitCount[id2])
	}
}

func TestTracker_RecordHits_SkipsZeroID(t *testing.T) {
	qs := newMockQS()
	tracker := NewTracker(qs, 50)

	memories := []store.Memory{
		{Content: "no id", Score: 0.5},
	}

	tracker.RecordHits(context.Background(), memories)

	if len(qs.events) != 0 {
		t.Errorf("expected 0 events for zero ID, got %d", len(qs.events))
	}
}

func TestTracker_RecordHits_NilTracker(t *testing.T) {
	var tracker *Tracker
	// Should not panic.
	tracker.RecordHits(context.Background(), []store.Memory{{Content: "test"}})
}

func TestTracker_IsLearning(t *testing.T) {
	qs := newMockQS()
	tracker := NewTracker(qs, 10)

	if !tracker.IsLearning(context.Background()) {
		t.Error("expected learning with 0 events")
	}

	// Record enough events to pass threshold.
	for i := 0; i < 10; i++ {
		qs.count++
	}

	if tracker.IsLearning(context.Background()) {
		t.Error("expected not learning with 10 events (threshold 10)")
	}
}

func TestTracker_IsLearning_NilTracker(t *testing.T) {
	var tracker *Tracker
	if !tracker.IsLearning(context.Background()) {
		t.Error("nil tracker should always be learning")
	}
}

func TestTracker_EventCount(t *testing.T) {
	qs := newMockQS()
	tracker := NewTracker(qs, 50)

	if tracker.EventCount(context.Background()) != 0 {
		t.Error("expected 0 events initially")
	}

	qs.count = 42
	if tracker.EventCount(context.Background()) != 42 {
		t.Errorf("expected 42, got %d", tracker.EventCount(context.Background()))
	}
}

func TestTracker_EventCount_NilTracker(t *testing.T) {
	var tracker *Tracker
	if tracker.EventCount(context.Background()) != 0 {
		t.Error("nil tracker should return 0")
	}
}
