package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/memory-daemon/memoryd/eval"
)

func main() {
	var (
		scenariosFile = flag.String("scenarios", "eval/scenarios/scenarios.json", "path to scenarios JSON file")
		scenarioName  = flag.String("scenario", "", "run only this scenario (by name)")
		jsonOutput    = flag.Bool("json", false, "output JSON instead of human-readable report")
		model         = flag.String("model", "", "model for task runs (default: claude-sonnet-4-20250514)")
		judgeModel    = flag.String("judge-model", "", "model for judging (default: same as -model)")
		memorydURL    = flag.String("memoryd", "http://127.0.0.1:7432", "memoryd daemon URL")
	)
	flag.Parse()

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "ANTHROPIC_API_KEY environment variable required")
		os.Exit(1)
	}

	data, err := os.ReadFile(*scenariosFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read scenarios: %v\n", err)
		os.Exit(1)
	}

	scenarios, err := eval.LoadScenarios(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse scenarios: %v\n", err)
		os.Exit(1)
	}

	if *scenarioName != "" {
		var filtered []eval.Scenario
		for _, s := range scenarios {
			if strings.EqualFold(s.Name, *scenarioName) {
				filtered = append(filtered, s)
			}
		}
		if len(filtered) == 0 {
			fmt.Fprintf(os.Stderr, "scenario %q not found\n", *scenarioName)
			os.Exit(1)
		}
		scenarios = filtered
	}

	cfg := eval.Config{
		AnthropicKey: apiKey,
		MemorydURL:   *memorydURL,
		Model:        *model,
		JudgeModel:   *judgeModel,
	}

	h := eval.NewHarness(cfg)
	ctx := context.Background()

	results, err := h.RunAll(ctx, scenarios)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval failed: %v\n", err)
		os.Exit(1)
	}

	if *jsonOutput {
		if err := eval.ReportJSON(os.Stdout, results); err != nil {
			fmt.Fprintf(os.Stderr, "json output: %v\n", err)
			os.Exit(1)
		}
	} else {
		eval.Report(os.Stdout, results)
	}
}
