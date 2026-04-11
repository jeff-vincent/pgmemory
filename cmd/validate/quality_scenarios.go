package main

import (
	"context"
	"fmt"
	"time"

	"github.com/memory-daemon/memoryd/internal/quality"
	"github.com/memory-daemon/memoryd/internal/store"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// scenarioContentScorerRange verifies that NewContentScorer initialises without
// error and that Score always returns a value in [0.0, 1.0].
// With the hash embedder the scores won't be semantically meaningful, but the
// arithmetic must be correct regardless of the embedding model in use.
func scenarioContentScorerRange(ctx context.Context) error {
	emb := newHashEmbedder(128)

	cs, err := quality.NewContentScorer(ctx, emb)
	if err != nil {
		return fmt.Errorf("NewContentScorer: %w", err)
	}
	if cs == nil {
		return fmt.Errorf("NewContentScorer returned nil scorer")
	}

	inputs := []string{
		// Obviously high-value technical content.
		"The deduplication threshold of 0.92 cosine similarity was chosen empirically after benchmarking showed it eliminates 97% of near-duplicate pairs without blocking legitimate updates.",
		// Obvious noise/filler.
		"Sure, I can help you with that! Let me know if there is anything else you need.",
		// Edge case: very short but above minLen.
		"Error: connection refused on port 7432.",
		// Edge case: entirely numeric.
		"0.92 0.88 0.75 0.65 128 1024 7432 7433",
	}

	for _, input := range inputs {
		vec, err := emb.Embed(ctx, input)
		if err != nil {
			return fmt.Errorf("embed %q: %w", input[:min(40, len(input))], err)
		}
		score := cs.Score(vec)
		if score < 0 || score > 1 {
			return fmt.Errorf("score out of [0,1] range: %.6f for input %q", score, input[:min(40, len(input))])
		}
	}

	// Nil scorer must return 0.5 (neutral default) not panic.
	var nilScorer *quality.ContentScorer
	vec, _ := emb.Embed(ctx, "anything")
	neutralScore := nilScorer.Score(vec)
	if neutralScore != 0.5 {
		return fmt.Errorf("nil scorer should return 0.5 neutral default, got %.4f", neutralScore)
	}

	if *verbose {
		for _, input := range inputs {
			vec, _ := emb.Embed(ctx, input)
			fmt.Printf("\n    score=%.4f  %q", cs.Score(vec), input[:min(60, len(input))])
		}
		fmt.Println()
	}
	return nil
}

// scenarioContentScaleHalfLife verifies the ContentScaleHalfLife scaling math.
// At content_score=1.0 the effective half-life should equal the base.
// At content_score=0.0 the effective half-life should be the 7-day minimum.
func scenarioContentScaleHalfLife(_ context.Context) error {
	baseHalfLife := float64(90 * 24 * time.Hour)
	minHalfLife := float64(7 * 24 * time.Hour)

	// Perfect content score: full base half-life.
	got := quality.ContentScaleHalfLife(baseHalfLife, 1.0)
	if got != baseHalfLife {
		return fmt.Errorf("content_score=1.0 should give base half-life %.0f, got %.0f", baseHalfLife, got)
	}

	// Zero content score: minimum half-life.
	got = quality.ContentScaleHalfLife(baseHalfLife, 0.0)
	if got != minHalfLife {
		return fmt.Errorf("content_score=0.0 should give min half-life %.0f, got %.0f", minHalfLife, got)
	}

	// Mid-range: should be between min and base.
	got = quality.ContentScaleHalfLife(baseHalfLife, 0.5)
	if got <= minHalfLife || got >= baseHalfLife {
		return fmt.Errorf("content_score=0.5 half-life %.0f should be between %.0f and %.0f", got, minHalfLife, baseHalfLife)
	}

	// Clamping: scores outside [0,1] should clamp.
	gotOver := quality.ContentScaleHalfLife(baseHalfLife, 1.5)
	if gotOver != baseHalfLife {
		return fmt.Errorf("content_score=1.5 should clamp to base half-life, got %.0f", gotOver)
	}
	gotUnder := quality.ContentScaleHalfLife(baseHalfLife, -0.5)
	if gotUnder != minHalfLife {
		return fmt.Errorf("content_score=-0.5 should clamp to min half-life, got %.0f", gotUnder)
	}

	if *verbose {
		fmt.Printf("\n    score=0.0→%.0fh  score=0.5→%.0fh  score=1.0→%.0fh (base=%.0fh min=%.0fh)\n",
			quality.ContentScaleHalfLife(baseHalfLife, 0.0)/float64(time.Hour),
			quality.ContentScaleHalfLife(baseHalfLife, 0.5)/float64(time.Hour),
			quality.ContentScaleHalfLife(baseHalfLife, 1.0)/float64(time.Hour),
			baseHalfLife/float64(time.Hour),
			minHalfLife/float64(time.Hour))
	}
	return nil
}

// scenarioLearningModeThreshold verifies the quality Tracker's learning mode:
// the system should report IsLearning=true until the configured threshold of
// retrieval events has been accumulated, then flip to false.
func scenarioLearningModeThreshold(ctx context.Context) error {
	st := newMemStore()
	const threshold = 50
	tracker := quality.NewTracker(st, threshold)

	// Before any events: should be in learning mode.
	if !tracker.IsLearning(ctx) {
		return fmt.Errorf("expected learning mode with 0 events (threshold=%d)", threshold)
	}

	// Build a set of dummy memories to pass to RecordHits.
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	emb := newHashEmbedder(128)
	memories := make([]store.Memory, 50)
	for i := range memories {
		content := fmt.Sprintf("unique memory content number %d for learning mode threshold test with distinct phrasing", i)
		vec, _ := emb.Embed(ctx, content)
		memories[i] = store.Memory{
			ID:        primitive.NewObjectID(),
			Content:   content,
			Embedding: vec,
			Source:    "validate",
			CreatedAt: baseTime,
		}
	}

	// Record exactly threshold events in one call.
	tracker.RecordHits(ctx, memories)

	// After threshold events: should exit learning mode.
	if tracker.IsLearning(ctx) {
		count := tracker.EventCount(ctx)
		return fmt.Errorf("expected learning mode off after %d events (threshold=%d), still on (count=%d)",
			threshold, threshold, count)
	}

	if *verbose {
		fmt.Printf("\n    threshold=%d events_recorded=%d is_learning=%v\n",
			threshold, tracker.EventCount(ctx), tracker.IsLearning(ctx))
	}
	return nil
}

// scenarioTrackerHitCounting verifies that RecordHits increments the event
// counter correctly, and that EventCount reflects actual stored events.
func scenarioTrackerHitCounting(ctx context.Context) error {
	st := newMemStore()
	tracker := quality.NewTracker(st, 1000) // high threshold so learning mode stays on
	emb := newHashEmbedder(128)
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if tracker.EventCount(ctx) != 0 {
		return fmt.Errorf("expected 0 events initially, got %d", tracker.EventCount(ctx))
	}

	// Record 3 hits.
	batch1 := make([]store.Memory, 3)
	for i := range batch1 {
		vec, _ := emb.Embed(ctx, fmt.Sprintf("memory for hit counting test batch one item %d", i))
		batch1[i] = store.Memory{ID: primitive.NewObjectID(), Embedding: vec, CreatedAt: baseTime}
	}
	tracker.RecordHits(ctx, batch1)
	if tracker.EventCount(ctx) != 3 {
		return fmt.Errorf("expected 3 events after first batch, got %d", tracker.EventCount(ctx))
	}

	// Record 7 more.
	batch2 := make([]store.Memory, 7)
	for i := range batch2 {
		vec, _ := emb.Embed(ctx, fmt.Sprintf("memory for hit counting test batch two item %d", i))
		batch2[i] = store.Memory{ID: primitive.NewObjectID(), Embedding: vec, CreatedAt: baseTime}
	}
	tracker.RecordHits(ctx, batch2)
	if tracker.EventCount(ctx) != 10 {
		return fmt.Errorf("expected 10 events after second batch, got %d", tracker.EventCount(ctx))
	}

	if *verbose {
		fmt.Printf("\n    batch1=3 batch2=7 → total_events=%d\n", tracker.EventCount(ctx))
	}
	return nil
}
