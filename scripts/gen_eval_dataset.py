#!/usr/bin/env python3
"""
Build a large eval dataset by combining:
  1. eval-mixed.jsonl  — 144 hand-crafted entries with ground-truth labels
  2. dataset-hf.jsonl  — 32K HF entries, randomly sampled + heuristic-labeled

Output: data/eval-large.jsonl (~1000 entries)
"""
import json
import random
import re
import sys
import collections

random.seed(2026)

# Patterns that indicate noise even if text is medium-length
ACK_STARTS = [
    "i'll ", "let me ", "sure", "ok", "done", "got it", "understood",
    "running", "checking", "opening", "reading", "searching",
    "i've ", "i have ", "here's the ", "proceeding", "continuing",
]

PROCEDURAL_PATTERNS = re.compile(
    r"(?i)^(let me |i'll |i need to |searching |running |checking |"
    r"looking at |reading |now |opening |executing |starting )"
)


def label_hf_row(user_prompt, assistant_response):
    """Heuristic label for an HF dataset row."""
    resp = assistant_response.strip()
    prompt = user_prompt.strip().lower()
    resp_lower = resp.lower()
    rlen = len(resp)

    # --- Noise ---
    # Very short
    if rlen < 80:
        return "noise"

    # Short ack / procedural
    if rlen < 200:
        for pat in ACK_STARTS:
            if resp_lower.startswith(pat):
                return "noise"
        if PROCEDURAL_PATTERNS.match(resp):
            return "noise"

    # --- Substantive ---
    has_code = "```" in resp
    has_bullets = resp.count("\n- ") >= 3 or resp.count("\n* ") >= 3
    has_numbered = resp.count("\n1.") >= 1 and resp.count("\n2.") >= 1

    # Long with code
    if rlen > 400 and has_code:
        return "substantive"

    # Very long with structure
    if rlen > 800 and (has_bullets or has_numbered):
        return "substantive"

    # Very long
    if rlen > 1200:
        return "substantive"

    # --- Low ---
    return "low"


def main():
    target = int(sys.argv[1]) if len(sys.argv) > 1 else 1000
    output = sys.argv[2] if len(sys.argv) > 2 else "data/eval-large.jsonl"
    eval_mixed_path = "data/eval-mixed.jsonl"
    hf_path = "data/dataset-hf.jsonl"

    # 1. Load eval-mixed (ground-truth labels)
    mixed = []
    with open(eval_mixed_path) as f:
        for line in f:
            d = json.loads(line)
            mixed.append({
                "user_prompt": d["user_prompt"],
                "assistant_response": d["assistant_response"],
                "label": d["label"],
            })
    print(f"Loaded {len(mixed)} hand-crafted entries from {eval_mixed_path}")

    # 2. Load HF dataset
    hf_all = []
    with open(hf_path) as f:
        for line in f:
            d = json.loads(line)
            hf_all.append(d)
    print(f"Loaded {len(hf_all)} HF entries from {hf_path}")

    # 3. Random sample from HF
    hf_needed = target - len(mixed)
    if hf_needed > len(hf_all):
        hf_needed = len(hf_all)
    hf_sample = random.sample(hf_all, hf_needed)

    # 4. Label HF rows
    hf_labeled = []
    for d in hf_sample:
        label = label_hf_row(d.get("user_prompt", ""), d.get("assistant_response", ""))
        hf_labeled.append({
            "user_prompt": d.get("user_prompt", ""),
            "assistant_response": d.get("assistant_response", ""),
            "label": label,
        })

    # 5. Combine and shuffle
    all_entries = mixed + hf_labeled
    random.shuffle(all_entries)

    # 6. Write
    with open(output, "w") as f:
        for entry in all_entries:
            f.write(json.dumps(entry) + "\n")

    # 7. Stats
    labels = collections.Counter(e["label"] for e in all_entries)
    print(f"\nGenerated {len(all_entries)} entries -> {output}")
    print(f"  noise: {labels['noise']}  low: {labels['low']}  substantive: {labels['substantive']}")
    print(f"  from eval-mixed: {len(mixed)}  from HF: {len(hf_labeled)}")

    hf_labels = collections.Counter(e["label"] for e in hf_labeled)
    print(f"\nHF label distribution:")
    print(f"  noise: {hf_labels['noise']}  low: {hf_labels['low']}  substantive: {hf_labels['substantive']}")

    # Response length stats per label
    for lbl in ["noise", "low", "substantive"]:
        lens = [len(e["assistant_response"]) for e in all_entries if e["label"] == lbl]
        if lens:
            avg = sum(lens) / len(lens)
            print(f"  {lbl} avg response len: {avg:.0f} chars (min={min(lens)}, max={max(lens)})")


if __name__ == "__main__":
    main()
