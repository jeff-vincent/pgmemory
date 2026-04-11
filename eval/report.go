package eval

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
)

// Report writes a human-readable comparison report.
func Report(w io.Writer, results []ScenarioResult) {
	fmt.Fprintf(w, "\n%s\n", strings.Repeat("=", 72))
	fmt.Fprintf(w, "  memoryd eval — bare Claude vs Claude + memoryd\n")
	fmt.Fprintf(w, "%s\n\n", strings.Repeat("=", 72))

	var totalBare, totalAug, totalCriteria int

	for _, r := range results {
		fmt.Fprintf(w, "--- %s ---\n\n", r.Scenario)

		for _, s := range r.Scores {
			delta := s.AugScore - s.BareScore
			marker := " "
			if delta > 0 {
				marker = "+"
			} else if delta < 0 {
				marker = "-"
			}
			fmt.Fprintf(w, "  [%s] %-60s  bare=%d  aug=%d\n", marker, s.Criterion, s.BareScore, s.AugScore)
			fmt.Fprintf(w, "      %s\n", s.Explanation)
		}

		if len(r.Augmented.RetrievalScores) > 0 {
			min, max, avg := scoreStats(r.Augmented.RetrievalScores)
			fmt.Fprintf(w, "\n  retrieval: n=%d  min=%.3f  max=%.3f  avg=%.3f",
				len(r.Augmented.RetrievalScores), min, max, avg)
			if r.Delta > 0 {
				fmt.Fprintf(w, "  → helped")
			} else if r.Delta < 0 {
				fmt.Fprintf(w, "  → hurt")
			} else {
				fmt.Fprintf(w, "  → neutral")
			}
			fmt.Fprintf(w, "\n")
		} else {
			fmt.Fprintf(w, "\n  retrieval: no scores recorded\n")
		}

		fmt.Fprintf(w, "\n  TOTAL: bare=%d  aug=%d  delta=%+d\n\n", r.BareTotal, r.AugTotal, r.Delta)

		totalBare += r.BareTotal
		totalAug += r.AugTotal
		totalCriteria += len(r.Scores)
	}

	fmt.Fprintf(w, "%s\n", strings.Repeat("=", 72))
	fmt.Fprintf(w, "  AGGREGATE (%d scenarios, %d criteria)\n", len(results), totalCriteria)
	fmt.Fprintf(w, "    Bare total:      %d\n", totalBare)
	fmt.Fprintf(w, "    Augmented total: %d\n", totalAug)
	fmt.Fprintf(w, "    Delta:           %+d\n", totalAug-totalBare)
	if totalBare > 0 {
		pct := float64(totalAug-totalBare) / float64(totalBare) * 100
		fmt.Fprintf(w, "    Improvement:     %.1f%%\n", pct)
	}

	// Score-vs-delta breakdown: bucket by avg retrieval score, show avg delta per bucket.
	fmt.Fprintf(w, "\n  Retrieval score vs quality delta:\n")
	type bucket struct {
		count    int
		deltaSum int
	}
	buckets := map[string]*bucket{
		"<0.50": {},
		"0.50-0.60": {},
		"0.60-0.70": {},
		"0.70-0.80": {},
		">=0.80": {},
	}
	bucketOrder := []string{"<0.50", "0.50-0.60", "0.60-0.70", "0.70-0.80", ">=0.80"}
	for _, r := range results {
		if len(r.Augmented.RetrievalScores) == 0 {
			continue
		}
		_, _, avg := scoreStats(r.Augmented.RetrievalScores)
		var key string
		switch {
		case avg < 0.50:
			key = "<0.50"
		case avg < 0.60:
			key = "0.50-0.60"
		case avg < 0.70:
			key = "0.60-0.70"
		case avg < 0.80:
			key = "0.70-0.80"
		default:
			key = ">=0.80"
		}
		buckets[key].count++
		buckets[key].deltaSum += r.Delta
	}
	for _, k := range bucketOrder {
		b := buckets[k]
		if b.count == 0 {
			continue
		}
		avgDelta := float64(b.deltaSum) / float64(b.count)
		fmt.Fprintf(w, "    avg score %-12s  n=%d  avg delta=%+.2f\n", k, b.count, avgDelta)
	}

	fmt.Fprintf(w, "%s\n\n", strings.Repeat("=", 72))
}

func scoreStats(scores []float64) (min, max, avg float64) {
	min = math.MaxFloat64
	max = -math.MaxFloat64
	var sum float64
	for _, s := range scores {
		if s < min {
			min = s
		}
		if s > max {
			max = s
		}
		sum += s
	}
	avg = sum / float64(len(scores))
	return
}

// ReportJSON writes machine-readable JSON output.
func ReportJSON(w io.Writer, results []ScenarioResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

// LoadScenarios reads scenarios from JSON.
func LoadScenarios(data []byte) ([]Scenario, error) {
	var scenarios []Scenario
	if err := json.Unmarshal(data, &scenarios); err != nil {
		return nil, err
	}
	return scenarios, nil
}
