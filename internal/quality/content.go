package quality

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/memory-daemon/memoryd/internal/embedding"
)

// DefaultQualityProtos are generic descriptions of high-value knowledge chunks.
// They are embedded once at startup and used to score incoming chunks by
// cosine similarity — no domain-specific rules needed.
var DefaultQualityProtos = []string{
	"important technical decision with reasoning and rationale",
	"architecture pattern approach and implementation details",
	"debugging solution root cause analysis and fix",
	"configuration setup deployment and environment instructions",
	"code pattern best practice convention and design",
	"error message workaround resolution and explanation",
}

// DefaultNoiseProtos are generic descriptions of low-signal content.
var DefaultNoiseProtos = []string{
	"greeting acknowledgment helpful response sure happy",
	"let me know if you need anything else feel free",
	"i will help you with that certainly of course",
}

// ContentScorer scores chunks by their semantic proximity to high-value
// vs. low-value knowledge, using only the embedding model already in use.
// It is safe for concurrent use after construction.
type ContentScorer struct {
	qualityVecs [][]float32
	noiseVecs   [][]float32
}

// NewContentScorer embeds the default quality and noise prototypes. Should be
// called once during daemon startup. On error, returns nil — callers should
// treat a nil scorer as "scoring disabled" and continue without content scoring.
func NewContentScorer(ctx context.Context, emb embedding.Embedder) (*ContentScorer, error) {
	return NewContentScorerWithProtos(ctx, emb, nil, nil)
}

// NewContentScorerWithProtos embeds custom prototype sets. Empty slices fall
// back to the built-in defaults. Use this when the user has configured custom
// prototypes via the dashboard.
func NewContentScorerWithProtos(ctx context.Context, emb embedding.Embedder, qualityProtos, noiseProtos []string) (*ContentScorer, error) {
	if len(qualityProtos) == 0 {
		qualityProtos = DefaultQualityProtos
	}
	if len(noiseProtos) == 0 {
		noiseProtos = DefaultNoiseProtos
	}
	qualityVecs, err := emb.EmbedBatch(ctx, qualityProtos)
	if err != nil {
		return nil, err
	}
	noiseVecs, err := emb.EmbedBatch(ctx, noiseProtos)
	if err != nil {
		return nil, err
	}
	return &ContentScorer{qualityVecs: qualityVecs, noiseVecs: noiseVecs}, nil
}

// NewContentScorerFromRejections builds a scorer whose noise prototypes are
// the actual assistant texts accumulated in the rejection store, rather than
// the static defaults. When rejectionTexts is empty it falls back to defaults.
// This is the primary path for adaptive noise learning.
//
// To avoid slow EmbedBatch calls and keep the noise signal focused, only the
// most recent maxRejectionProtos texts are used.
const maxRejectionProtos = 150

func NewContentScorerFromRejections(ctx context.Context, emb embedding.Embedder, rejectionTexts, qualityProtos []string) (*ContentScorer, error) {
	if len(qualityProtos) == 0 {
		qualityProtos = DefaultQualityProtos
	}
	noiseProtos := rejectionTexts
	if len(noiseProtos) == 0 {
		noiseProtos = DefaultNoiseProtos
	}
	// Cap to the most recent entries to keep embedding fast and noise focused.
	if len(noiseProtos) > maxRejectionProtos {
		noiseProtos = noiseProtos[len(noiseProtos)-maxRejectionProtos:]
	}
	return NewContentScorerWithProtos(ctx, emb, qualityProtos, noiseProtos)
}

// Score returns a content quality score in [0.0, 1.0] for the given embedding
// vector. A score near 1.0 means the chunk is semantically close to high-value
// knowledge prototypes; near 0.0 means it resembles noise.
//
// Uses ratio normalization: score = avgQualitySim / (avgQualitySim + topKNoiseSim)
// so the result is always in (0, 1) and independent of absolute similarity magnitudes.
//
// The noise component uses the top-K most similar noise prototypes rather than
// averaging all of them. This prevents dilution when the rejection store
// accumulates hundreds of diverse noise examples — avgNoise would converge to
// a constant, destroying discriminative power.
const noiseTopK = 3

func (cs *ContentScorer) Score(vec []float32) float64 {
	if cs == nil || len(vec) == 0 {
		return 0.5 // neutral default when scorer unavailable
	}

	var qualitySum float64
	for _, q := range cs.qualityVecs {
		qualitySum += cosineSim(vec, q)
	}
	avgQuality := qualitySum / float64(len(cs.qualityVecs))

	// Top-K noise: use the K most similar noise prototypes.
	noiseSims := make([]float64, len(cs.noiseVecs))
	for i, n := range cs.noiseVecs {
		noiseSims[i] = cosineSim(vec, n)
	}
	sort.Float64s(noiseSims)

	k := noiseTopK
	if k > len(noiseSims) {
		k = len(noiseSims)
	}
	var topNoiseSum float64
	for i := len(noiseSims) - k; i < len(noiseSims); i++ {
		topNoiseSum += noiseSims[i]
	}
	avgNoise := topNoiseSum / float64(k)

	denom := avgQuality + avgNoise
	if denom <= 0 {
		return 0.5
	}
	score := avgQuality / denom
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

// ContentScaleHalfLife returns the effective decay half-life for a chunk
// based on its content score. High-quality chunks keep the full configured
// half-life; low-quality chunks get a shorter one, falling below the prune
// threshold much sooner.
//
// The minimum effective half-life is 7 days, regardless of config.
// At content_score=0 and a 90-day base: ~7-day half-life → pruned in ~33 days.
// At content_score=1 and a 90-day base: full 90-day half-life.
func ContentScaleHalfLife(halfLife float64, contentScore float64) float64 {
	if contentScore < 0 {
		contentScore = 0
	}
	if contentScore > 1 {
		contentScore = 1
	}
	minHalfLife := float64(7 * 24 * time.Hour)
	if halfLife <= minHalfLife {
		return halfLife
	}
	return minHalfLife + contentScore*(halfLife-minHalfLife)
}

func cosineSim(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
