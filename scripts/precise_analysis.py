#!/usr/bin/env python3
"""Precise analysis using original eval-mixed.jsonl entries as ground truth."""
import json

# Load original eval-mixed entries
mixed_entries = {}
with open("data/eval-mixed.jsonl") as f:
    for line in f:
        d = json.loads(line)
        key = d["assistant_response"][:100]
        mixed_entries[key] = d["label"]

# Build index of eval-large entries that came from eval-mixed
dataset = []
from_mixed = {}
with open("data/eval-large.jsonl") as f:
    for i, line in enumerate(f):
        d = json.loads(line)
        dataset.append(d)
        key = d["assistant_response"][:100]
        if key in mixed_entries:
            from_mixed[i] = mixed_entries[key]

print(f"Matched {len(from_mixed)} entries from eval-mixed in eval-large")
label_counts = {}
for idx, lbl in from_mixed.items():
    label_counts[lbl] = label_counts.get(lbl, 0) + 1
print(f"  Labels: {label_counts}")

# Analyze each run
for run_file in ["benchmark-1k-r1.jsonl", "benchmark-1k-r2.jsonl", "benchmark-1k-r3.jsonl"]:
    results = {}
    with open(run_file) as f:
        for line in f:
            r = json.loads(line)
            results[r["index"]] = r

    print(f"\n{'='*50}")
    print(f"{run_file}")
    print(f"{'='*50}")

    for label in ["noise", "low", "substantive"]:
        stored = 0
        filtered = 0
        error = 0
        stages = {}
        for idx, lbl in from_mixed.items():
            if lbl != label:
                continue
            r = results.get(idx)
            if not r:
                continue
            stage = r["stage"]
            stages[stage] = stages.get(stage, 0) + 1
            if stage == "stored":
                stored += 1
            elif stage == "error":
                error += 1
            else:
                filtered += 1
        total = stored + filtered + error
        if total > 0:
            pct = stored / total * 100
            print(f"  {label:>12}: {stored}/{total} stored ({pct:.0f}%)  stages={stages}")
