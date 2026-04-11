package store

import "math"

// reciprocalRankFusion merges two ranked result lists using Reciprocal Rank
// Fusion (RRF) with smoothing constant k.
func reciprocalRankFusion(listA, listB []Memory, k int) []Memory {
	type scored struct {
		mem   Memory
		score float64
	}

	byID := map[string]*scored{}

	for rank, m := range listA {
		id := m.ID.Hex()
		s := 1.0 / float64(rank+k+1)
		if existing, ok := byID[id]; ok {
			existing.score += s
		} else {
			byID[id] = &scored{mem: m, score: s}
		}
	}

	for rank, m := range listB {
		id := m.ID.Hex()
		s := 1.0 / float64(rank+k+1)
		if existing, ok := byID[id]; ok {
			existing.score += s
		} else {
			byID[id] = &scored{mem: m, score: s}
		}
	}

	// Sort by RRF score descending.
	results := make([]Memory, 0, len(byID))
	scores := make([]float64, 0, len(byID))
	for _, s := range byID {
		results = append(results, s.mem)
		scores = append(scores, s.score)
	}

	// Simple insertion sort (lists are small, typically < 100).
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && scores[j] > scores[j-1]; j-- {
			results[j], results[j-1] = results[j-1], results[j]
			scores[j], scores[j-1] = scores[j-1], scores[j]
		}
	}

	// Assign fused score to the Score field for downstream use.
	for i := range results {
		results[i].Score = scores[i]
	}

	return results
}

// mmrRerank applies Maximal Marginal Relevance to select diverse results.
// lambda controls relevance (1.0) vs diversity (0.0).
func mmrRerank(candidates []Memory, queryVec []float32, topK int, lambda float64) []Memory {
	if len(candidates) <= topK {
		return candidates
	}

	selected := make([]Memory, 0, topK)
	remaining := make([]int, len(candidates)) // indices into candidates
	for i := range remaining {
		remaining[i] = i
	}

	// Pre-compute query similarities.
	querySims := make([]float64, len(candidates))
	for i, c := range candidates {
		if len(c.Embedding) > 0 {
			querySims[i] = cosineSim(queryVec, c.Embedding)
		} else {
			querySims[i] = c.Score // fallback to search score
		}
	}

	for len(selected) < topK && len(remaining) > 0 {
		bestIdx := -1
		bestMMR := math.Inf(-1)

		for ri, ci := range remaining {
			relevance := querySims[ci]

			// Max similarity to any already-selected memory.
			var maxSim float64
			for _, sel := range selected {
				if len(candidates[ci].Embedding) > 0 && len(sel.Embedding) > 0 {
					sim := cosineSim(candidates[ci].Embedding, sel.Embedding)
					if sim > maxSim {
						maxSim = sim
					}
				}
			}

			mmr := lambda*relevance - (1-lambda)*maxSim
			if mmr > bestMMR {
				bestMMR = mmr
				bestIdx = ri
			}
		}

		if bestIdx < 0 {
			break
		}

		ci := remaining[bestIdx]
		selected = append(selected, candidates[ci])
		// Remove from remaining (swap with last).
		remaining[bestIdx] = remaining[len(remaining)-1]
		remaining = remaining[:len(remaining)-1]
	}

	return selected
}

// cosineSim computes cosine similarity between two vectors.
func cosineSim(a []float32, b []float32) float64 {
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
