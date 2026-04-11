#!/usr/bin/env python3
"""Cross-reference benchmark JSONL results with ground-truth labels."""
import json, sys

results_path = sys.argv[1] if len(sys.argv) > 1 else "benchmark-eval-mixed.jsonl"
labels_path = sys.argv[2] if len(sys.argv) > 2 else "data/eval-mixed.jsonl"

# Load results
results = []
with open(results_path) as f:
    for line in f:
        results.append(json.loads(line))

# Load labels from source dataset by index
label_by_idx = {}
with open(labels_path) as f:
    for i, line in enumerate(f):
        d = json.loads(line)
        label_by_idx[i] = d.get("label", "unknown")

# Cross-reference
matrix = {}
filtered_substantive = []
for r in results:
    idx = r.get("index", -1)
    label = label_by_idx.get(idx, "unknown")
    stage = r.get("stage", "")
    stored = stage == "stored"

    if label not in matrix:
        matrix[label] = {"stored": 0, "filtered": 0, "total": 0}
    matrix[label]["total"] += 1
    if stored:
        matrix[label]["stored"] += 1
    else:
        matrix[label]["filtered"] += 1

    if label == "substantive" and not stored:
        filtered_substantive.append((stage, r.get("summary", "")[:120]))

print("=== Ground-Truth Label x Pipeline Stage ===")
print(f"{'Label':<14} {'Stored':>7} {'Filtered':>9} {'Total':>6} {'Store%':>7}")
print("-" * 50)
for label in ["noise", "low", "substantive", "unknown"]:
    if label in matrix:
        m = matrix[label]
        pct = m["stored"] / m["total"] * 100 if m["total"] else 0
        print(f"{label:<14} {m['stored']:>7} {m['filtered']:>9} {m['total']:>6} {pct:>6.1f}%")

sub = matrix.get("substantive", {})
noise = matrix.get("noise", {})
low = matrix.get("low", {})
print()
print(f"Substantive recall:  {sub.get('stored',0)}/{sub.get('total',0)} stored  ({sub.get('stored',0)/max(sub.get('total',1),1)*100:.0f}%)")
print(f"Noise precision:     {noise.get('filtered',0)}/{noise.get('total',0)} filtered ({noise.get('filtered',0)/max(noise.get('total',1),1)*100:.0f}%)")
print(f"Low precision:       {low.get('filtered',0)}/{low.get('total',0)} filtered ({low.get('filtered',0)/max(low.get('total',1),1)*100:.0f}%)")

print()
print(f"=== Filtered substantive samples ({len(filtered_substantive)} total, showing first 15) ===")
for i, (stage, preview) in enumerate(filtered_substantive[:15]):
    print(f"  {i+1:2d}. [{stage}] {preview}")
