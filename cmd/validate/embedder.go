package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
)

// hashEmbedder produces deterministic embeddings from text content using
// SHA-256 hashing. Similar texts produce somewhat similar vectors because
// we hash overlapping character n-grams and accumulate into buckets.
// This is not a real embedding model — it's fast, deterministic, and
// good enough for validating the pipeline's dedup/merge/prune logic.
type hashEmbedder struct {
	dim   int
	ngram int // character n-gram size for shingling
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

// embed creates a vector by hashing character n-grams and accumulating
// into dimension buckets. The result is L2-normalized so cosine similarity
// works correctly. Texts sharing many n-grams will have higher similarity.
func (e *hashEmbedder) embed(text string) []float32 {
	vec := make([]float32, e.dim)

	runes := []rune(text)
	if len(runes) < e.ngram {
		// Short text: hash the whole thing.
		h := sha256.Sum256([]byte(text))
		for i := 0; i < e.dim; i++ {
			vec[i] = float32(h[i%32]) / 128.0
		}
		normalize(vec)
		return vec
	}

	// Shingle: hash each n-gram and scatter into the vector.
	for i := 0; i <= len(runes)-e.ngram; i++ {
		gram := string(runes[i : i+e.ngram])
		h := sha256.Sum256([]byte(gram))
		bucket := int(binary.LittleEndian.Uint32(h[:4])) % e.dim
		// Use bytes 4-7 for sign/magnitude.
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
