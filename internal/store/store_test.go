package store

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestMemory_Fields(t *testing.T) {
	now := time.Now()
	m := Memory{
		ID:        primitive.NewObjectID(),
		Content:   "test content",
		Embedding: []float32{0.1, 0.2, 0.3},
		Source:    "test",
		CreatedAt: now,
		Metadata:  map[string]any{"codebase": "ember"},
		Score:     0.95,
	}

	if m.Content != "test content" {
		t.Errorf("Content = %q, want test content", m.Content)
	}
	if m.Source != "test" {
		t.Errorf("Source = %q, want test", m.Source)
	}
	if len(m.Embedding) != 3 {
		t.Errorf("Embedding length = %d, want 3", len(m.Embedding))
	}
	if m.Score != 0.95 {
		t.Errorf("Score = %f, want 0.95", m.Score)
	}
	if m.Metadata["codebase"] != "ember" {
		t.Errorf("Metadata[codebase] = %v, want ember", m.Metadata["codebase"])
	}
	if m.CreatedAt != now {
		t.Errorf("CreatedAt mismatch")
	}
	if m.ID.IsZero() {
		t.Error("ID should not be zero")
	}
}

func TestMemory_ZeroValue(t *testing.T) {
	var m Memory
	if m.Content != "" {
		t.Error("zero value Content should be empty")
	}
	if m.Embedding != nil {
		t.Error("zero value Embedding should be nil")
	}
	if m.ID != primitive.NilObjectID {
		t.Error("zero value ID should be nil ObjectID")
	}
}
