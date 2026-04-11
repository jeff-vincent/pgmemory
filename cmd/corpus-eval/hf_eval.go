// hf_eval.go
//
// HuggingFace-dataset-based pipeline optimization for memoryd.
//
// Downloads a sample from nlile/misc-merged-claude-code-traces-v1 and
// classifies assistant responses into three buckets:
//
//   noise       – empty / too short / high-symbol density (noise filter catches these)
//   explanation – "explanation of findings" to the user: long enough to pass the noise
//                 filter, phrased similarly, but carries no durable knowledge
//   substantive – specific technical content worth storing
//
// Phase 1 (local, no memoryd required): shows classification distribution and
// representative explanation samples.
//
// Phase 2 (requires memoryd): sweeps content_score_gate × noise_proto configurations,
// stores balanced samples, measures filter rates, and recommends the optimal setting.
//
// Entry point: runHFEval()

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// ---------------------------------------------------------------------------
// HuggingFace datasets-server API
// ---------------------------------------------------------------------------

const hfRowsEndpoint = "https://datasets-server.huggingface.co/rows"
const hfDataset = "nlile/misc-merged-claude-code-traces-v1"

type hfRow struct {
	AssistantResponse string `json:"assistant_response"`
	UserPrompt        string `json:"user_prompt"`
}

type hfAPIResp struct {
	Rows []struct {
		Row hfRow `json:"row"`
	} `json:"rows"`
}

func fetchHFRows(n int) ([]hfRow, error) {
	var rows []hfRow
	batchSize := 100
	hfClient := &http.Client{Timeout: 30 * time.Second}
	for offset := 0; offset < n; offset += batchSize {
		limit := batchSize
		if offset+limit > n {
			limit = n - offset
		}
		url := fmt.Sprintf("%s?dataset=%s&config=default&split=train&offset=%d&length=%d",
			hfRowsEndpoint, hfDataset, offset, limit)
		resp, err := hfClient.Get(url)
		if err != nil {
			return nil, fmt.Errorf("fetching HF rows at offset %d: %w", offset, err)
		}
		var apiResp hfAPIResp
		err = json.NewDecoder(resp.Body).Decode(&apiResp)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decoding HF response: %w", err)
		}
		for _, r := range apiResp.Rows {
			rows = append(rows, r.Row)
		}
		if len(apiResp.Rows) < limit {
			break // end of dataset
		}
		time.Sleep(100 * time.Millisecond) // be polite to the API
	}
	return rows, nil
}

// ---------------------------------------------------------------------------
// Classification
// ---------------------------------------------------------------------------

type respClass int

const (
	rcNoise       respClass = iota // should be filtered — empty, short, high-symbol
	rcExplanation                  // explanation-of-findings: passes noise gate but is generic
	rcSubstantive                  // specific technical content worth storing
)

func (rc respClass) String() string {
	switch rc {
	case rcNoise:
		return "noise"
	case rcExplanation:
		return "explanation"
	default:
		return "substantive"
	}
}

// explanationPrefixes are the leading phrases that characterise
// "explanation of findings" responses.
var explanationPrefixes = []string{
	"i've ", "i have ", "i can see", "i can confirm", "i notice",
	"looking at ", "based on ", "after reviewing", "after looking",
	"i see that", "i found that", "i found the", "i checked",
	"the issue is", "the problem is", "the error is",
	"it appears", "it seems", "it looks like",
	"i've made", "i've updated", "i've added", "i've created",
	"i've fixed", "i've changed", "i've modified", "i've completed",
	"i've reviewed", "i've checked", "i've run", "i've tested",
	"the fix is", "the change is", "the solution is",
}

// hasSpecificTechnicalContent returns true when text contains identifiers or
// structures that make it worth storing (code blocks, CamelCase, file paths,
// error messages, numeric specifics attached to identifiers).
func hasSpecificTechnicalContent(text string) bool {
	if strings.Contains(text, "```") {
		return true
	}
	// Function/method call pattern
	if strings.Count(text, "(") > 2 && strings.Count(text, ")") > 2 {
		return true
	}
	// File path pattern
	if strings.Count(text, "/") > 2 {
		return true
	}
	// Error messages
	if strings.Contains(text, "Error:") || strings.Contains(text, "error:") ||
		strings.Contains(text, "Exception") || strings.Contains(text, "panic:") {
		return true
	}
	// CamelCase or snake_case identifiers — signs of code references
	words := strings.Fields(text)
	for _, w := range words {
		w = strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		if len(w) < 4 {
			continue
		}
		// CamelCase: has uppercase letter after position 0
		for i := 1; i < len(w); i++ {
			if w[i] >= 'A' && w[i] <= 'Z' {
				return true
			}
		}
		// snake_case with multiple segments
		if strings.Count(w, "_") > 1 {
			return true
		}
	}
	return false
}

func alnumRatio(text string) float64 {
	if len(text) == 0 {
		return 0
	}
	n := 0
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			n++
		}
	}
	return float64(n) / float64(len(text))
}

func classifyResponse(text string) respClass {
	text = strings.TrimSpace(text)

	// Noise: empty or trivially short
	if len(text) < 20 {
		return rcNoise
	}
	// Noise: high symbol density
	if alnumRatio(text) < 0.40 {
		return rcNoise
	}

	// Check for explanation-of-findings prefix in the first 140 chars
	sample := strings.ToLower(text)
	if len(sample) > 140 {
		sample = sample[:140]
	}
	isExplanationLead := false
	for _, pfx := range explanationPrefixes {
		if strings.HasPrefix(sample, pfx) {
			isExplanationLead = true
			break
		}
	}

	if isExplanationLead {
		// Even with an explanation lead, if the body has specific technical
		// content it's still worth storing.
		if hasSpecificTechnicalContent(text) {
			return rcSubstantive
		}
		return rcExplanation
	}

	// Anything else that's long enough with decent density is substantive.
	if len(text) >= 60 && alnumRatio(text) >= 0.50 {
		return rcSubstantive
	}
	return rcNoise
}

// ---------------------------------------------------------------------------
// Candidate noise protos for "explanation of findings"
// These are short semantic descriptions — the content scorer embeds them and
// compares incoming chunks by cosine similarity.
// ---------------------------------------------------------------------------

var explanationNoiseProtos = []string{
	"I've reviewed the code and made the changes to fix the issue",
	"after looking at the file I found and resolved the problem",
	"I can see the implementation is working correctly now",
	"I've completed the task the changes are in place as requested",
	"I've updated the configuration and it should work as expected",
	"based on my analysis the issue appears to be in the function",
	"I've made the modifications to the file and verified the fix",
	"looking at the output the error has been resolved successfully",
}

// ---------------------------------------------------------------------------
// Auth-aware API helpers (reads token from ~/.memoryd/token)
// ---------------------------------------------------------------------------

func loadToken() string {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".memoryd", "token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func authPost(baseURL, path string, body any, out any, token string) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, _ := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, data)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func authGet(baseURL, path string, out any, token string) error {
	req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, data)
	}
	return json.Unmarshal(data, out)
}

func authDelete(baseURL, path string, token string) error {
	req, _ := http.NewRequest(http.MethodDelete, baseURL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ---------------------------------------------------------------------------
// Pipeline config management
// ---------------------------------------------------------------------------

type pipelineCfg struct {
	NoiseMinLen        int      `json:"noise_min_len"`
	NoiseMinAlnumRatio float64  `json:"noise_min_alnum_ratio"`
	ContentScoreGate   float64  `json:"content_score_gate"`
	DedupThreshold     float64  `json:"dedup_threshold"`
	TopicBoundary      float64  `json:"topic_boundary_threshold"`
	MaxGroupChars      int      `json:"max_group_chars"`
	QualityProtos      []string `json:"quality_protos"`
	NoiseProtos        []string `json:"noise_protos"`
}

type pipelineGetResp struct {
	Pipeline pipelineCfg `json:"pipeline"`
}

func getPipelineConfig(baseURL, token string) (pipelineCfg, error) {
	var resp pipelineGetResp
	err := authGet(baseURL, "/api/pipeline", &resp, token)
	return resp.Pipeline, err
}

func setPipelineConfig(baseURL, token string, cfg pipelineCfg) error {
	return authPost(baseURL, "/api/pipeline", map[string]any{"pipeline": cfg}, nil, token)
}

// ---------------------------------------------------------------------------
// Sweep configuration
// ---------------------------------------------------------------------------

type sweepCfg struct {
	name       string
	minLen     int     // noise_min_len override (0 = keep original)
	alnumRatio float64 // noise_min_alnum_ratio override (0 = keep original)
	gate       float64 // content_score_gate
	addProtos  bool    // whether to add explanationNoiseProtos to noise_protos
}

// Three-phase sweep:
//   Phase 1: noise_min_len (gate=0, alnum=default) — find length bottleneck
//   Phase 2: noise_min_alnum_ratio (min_len=40, gate=0) — find alnum bottleneck
//   Phase 3: content_score_gate × protos (min_len=40, alnum=0.25) — tune quality filter
var sweepCfgs = []sweepCfg{
	// --- Phase 1: noise_min_len sweep ---
	{"min_len=20   alnum=0.40  gate=0", 20, 0.40, 0.00, false},
	{"min_len=40   alnum=0.40  gate=0", 40, 0.40, 0.00, false},
	{"min_len=60   alnum=0.40  gate=0", 60, 0.40, 0.00, false},
	{"min_len=80   alnum=0.40  gate=0", 80, 0.40, 0.00, false},
	{"min_len=120  alnum=0.40  gate=0", 120, 0.40, 0.00, false},
	// --- Phase 2: alnum_ratio sweep (min_len=40 fixed) ---
	{"min_len=40   alnum=0.20  gate=0", 40, 0.20, 0.00, false},
	{"min_len=40   alnum=0.25  gate=0", 40, 0.25, 0.00, false},
	{"min_len=40   alnum=0.30  gate=0", 40, 0.30, 0.00, false},
	{"min_len=40   alnum=0.35  gate=0", 40, 0.35, 0.00, false},
	// --- Phase 3: gate × protos (min_len=40, alnum=0.25) ---
	{"min_len=40   alnum=0.25  gate=0.10", 40, 0.25, 0.10, false},
	{"min_len=40   alnum=0.25  gate=0.15", 40, 0.25, 0.15, false},
	{"min_len=40   alnum=0.25  gate=0.20", 40, 0.25, 0.20, false},
	{"min_len=40   alnum=0.25  gate=0.15 +protos", 40, 0.25, 0.15, true},
	{"min_len=40   alnum=0.25  gate=0.20 +protos", 40, 0.25, 0.20, true},
}

// ---------------------------------------------------------------------------
// Per-entry store and measure
// ---------------------------------------------------------------------------

// storeEntry stores one entry and returns whether any chunk was persisted.
// stored=true  → at least one chunk reached the store (entry is "kept")
// stored=false → every chunk was filtered or deduped (entry is "fully filtered")
func storeEntry(baseURL, token, source, text string) (kept bool) {
	var sr storeResp
	err := authPost(baseURL, "/api/store", storeReq{Content: text, Source: source}, &sr, token)
	if err != nil {
		return true // network error — don't penalise config
	}
	s, ext, d, m, _ := parseSummary(sr.Summary)
	// "kept" means something made it into the store (stored, extended, deduped-as-merge, etc.)
	return s > 0 || ext > 0 || d > 0 || m > 0
}

func deleteBySource(baseURL, token, source string) int {
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/memories", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	var mems []struct {
		ID     string `json:"id"`
		Source string `json:"source"`
	}
	data, _ := io.ReadAll(resp.Body)
	json.Unmarshal(data, &mems)

	n := 0
	for _, m := range mems {
		if m.Source == source {
			authDelete(baseURL, "/api/memories/"+m.ID, token)
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Sweep result — all counts are per-entry, not per-chunk
// ---------------------------------------------------------------------------

type sweepResult struct {
	cfg             sweepCfg
	explainFiltered int // explanation entries where NOTHING was stored
	explainKept     int // explanation entries where at least one chunk was stored
	substFiltered   int // substantive entries where NOTHING was stored
	substKept       int // substantive entries where at least one chunk was stored
}

func (r sweepResult) explainFilterRate() float64 {
	total := r.explainFiltered + r.explainKept
	if total == 0 {
		return 0
	}
	return float64(r.explainFiltered) / float64(total)
}

func (r sweepResult) substFalsePositiveRate() float64 {
	total := r.substFiltered + r.substKept
	if total == 0 {
		return 0
	}
	return float64(r.substFiltered) / float64(total)
}

// score: maximize explanation catch, penalise substantive false positives.
func (r sweepResult) score() float64 {
	efr := r.explainFilterRate()
	sfpr := r.substFalsePositiveRate()
	return efr - 2*sfpr
}

// ---------------------------------------------------------------------------
// Main HF eval entry point
// ---------------------------------------------------------------------------

func runHFEval(w io.Writer, baseURL string, sampleSize int, noCleanup bool) {
	token := loadToken()

	fmt.Fprintf(os.Stderr, "=== memoryd HF pipeline optimiser ===\n")
	fmt.Fprintf(os.Stderr, "dataset:    %s\n", hfDataset)
	fmt.Fprintf(os.Stderr, "sample:     %d rows\n", sampleSize)
	fmt.Fprintf(os.Stderr, "target:     %s\n\n", baseURL)

	// ----------------------------------------------------------------
	// Phase 1 — local classification (no memoryd required)
	// ----------------------------------------------------------------

	fmt.Fprintln(os.Stderr, "[phase 1] fetching rows from HuggingFace...")
	rows, err := fetchHFRows(sampleSize)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error fetching HF rows: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "          fetched %d rows\n", len(rows))

	var noises, explanations, substantives []string
	for _, row := range rows {
		text := strings.TrimSpace(row.AssistantResponse)
		switch classifyResponse(text) {
		case rcNoise:
			noises = append(noises, text)
		case rcExplanation:
			explanations = append(explanations, text)
		case rcSubstantive:
			substantives = append(substantives, text)
		}
	}

	total := len(rows)
	fmt.Fprintf(os.Stderr, "          noise=%d (%.0f%%)  explanation=%d (%.0f%%)  substantive=%d (%.0f%%)\n\n",
		len(noises), pct(len(noises), total),
		len(explanations), pct(len(explanations), total),
		len(substantives), pct(len(substantives), total),
	)

	// ----------------------------------------------------------------
	// Phase 2 — live gate sweep (requires memoryd)
	// ----------------------------------------------------------------

	var results []sweepResult
	liveAvailable := false

	var healthCheck struct{ Status string }
	if err := authGet(baseURL, "/health", &healthCheck, ""); err == nil && healthCheck.Status == "ok" {
		liveAvailable = true
	}

	if liveAvailable {
		fmt.Fprintln(os.Stderr, "[phase 2] running live gate sweep against memoryd...")

		// Save current config so we can restore it
		origCfg, err := getPipelineConfig(baseURL, token)
		if err != nil {
			fmt.Fprintf(os.Stderr, "          warning: could not read pipeline config: %v\n", err)
		}
		fmt.Fprintf(os.Stderr, "          current pipeline: noise_min_len=%d  noise_min_alnum=%.2f  content_gate=%.2f\n",
			origCfg.NoiseMinLen, origCfg.NoiseMinAlnumRatio, origCfg.ContentScoreGate)

		// Diagnostic probe: test all 40 eval substantive entries at most-permissive
		// settings. Show entries that still produce nothing to understand the floor.
		if len(substantives) > 0 {
			evalSubstProbe := substantives
			if len(evalSubstProbe) > 40 {
				evalSubstProbe = evalSubstProbe[:40]
			}
			probeSrc := fmt.Sprintf("hf-probe-%d", time.Now().UnixNano())
			probeCfg := origCfg
			probeCfg.ContentScoreGate = 0
			probeCfg.NoiseMinLen = 20
			probeCfg.NoiseMinAlnumRatio = 0.10
			_ = setPipelineConfig(baseURL, token, probeCfg)
			time.Sleep(300 * time.Millisecond)
			probeKept, probeFiltered := 0, 0
			fmt.Fprintf(os.Stderr, "          probe (min_len=20 alnum=0.10 gate=0): testing %d entries\n", len(evalSubstProbe))
			for i, text := range evalSubstProbe {
				var probeResp storeResp
				_ = authPost(baseURL, "/api/store", storeReq{Content: text, Source: probeSrc}, &probeResp, token)
				kept := storeEntry(baseURL, token, probeSrc+"x", text) // re-use logic
				_ = kept
				s, ext, d, m, f := parseSummary(probeResp.Summary)
				if s > 0 || ext > 0 || d > 0 || m > 0 {
					probeKept++
				} else {
					probeFiltered++
					preview := strings.ReplaceAll(text, "\n", "\\n")
					if len(preview) > 80 {
						preview = preview[:80]
					}
					fmt.Fprintf(os.Stderr, "            FILTERED [%d] len=%-5d f=%-2d summary=%q\n", i+1, len(text), f, probeResp.Summary)
					fmt.Fprintf(os.Stderr, "                     %q\n", preview)
				}
			}
			fmt.Fprintf(os.Stderr, "          probe result: kept=%d filtered=%d\n", probeKept, probeFiltered)
			deleteBySource(baseURL, token, probeSrc)
			_ = setPipelineConfig(baseURL, token, origCfg)
			time.Sleep(300 * time.Millisecond)
		}

		// Build balanced sample: up to 40 of each class
		capAt := func(ss []string, n int) []string {
			if len(ss) > n {
				return ss[:n]
			}
			return ss
		}
		evalExplain := capAt(explanations, 40)
		evalSubst := capAt(substantives, 40)

		for i, sc := range sweepCfgs {
			fmt.Fprintf(os.Stderr, "          [%d/%d] %s\n", i+1, len(sweepCfgs), sc.name)

			// Build config for this sweep run
			cfg := origCfg
			cfg.ContentScoreGate = sc.gate
			if sc.minLen > 0 {
				cfg.NoiseMinLen = sc.minLen
			}
			if sc.alnumRatio > 0 {
				cfg.NoiseMinAlnumRatio = sc.alnumRatio
			}
			if sc.addProtos {
				cfg.NoiseProtos = append(origCfg.NoiseProtos, explanationNoiseProtos...)
			} else {
				cfg.NoiseProtos = origCfg.NoiseProtos
			}

			if err := setPipelineConfig(baseURL, token, cfg); err != nil {
				fmt.Fprintf(os.Stderr, "          config update failed: %v\n", err)
				continue
			}
			// Brief pause for scorer reload to settle
			time.Sleep(300 * time.Millisecond)

			runSource := fmt.Sprintf("hf-sweep-%d-%d", i, time.Now().UnixNano())
			res := sweepResult{cfg: sc}

			for _, text := range evalExplain {
				if storeEntry(baseURL, token, runSource, text) {
					res.explainKept++
				} else {
					res.explainFiltered++
				}
			}
			for _, text := range evalSubst {
				if storeEntry(baseURL, token, runSource, text) {
					res.substKept++
				} else {
					res.substFiltered++
				}
			}

			results = append(results, res)

			if !noCleanup {
				n := deleteBySource(baseURL, token, runSource)
				fmt.Fprintf(os.Stderr, "          cleaned up %d eval memories\n", n)
			}
		}

		// Restore original config
		if err := setPipelineConfig(baseURL, token, origCfg); err != nil {
			fmt.Fprintf(os.Stderr, "          warning: could not restore pipeline config: %v\n", err)
		} else {
			fmt.Fprintln(os.Stderr, "          pipeline config restored")
		}

	} else {
		fmt.Fprintln(os.Stderr, "[phase 2] skipped — memoryd not reachable at "+baseURL)
	}

	// ----------------------------------------------------------------
	// Report
	// ----------------------------------------------------------------

	writeHFReport(w, rows, noises, explanations, substantives, results, liveAvailable)
}

// ---------------------------------------------------------------------------
// Report
// ---------------------------------------------------------------------------

func writeHFReport(w io.Writer, rows []hfRow, noises, explanations, substantives []string,
	results []sweepResult, liveRan bool) {

	total := len(rows)
	now := time.Now().Format("2006-01-02 15:04:05")

	fmt.Fprintf(w, "# memoryd HF Pipeline Optimiser — %s\n\n", now)
	fmt.Fprintf(w, "**Dataset:** `%s`  \n", hfDataset)
	fmt.Fprintf(w, "**Sample rows:** %d  \n\n", total)

	// --- Distribution ---
	fmt.Fprintf(w, "## Classification Distribution\n\n")
	fmt.Fprintf(w, "| Class | Count | %% | Description |\n")
	fmt.Fprintf(w, "|-------|-------|-----|-------------|\n")
	fmt.Fprintf(w, "| noise | %d | %.0f%% | empty / too short / high-symbol — noise filter catches these |\n",
		len(noises), pct(len(noises), total))
	fmt.Fprintf(w, "| explanation | %d | %.0f%% | explanation-of-findings: passes noise gate, phrased similarly, low knowledge density |\n",
		len(explanations), pct(len(explanations), total))
	fmt.Fprintf(w, "| substantive | %d | %.0f%% | specific technical content worth storing |\n",
		len(substantives), pct(len(substantives), total))
	fmt.Fprintln(w)

	// --- Problem statement ---
	fmt.Fprintf(w, "## The Problem\n\n")
	fmt.Fprintf(w, "**Explanation-of-findings responses** pass the noise gate (they're long, with high alnum ratio)\n")
	fmt.Fprintf(w, "but carry no durable knowledge. They follow a consistent template:\n\n")
	fmt.Fprintf(w, "> _I've reviewed / Looking at / Based on my analysis / I've completed..._\n\n")
	fmt.Fprintf(w, "These %d entries (%.0f%% of the sample) accumulate in the memory store and dilute retrieval\n",
		len(explanations), pct(len(explanations), total))
	fmt.Fprintf(w, "quality without contributing useful context to future sessions.\n\n")

	// --- Explanation samples ---
	fmt.Fprintf(w, "## Representative Explanation-of-Findings Samples\n\n")
	fmt.Fprintf(w, "_10 examples from the dataset — these are what we want to filter:_\n\n")
	limit := 10
	if len(explanations) < limit {
		limit = len(explanations)
	}
	for i, e := range explanations[:limit] {
		preview := e
		if len(preview) > 200 {
			preview = preview[:200] + "…"
		}
		// Escape pipe chars for markdown table
		preview = strings.ReplaceAll(preview, "|", "\\|")
		preview = strings.ReplaceAll(preview, "\n", " ")
		fmt.Fprintf(w, "%d. %s\n\n", i+1, preview)
	}

	// --- Proposed noise protos ---
	fmt.Fprintf(w, "## Proposed Additional Noise Protos\n\n")
	fmt.Fprintf(w, "Add these to `noise_protos` in the pipeline config. They are short semantic descriptions\n")
	fmt.Fprintf(w, "that the content scorer will embed and use to recognise explanation-of-findings content.\n\n")
	fmt.Fprintf(w, "```json\n")
	protoJSON, _ := json.MarshalIndent(explanationNoiseProtos, "", "  ")
	fmt.Fprintln(w, string(protoJSON))
	fmt.Fprintf(w, "```\n\n")

	// --- Live sweep results ---
	if liveRan && len(results) > 0 {
		fmt.Fprintf(w, "## Sweep Results\n\n")
		fmt.Fprintf(w, "**Phase 1** sweeps `noise_min_len` (gate=0, alnum=0.40) — isolates the length bottleneck.  \n")
		fmt.Fprintf(w, "**Phase 2** sweeps `noise_min_alnum_ratio` (min_len=40, gate=0) — isolates the symbol bottleneck.  \n")
		fmt.Fprintf(w, "**Phase 3** sweeps `content_score_gate` × explain-protos (min_len=40, alnum=0.25).  \n\n")
		fmt.Fprintf(w, "Each configuration tested against %d explanation entries and %d substantive entries.\n\n",
			results[0].explainFiltered+results[0].explainKept,
			results[0].substFiltered+results[0].substKept,
		)
		fmt.Fprintf(w, "| Config | Explain filtered %% | Subst false-positive %% | Score |\n")
		fmt.Fprintf(w, "|--------|---------------------|--------------------------|-------|\n")

		sorted := make([]sweepResult, len(results))
		copy(sorted, results)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].score() > sorted[j].score()
		})

		for _, r := range results { // print in original sweep order
			fmt.Fprintf(w, "| %-36s | %5.0f%% | %5.0f%% | %+.3f |\n",
				r.cfg.name,
				r.explainFilterRate()*100,
				r.substFalsePositiveRate()*100,
				r.score(),
			)
		}
		fmt.Fprintln(w)

		// Best config
		best := sorted[0]
		fmt.Fprintf(w, "## Recommended Configuration\n\n")
		fmt.Fprintf(w, "Best result: **%s**\n\n", best.cfg.name)
		fmt.Fprintf(w, "- Explanation filter rate: **%.0f%%** (%d / %d filtered)\n",
			best.explainFilterRate()*100, best.explainFiltered, best.explainFiltered+best.explainKept)
		fmt.Fprintf(w, "- Substantive false positive rate: **%.0f%%** (%d / %d incorrectly filtered)\n\n",
			best.substFalsePositiveRate()*100, best.substFiltered, best.substFiltered+best.substKept)

		// Recommended config JSON
		noiseProtos := explanationNoiseProtos
		if !best.cfg.addProtos {
			noiseProtos = nil
		}
		recCfg := struct {
			ContentScoreGate float64  `json:"content_score_gate"`
			NoiseProtos      []string `json:"noise_protos,omitempty"`
		}{
			ContentScoreGate: best.cfg.gate,
			NoiseProtos:      noiseProtos,
		}
		fmt.Fprintf(w, "Apply via `POST /api/pipeline`:\n\n")
		fmt.Fprintf(w, "```json\n")
		cfgJSON, _ := json.MarshalIndent(map[string]any{"pipeline": recCfg}, "", "  ")
		fmt.Fprintln(w, string(cfgJSON))
		fmt.Fprintf(w, "```\n\n")

		fmt.Fprintf(w, "> **Note:** `noise_protos` appends to the defaults — the default protos\n")
		fmt.Fprintf(w, "> (`greeting acknowledgment helpful response...`) remain active.\n")
		fmt.Fprintf(w, "> `content_score_gate` applies to ALL content; tune upward with caution.\n\n")

	} else if !liveRan {
		fmt.Fprintf(w, "## Gate Sweep Results\n\n")
		fmt.Fprintf(w, "_Phase 2 skipped — memoryd was not reachable. Start the daemon and re-run\n")
		fmt.Fprintf(w, "to get sweep results and a recommended `content_score_gate` value._\n\n")
		fmt.Fprintf(w, "### Suggested starting point (without sweep data)\n\n")
		fmt.Fprintf(w, "Based on the classification distribution above, a reasonable starting config is:\n\n")
		fmt.Fprintf(w, "```json\n")
		starter, _ := json.MarshalIndent(map[string]any{
			"pipeline": map[string]any{
				"content_score_gate": 0.15,
				"noise_protos":       explanationNoiseProtos,
			},
		}, "", "  ")
		fmt.Fprintln(w, string(starter))
		fmt.Fprintf(w, "```\n\n")
		fmt.Fprintf(w, "Run again with memoryd running to validate this against your actual data.\n\n")
	}

	// --- Interpretation guide ---
	fmt.Fprintf(w, "## Interpretation Guide\n\n")
	fmt.Fprintf(w, "| Knob | Effect | Risk |\n")
	fmt.Fprintf(w, "|------|--------|------|\n")
	fmt.Fprintf(w, "| `content_score_gate` ↑ | Filters more low-quality content | May filter short but specific notes |\n")
	fmt.Fprintf(w, "| `noise_protos` additions | Shifts scorer toward filtering similar content | Protos must be carefully chosen |\n")
	fmt.Fprintf(w, "| `noise_min_len` ↑ | Raises minimum length before embedding | Short commands/snippets get filtered |\n")
	fmt.Fprintf(w, "| `noise_min_alnum_ratio` ↑ | Stricter symbol filtering | Tool outputs with paths/symbols lost |\n\n")

	fmt.Fprintf(w, "The `content_score_gate` and `noise_protos` work together: the protos shift the scorer\n")
	fmt.Fprintf(w, "toward lower scores for explanation-like content; the gate sets the cutoff.\n")
	fmt.Fprintf(w, "Both can be updated live via `POST /api/pipeline` without restarting the daemon.\n")
}

// ---------------------------------------------------------------------------
// Direct eval — feeds N rows through /api/ingest (full pipeline)
// ---------------------------------------------------------------------------

type ingestResp struct {
	Stage   string `json:"stage"`
	Stored  int    `json:"stored"`
	Entry   string `json:"entry"`
	Summary string `json:"summary"`
}

type rowResult struct {
	stage   string
	latency time.Duration
	err     error
}

func runDirectEval(w io.Writer, baseURL string, sampleSize, concurrency int, noCleanup bool) {
	token := loadToken()

	fmt.Fprintf(os.Stderr, "=== memoryd direct ingest eval ===\n")
	fmt.Fprintf(os.Stderr, "dataset:     %s\n", hfDataset)
	fmt.Fprintf(os.Stderr, "rows:        %d\n", sampleSize)
	fmt.Fprintf(os.Stderr, "concurrency: %d\n", concurrency)
	fmt.Fprintf(os.Stderr, "target:      %s\n\n", baseURL)

	// Check connectivity.
	var health struct{ Status string }
	if err := authGet(baseURL, "/health", &health, token); err != nil || health.Status != "ok" {
		fmt.Fprintf(os.Stderr, "error: memoryd not reachable at %s\n", baseURL)
		os.Exit(1)
	}

	// Fetch rows.
	fmt.Fprintln(os.Stderr, "[1/3] fetching rows from HuggingFace...")
	rows, err := fetchHFRows(sampleSize)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "      fetched %d rows\n\n", len(rows))

	// Ingest with worker pool.
	fmt.Fprintf(os.Stderr, "[2/3] ingesting through /api/ingest (%d workers)...\n", concurrency)
	start := time.Now()

	type work struct {
		row hfRow
		idx int
	}
	workCh := make(chan work, concurrency*2)
	resultCh := make(chan rowResult, len(rows))

	evalSource := fmt.Sprintf("direct-eval-%d", time.Now().UnixNano())

	for i := 0; i < concurrency; i++ {
		go func() {
			ingestClient := &http.Client{Timeout: 120 * time.Second}
			for job := range workCh {
				t0 := time.Now()
				ar := job.row.AssistantResponse
				if len(ar) > 32*1024 {
					ar = ar[:32*1024]
				}
				body, _ := json.Marshal(map[string]string{
					"user_prompt":          job.row.UserPrompt,
					"assistant_response":   ar,
					"source":               evalSource,
				})
				req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/ingest", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				resp, err := ingestClient.Do(req)
				if err != nil {
					fmt.Fprintf(os.Stderr, "      [debug] network err: %v\n", err)
					resultCh <- rowResult{stage: "error", latency: time.Since(t0), err: err}
					continue
				}
				var ir ingestResp
				data, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				json.Unmarshal(data, &ir)
				stage := ir.Stage
				if resp.StatusCode >= 400 || stage == "" {
					stage = "error"
					// Debug first few errors.
					dlen := len(data)
					if dlen > 200 {
						dlen = 200
					}
					fmt.Fprintf(os.Stderr, "      [debug] HTTP %d body: %s\n", resp.StatusCode, string(data[:dlen]))
				}
				resultCh <- rowResult{stage: stage, latency: time.Since(t0)}

				// Progress tick every 100 rows.
				if job.idx%100 == 0 {
					fmt.Fprintf(os.Stderr, "      %d / %d\n", job.idx, len(rows))
				}
			}
		}()
	}

	for i, row := range rows {
		workCh <- work{row: row, idx: i + 1}
	}
	close(workCh)

	results := make([]rowResult, len(rows))
	for i := range rows {
		results[i] = <-resultCh
	}
	elapsed := time.Since(start)

	// Aggregate.
	stageCounts := map[string]int{}
	var latencies []float64
	for _, r := range results {
		stageCounts[r.stage]++
		if r.err == nil {
			latencies = append(latencies, float64(r.latency.Milliseconds()))
		}
	}
	sort.Float64s(latencies)

	p := func(pct float64) float64 {
		if len(latencies) == 0 {
			return 0
		}
		idx := int(float64(len(latencies))*pct/100) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(latencies) {
			idx = len(latencies) - 1
		}
		return latencies[idx]
	}

	fmt.Fprintf(os.Stderr, "      done in %s\n\n", elapsed.Round(time.Second))

	// Fetch rejection stats.
	fmt.Fprintln(os.Stderr, "[3/3] fetching rejection store stats...")
	var rejResp struct {
		Stats struct {
			Total           int            `json:"total"`
			Capacity        int            `json:"capacity"`
			ByStage         map[string]int `json:"by_stage"`
			AvgUserLen      float64        `json:"avg_user_len"`
			AvgAsstLen      float64        `json:"avg_asst_len"`
			TopAsstPrefixes []struct {
				Prefix string `json:"prefix"`
				Count  int    `json:"count"`
			} `json:"top_asst_prefixes"`
		} `json:"stats"`
		Sample []struct {
			Stage      string `json:"stage"`
			UserLen    int    `json:"user_len"`
			AsstLen    int    `json:"asst_len"`
			UserPrefix string `json:"user_prefix"`
			AsstPrefix string `json:"asst_prefix"`
		} `json:"sample"`
	}
	_ = authGet(baseURL, "/api/rejections?n=5", &rejResp, token)

	// Cleanup.
	if !noCleanup {
		fmt.Fprintln(os.Stderr, "      cleaning up eval memories...")
		deleteBySource(baseURL, token, evalSource)
	}

	// Report.
	writeDirectReport(w, rows, results, stageCounts, latencies, elapsed, p, rejResp.Stats.ByStage, rejResp.Stats.TopAsstPrefixes, rejResp.Stats.Total, rejResp.Stats.Capacity, rejResp.Stats.AvgAsstLen)
}

func writeDirectReport(w io.Writer, rows []hfRow, results []rowResult,
	stageCounts map[string]int, latencies []float64, elapsed time.Duration,
	p func(float64) float64,
	rejByStage map[string]int, topPrefixes []struct {
		Prefix string `json:"prefix"`
		Count  int    `json:"count"`
	}, rejTotal, rejCap int, avgAsstLen float64,
) {
	total := len(rows)
	stored := stageCounts["stored"]
	preFilter := stageCounts["pre_filter"]
	synthSkip := stageCounts["synthesizer_skip"]
	noiseFiltered := stageCounts["noise_filtered"]
	noSynth := stageCounts["no_synthesizer"]
	errors := stageCounts["error"]
	filtered := preFilter + synthSkip + noiseFiltered

	now := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(w, "# memoryd Direct Ingest Eval — %s\n\n", now)
	fmt.Fprintf(w, "**Dataset:** `%s`  \n", hfDataset)
	fmt.Fprintf(w, "**Rows ingested:** %d  \n", total)
	fmt.Fprintf(w, "**Wall time:** %s  \n", elapsed.Round(time.Second))
	if elapsed.Seconds() > 0 {
		fmt.Fprintf(w, "**Throughput:** %.1f rows/sec  \n\n", float64(total)/elapsed.Seconds())
	}

	// Stage breakdown.
	fmt.Fprintf(w, "## Pipeline Stage Breakdown\n\n")
	fmt.Fprintf(w, "| Stage | Count | %% | Description |\n")
	fmt.Fprintf(w, "|-------|-------|----|-------------|\n")
	fmt.Fprintf(w, "| stored | %d | %.0f%% | distilled entry reached the write pipeline |\n", stored, pct(stored, total))
	fmt.Fprintf(w, "| synthesizer_skip | %d | %.0f%% | LLM judged exchange as no durable value |\n", synthSkip, pct(synthSkip, total))
	fmt.Fprintf(w, "| pre_filter | %d | %.0f%% | cheap heuristic (ack + procedural prefix) |\n", preFilter, pct(preFilter, total))
	fmt.Fprintf(w, "| noise_filtered | %d | %.0f%% | no synthesizer — write pipeline noise gate |\n", noiseFiltered, pct(noiseFiltered, total))
	if noSynth > 0 {
		fmt.Fprintf(w, "| no_synthesizer | %d | %.0f%% | synthesizer unavailable, raw write |\n", noSynth, pct(noSynth, total))
	}
	if errors > 0 {
		fmt.Fprintf(w, "| error | %d | %.0f%% | network / API error |\n", errors, pct(errors, total))
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "**Total filtered:** %d / %d (%.0f%%)  \n", filtered, total, pct(filtered, total))
	fmt.Fprintf(w, "**Store rate:** %d / %d (%.0f%%)\n\n", stored, total, pct(stored, total))

	// Latency.
	if len(latencies) > 0 {
		var sum float64
		for _, l := range latencies {
			sum += l
		}
		fmt.Fprintf(w, "## Latency (per row, ms)\n\n")
		fmt.Fprintf(w, "| p50 | p90 | p99 | mean |\n")
		fmt.Fprintf(w, "|-----|-----|-----|------|\n")
		fmt.Fprintf(w, "| %.0f | %.0f | %.0f | %.0f |\n\n",
			p(50), p(90), p(99), sum/float64(len(latencies)))
	}

	// Rejection store.
	fmt.Fprintf(w, "## Rejection Store\n\n")
	fmt.Fprintf(w, "**Total stored:** %d / %d capacity  \n", rejTotal, rejCap)
	fmt.Fprintf(w, "**Avg assistant text length:** %.0f chars  \n\n", avgAsstLen)
	if len(rejByStage) > 0 {
		fmt.Fprintf(w, "| Stage | Count |\n|-------|-------|\n")
		for stage, n := range rejByStage {
			fmt.Fprintf(w, "| %s | %d |\n", stage, n)
		}
		fmt.Fprintln(w)
	}
	if len(topPrefixes) > 0 {
		fmt.Fprintf(w, "### Top rejected assistant first-word tokens\n\n")
		fmt.Fprintf(w, "| Token | Count |\n|-------|-------|\n")
		for _, tp := range topPrefixes {
			fmt.Fprintf(w, "| `%s` | %d |\n", tp.Prefix, tp.Count)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "## Interpretation\n\n")
	fmt.Fprintf(w, "- **Store rate** is the fraction of real-world LLM exchanges that\n")
	fmt.Fprintf(w, "  contained durable technical knowledge worth retrieving in a future session.\n")
	fmt.Fprintf(w, "- **synthesizer_skip** entries reached the LLM but were judged procedural\n")
	fmt.Fprintf(w, "  (workflow narration with no reusable knowledge). As the rejection store\n")
	fmt.Fprintf(w, "  fills, these will be caught earlier by the content scorer.\n")
	fmt.Fprintf(w, "- **pre_filter** entries were caught before any LLM call by matching known\n")
	fmt.Fprintf(w, "  short-ack + procedural-prefix patterns. Zero LLM cost.\n")
	fmt.Fprintf(w, "- The **top rejected tokens** table shows what to add to `proceduralAsstPrefixes`\n")
	fmt.Fprintf(w, "  in `internal/rejection/prefilter.go` to push more exchanges to the cheap path.\n")
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return math.Round(float64(n)/float64(total)*100*10) / 10
}
