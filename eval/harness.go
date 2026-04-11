// Package eval runs qualitative A/B comparisons of Claude with and without memoryd.
package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Scenario struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Seeds       []string `json:"seeds"`
	Prompt      string   `json:"prompt"`
	Rubric      []string `json:"rubric"`
	MaxScore    int      `json:"max_score"`
	Tags        []string `json:"tags"`
}

type RunResult struct {
	Condition       string    `json:"condition"`
	Response        string    `json:"response"`
	Latency         time.Duration `json:"latency_ms"`
	Context         string    `json:"context,omitempty"`
	RetrievalScores []float64 `json:"retrieval_scores,omitempty"`
}

type JudgeScore struct {
	Criterion   string `json:"criterion"`
	BareScore   int    `json:"bare_score"`
	AugScore    int    `json:"aug_score"`
	Explanation string `json:"explanation"`
}

type ScenarioResult struct {
	Scenario  string       `json:"scenario"`
	Bare      RunResult    `json:"bare"`
	Augmented RunResult    `json:"augmented"`
	Scores    []JudgeScore `json:"scores"`
	BareTotal int          `json:"bare_total"`
	AugTotal  int          `json:"aug_total"`
	Delta     int          `json:"delta"`
}

type Config struct {
	AnthropicKey string
	AnthropicURL string
	MemorydURL   string
	Model        string
	JudgeModel   string
	MaxTokens    int
}

type Harness struct {
	cfg    Config
	client *http.Client
}

func NewHarness(cfg Config) *Harness {
	if cfg.AnthropicURL == "" {
		cfg.AnthropicURL = "https://api.anthropic.com"
	}
	if cfg.MemorydURL == "" {
		cfg.MemorydURL = "http://127.0.0.1:7432"
	}
	if cfg.Model == "" {
		cfg.Model = "claude-sonnet-4-20250514"
	}
	if cfg.JudgeModel == "" {
		cfg.JudgeModel = cfg.Model
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 4096
	}
	return &Harness{
		cfg:    cfg,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (h *Harness) Run(ctx context.Context, sc Scenario) (*ScenarioResult, error) {
	if err := h.seedMemories(ctx, sc.Seeds); err != nil {
		return nil, fmt.Errorf("seed: %w", err)
	}
	bare, err := h.runBare(ctx, sc)
	if err != nil {
		return nil, fmt.Errorf("bare run: %w", err)
	}
	aug, err := h.runAugmented(ctx, sc)
	if err != nil {
		return nil, fmt.Errorf("augmented run: %w", err)
	}
	scores, err := h.judge(ctx, sc, bare, aug)
	if err != nil {
		return nil, fmt.Errorf("judge: %w", err)
	}
	var bareTotal, augTotal int
	for _, s := range scores {
		bareTotal += s.BareScore
		augTotal += s.AugScore
	}
	return &ScenarioResult{
		Scenario:  sc.Name,
		Bare:      *bare,
		Augmented: *aug,
		Scores:    scores,
		BareTotal: bareTotal,
		AugTotal:  augTotal,
		Delta:     augTotal - bareTotal,
	}, nil
}

func (h *Harness) RunAll(ctx context.Context, scenarios []Scenario) ([]ScenarioResult, error) {
	var results []ScenarioResult
	for i, sc := range scenarios {
		fmt.Fprintf(os.Stderr, "[%d/%d] %s ...\n", i+1, len(scenarios), sc.Name)
		if err := h.clearMemories(ctx); err != nil {
			return nil, fmt.Errorf("clear before %s: %w", sc.Name, err)
		}
		r, err := h.Run(ctx, sc)
		if err != nil {
			return nil, fmt.Errorf("scenario %s: %w", sc.Name, err)
		}
		results = append(results, *r)
	}
	return results, nil
}

func (h *Harness) seedMemories(ctx context.Context, seeds []string) error {
	for _, s := range seeds {
		body, _ := json.Marshal(map[string]string{"content": s, "source": "eval-seed"})
		req, _ := http.NewRequestWithContext(ctx, "POST", h.cfg.MemorydURL+"/api/store", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := h.client.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
	}
	return nil
}

func (h *Harness) clearMemories(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", h.cfg.MemorydURL+"/api/memories", nil)
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var memories []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&memories); err != nil {
		return err
	}
	for _, m := range memories {
		dreq, _ := http.NewRequestWithContext(ctx, "DELETE", h.cfg.MemorydURL+"/api/memories/"+m.ID, nil)
		dr, err := h.client.Do(dreq)
		if err != nil {
			return err
		}
		dr.Body.Close()
	}
	return nil
}

func (h *Harness) retrieveContext(ctx context.Context, query string) (string, []float64, error) {
	body, _ := json.Marshal(map[string]string{"query": query})
	req, _ := http.NewRequestWithContext(ctx, "POST", h.cfg.MemorydURL+"/api/search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	var result struct {
		Context string    `json:"context"`
		Scores  []float64 `json:"scores"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil, err
	}
	return result.Context, result.Scores, nil
}

func (h *Harness) callClaude(ctx context.Context, system, user string) (string, time.Duration, error) {
	msgs := []map[string]string{{"role": "user", "content": user}}
	payload := map[string]any{
		"model":      h.cfg.Model,
		"max_tokens": h.cfg.MaxTokens,
		"messages":   msgs,
	}
	if system != "" {
		payload["system"] = system
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", h.cfg.AnthropicURL+"/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", h.cfg.AnthropicKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	start := time.Now()
	resp, err := h.client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return "", latency, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", latency, fmt.Errorf("anthropic %d: %s", resp.StatusCode, string(b))
	}
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", latency, err
	}
	var texts []string
	for _, c := range result.Content {
		texts = append(texts, c.Text)
	}
	return strings.Join(texts, "\n"), latency, nil
}

func (h *Harness) runBare(ctx context.Context, sc Scenario) (*RunResult, error) {
	text, latency, err := h.callClaude(ctx, "", sc.Prompt)
	if err != nil {
		return nil, err
	}
	return &RunResult{Condition: "bare", Response: text, Latency: latency}, nil
}

func (h *Harness) runAugmented(ctx context.Context, sc Scenario) (*RunResult, error) {
	retrieved, scores, err := h.retrieveContext(ctx, sc.Prompt)
	if err != nil {
		return nil, fmt.Errorf("retrieve: %w", err)
	}
	system := ""
	if retrieved != "" {
		system = retrieved
	}
	text, latency, err := h.callClaude(ctx, system, sc.Prompt)
	if err != nil {
		return nil, err
	}
	return &RunResult{Condition: "augmented", Response: text, Latency: latency, Context: retrieved, RetrievalScores: scores}, nil
}

func (h *Harness) judge(ctx context.Context, sc Scenario, bare, aug *RunResult) ([]JudgeScore, error) {
	maxScore := sc.MaxScore
	if maxScore == 0 {
		maxScore = 5
	}
	rubricLines := ""
	for i, r := range sc.Rubric {
		rubricLines += fmt.Sprintf("%d. %s\n", i+1, r)
	}
	judgePrompt := fmt.Sprintf("You are an expert evaluator comparing two AI responses to the same task.\n\nTASK:\n%s\n\nRUBRIC (score each criterion 1-%d):\n%s\nRESPONSE A (bare):\n%s\n\nRESPONSE B (augmented with retrieved context):\n%s\n\nFor each rubric criterion, output a JSON array. Each element must have:\n- \"criterion\": the criterion text\n- \"bare_score\": integer 1-%d\n- \"aug_score\": integer 1-%d\n- \"explanation\": one sentence explaining the difference\n\nOutput ONLY the JSON array, no other text.",
		sc.Prompt, maxScore, rubricLines, bare.Response, aug.Response, maxScore, maxScore)
	origModel := h.cfg.Model
	h.cfg.Model = h.cfg.JudgeModel
	text, _, err := h.callClaude(ctx, "You are a fair, rigorous evaluator. Output only valid JSON.", judgePrompt)
	h.cfg.Model = origModel
	if err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) > 2 {
			text = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	var scores []JudgeScore
	if err := json.Unmarshal([]byte(text), &scores); err != nil {
		return nil, fmt.Errorf("judge parse: %w\nraw: %s", err, text)
	}
	return scores, nil
}
