package quality

import (
	"context"
	"log"

	"github.com/memory-daemon/memoryd/internal/store"
)

// DefaultThreshold is the minimum number of retrieval events before
// quality-informed filtering kicks in.
const DefaultThreshold int64 = 50

// Tracker records retrieval events and maintains quality signals on memories.
type Tracker struct {
	qs        store.QualityStore
	threshold int64
}

// NewTracker creates a quality tracker. Pass nil to disable tracking.
func NewTracker(qs store.QualityStore, threshold int64) *Tracker {
	if qs == nil {
		return nil
	}
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	return &Tracker{qs: qs, threshold: threshold}
}

// RecordHits logs that these memories were returned in a search result.
// Safe to call from a goroutine.
func (t *Tracker) RecordHits(ctx context.Context, memories []store.Memory) {
	if t == nil || t.qs == nil || len(memories) == 0 {
		return
	}

	var events []store.RetrievalEvent
	for _, m := range memories {
		if m.ID.IsZero() {
			continue
		}
		events = append(events, store.RetrievalEvent{
			MemoryID: m.ID,
			Score:    m.Score,
		})
		if err := t.qs.IncrementHitCount(ctx, m.ID); err != nil {
			log.Printf("[quality] hit count update error: %v", err)
		}
	}

	if len(events) > 0 {
		if err := t.qs.RecordRetrievalBatch(ctx, events); err != nil {
			log.Printf("[quality] record retrieval error: %v", err)
		}
	}
}

// IsLearning returns true if the system has not gathered enough retrieval
// data to make quality judgments. While learning, keep everything.
func (t *Tracker) IsLearning(ctx context.Context) bool {
	if t == nil || t.qs == nil {
		return true
	}
	count, err := t.qs.GetRetrievalCount(ctx)
	if err != nil {
		log.Printf("[quality] retrieval count error (assuming learning): %v", err)
		return true
	}
	return count < t.threshold
}

// Threshold returns the configured learning threshold.
func (t *Tracker) Threshold() int64 {
	if t == nil {
		return DefaultThreshold
	}
	return t.threshold
}

// EventCount returns the current number of retrieval events.
func (t *Tracker) EventCount(ctx context.Context) int64 {
	if t == nil || t.qs == nil {
		return 0
	}
	count, err := t.qs.GetRetrievalCount(ctx)
	if err != nil {
		return 0
	}
	return count
}
