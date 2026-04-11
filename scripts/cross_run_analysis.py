#!/usr/bin/env python3
"""Cross-run analysis for benchmark runs 1-3."""
import json
import sys

print("=== Cross-Run Analysis ===\n")

runs = ["benchmark-final1.jsonl", "benchmark-final2.jsonl", "benchmark-final3.jsonl"]
data = {}

for i, run in enumerate(runs, 1):
    stages = {}
    total_latency = 0
    count = 0
    try:
        with open(run) as f:
            for line in f:
                d = json.loads(line)
                stages[d["stage"]] = stages.get(d["stage"], 0) + 1
                total_latency += d.get("latency_ms", 0)
                count += 1
    except FileNotFoundError:
        print(f"  {run} not found, skipping")
        continue
    data[i] = {"stages": stages, "avg_lat": total_latency / count if count else 0}

header = f"{'Stage':<24} {'Run 1':>6} {'Run 2':>6} {'Run 3':>6}  {'Change 1->3':>12}"
print(header)
print("-" * len(header))

all_stages = [
    "length_filter", "content_score_filter", "pre_filter",
    "synthesizer_skip", "stored", "noise_filtered", "error",
]
for stage in all_stages:
    v1 = data.get(1, {}).get("stages", {}).get(stage, 0)
    v2 = data.get(2, {}).get("stages", {}).get(stage, 0)
    v3 = data.get(3, {}).get("stages", {}).get(stage, 0)
    if v1 or v2 or v3:
        change = v3 - v1
        arrow = "+" if change > 0 else "" if change < 0 else " "
        print(f"{stage:<24} {v1:>6} {v2:>6} {v3:>6}  {arrow}{change:>11}")

print()
for i in sorted(data.keys()):
    lat = data[i]["avg_lat"]
    print(f"Run {i}: avg latency {lat:.0f}ms")

print()
print("=== Haiku API Calls ===\n")

for i in sorted(data.keys()):
    s = data[i]["stages"]
    pre_filtered = s.get("length_filter", 0) + s.get("content_score_filter", 0) + s.get("pre_filter", 0)
    haiku_calls = 144 - pre_filtered
    pct = pre_filtered / 144 * 100
    print(f"Run {i}: {haiku_calls:>3} Haiku calls  ({pct:.0f}% filtered pre-LLM)")

if 1 in data and 3 in data:
    s1 = data[1]["stages"]
    s3 = data[3]["stages"]
    r1_pre = s1.get("length_filter", 0) + s1.get("content_score_filter", 0) + s1.get("pre_filter", 0)
    r3_pre = s3.get("length_filter", 0) + s3.get("content_score_filter", 0) + s3.get("pre_filter", 0)
    r1_haiku = 144 - r1_pre
    r3_haiku = 144 - r3_pre
    saved = r1_haiku - r3_haiku
    print(f"\nAdaptive noise learning saved {saved} Haiku calls ({saved/r1_haiku*100:.0f}% reduction)")
    print(f"Avg latency: {data[1]['avg_lat']:.0f}ms -> {data[3]['avg_lat']:.0f}ms ({(1-data[3]['avg_lat']/data[1]['avg_lat'])*100:.0f}% faster)")
