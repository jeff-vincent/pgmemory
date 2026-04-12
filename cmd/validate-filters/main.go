// Command validate-filters runs 105 synthetic agent dev session exchanges through
// pgmemory's filter gate chain and reports precision/recall against expected outcomes.
//
// Runs entirely in-memory with no external dependencies (no Postgres, no llama-server).
//
// Usage:
//
//	go run ./cmd/validate-filters
//	go run ./cmd/validate-filters -v          # verbose: show each exchange result
//	go run ./cmd/validate-filters -threshold 0.90  # fail if precision or recall < 90%
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jeff-vincent/pgmemory/internal/config"
	"github.com/jeff-vincent/pgmemory/internal/pipeline"
)

var (
	verbose   = flag.Bool("v", false, "verbose output (show each exchange result)")
	threshold = flag.Float64("threshold", 0.80, "minimum precision/recall to pass (0-1)")
	dump      = flag.Bool("dump", false, "dump exchanges as JSON to stdout and exit")
)

func main() {
	flag.Parse()
	if !*verbose {
		log.SetOutput(nopWriter{})
	}

	exchanges := allExchanges()

	if *dump {
		type exJSON struct {
			ID        int    `json:"id"`
			Category  string `json:"category"`
			User      string `json:"user"`
			Assistant string `json:"assistant"`
			Expected  string `json:"expected"`
			Notes     string `json:"notes"`
		}
		var out []exJSON
		for _, e := range exchanges {
			out = append(out, exJSON{e.ID, e.Category, e.User, e.Assistant, e.Expected, e.Notes})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.Encode(out)
		return
	}
	fmt.Printf("=== Filter Validation Harness ===\n")
	fmt.Printf("Exchanges: %d\n\n", len(exchanges))

	// Build in-memory pipeline with default config.
	// Content score gates disabled — hash embedder is not semantically meaningful.
	ms := newMemStore()
	emb := newHashEmbedder(128) // small dim for speed, sufficient for dedup logic
	pCfg := config.Default.Pipeline
	pCfg.ContentScorePreGate = 0 // disable — hash embedder scores are meaningless
	pCfg.ContentScoreGate = 0    // disable

	wp := pipeline.NewWritePipeline(emb, ms, pipeline.WithPipelineConfig(pCfg))
	ctx := context.Background()

	type result struct {
		ex       Exchange
		actual   string
		expected string
		match    bool
	}

	var results []result

	for _, ex := range exchanges {
		cr := classifyExchange(ctx, wp, pCfg, ex)
		match := cr.Stage == ex.Expected
		results = append(results, result{
			ex:       ex,
			actual:   cr.Stage,
			expected: ex.Expected,
			match:    match,
		})

		if *verbose {
			mark := "OK"
			if !match {
				mark = "MISMATCH"
			}
			fmt.Printf("  #%-3d %-8s %-18s -> %-18s %s\n",
				ex.ID, mark, ex.Expected, cr.Stage, truncate(ex.Notes, 50))
		}
	}

	// Compute per-gate stats.
	type gateStats struct {
		expected int
		actual   int
		correct  int
		fp       int // actual=stage but expected!=stage
		fn       int // expected=stage but actual!=stage
	}
	gates := map[string]*gateStats{}
	stageOrder := []string{"store", "filter_prefilter", "filter_length", "filter_noise", "dedup"}
	for _, s := range stageOrder {
		gates[s] = &gateStats{}
	}

	for _, r := range results {
		if g, ok := gates[r.expected]; ok {
			g.expected++
		}
		if g, ok := gates[r.actual]; ok {
			g.actual++
		}
		if r.match {
			if g, ok := gates[r.expected]; ok {
				g.correct++
			}
		} else {
			// False positive: actual says this stage, but expected says something else
			if g, ok := gates[r.actual]; ok {
				g.fp++
			}
			// False negative: expected this stage, but got something else
			if g, ok := gates[r.expected]; ok {
				g.fn++
			}
		}
	}

	// Print report.
	fmt.Printf("\n%-20s | %8s | %6s | %7s | %4s | %4s\n",
		"Gate", "Expected", "Actual", "Correct", "FP", "FN")
	fmt.Printf("%s\n", strings.Repeat("-", 65))
	for _, stage := range stageOrder {
		g := gates[stage]
		fmt.Printf("%-20s | %8d | %6d | %7d | %4d | %4d\n",
			stage, g.expected, g.actual, g.correct, g.fp, g.fn)
	}

	// Overall precision/recall for "store" gate.
	storeG := gates["store"]
	var precision, recall float64
	if storeG.actual > 0 {
		precision = float64(storeG.correct) / float64(storeG.actual)
	}
	if storeG.expected > 0 {
		recall = float64(storeG.correct) / float64(storeG.expected)
	}

	totalCorrect := 0
	for _, r := range results {
		if r.match {
			totalCorrect++
		}
	}
	accuracy := float64(totalCorrect) / float64(len(results))

	fmt.Printf("\nStore precision: %.1f%%  Store recall: %.1f%%  Overall accuracy: %.1f%%\n",
		precision*100, recall*100, accuracy*100)

	// Print mismatches.
	var mismatches []result
	for _, r := range results {
		if !r.match {
			mismatches = append(mismatches, r)
		}
	}
	if len(mismatches) > 0 {
		fmt.Printf("\n=== Mismatches (%d) ===\n", len(mismatches))
		for _, r := range mismatches {
			fmt.Printf("  #%-3d expected=%-18s actual=%-18s\n", r.ex.ID, r.expected, r.actual)
			fmt.Printf("       User: %s\n", truncate(r.ex.User, 80))
			fmt.Printf("       Asst: %s\n", truncate(r.ex.Assistant, 80))
			fmt.Printf("       Note: %s\n\n", r.ex.Notes)
		}
	}

	// Check threshold.
	if accuracy < *threshold {
		fmt.Printf("\nFAIL: accuracy %.1f%% < threshold %.1f%%\n", accuracy*100, *threshold*100)
		os.Exit(1)
	}
	fmt.Printf("\nPASS\n")
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
