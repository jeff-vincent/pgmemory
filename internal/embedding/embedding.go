package embedding

import "context"

// Embedder produces vector embeddings from text.
type Embedder interface {
	// Embed returns a float32 vector for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch returns vectors for multiple texts in a single call.
	// Falls back to sequential Embed calls if the backend doesn't support batching.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dim returns the embedding dimension.
	Dim() int

	// Close releases resources (stops subprocess, etc.).
	Close() error
}
