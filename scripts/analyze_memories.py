#!/usr/bin/env python3
"""Analyze memory quality from memory_list JSON dump."""
import json, sys, collections

data = json.load(sys.stdin)

# Source distribution
sources = collections.Counter(m['source'] for m in data)
print('=== SOURCE DISTRIBUTION ===')
for src, cnt in sources.most_common():
    print(f'  {src}: {cnt}')

# Split by benchmark vs. other
bench = [m for m in data if m['source'].startswith('benchmark')]
others = [m for m in data if not m['source'].startswith('benchmark')]

def score_stats(label, entries):
    if not entries:
        return
    scores = [m.get('content_score', 0.0) for m in entries]
    scores.sort()
    n = len(scores)
    print(f'\n=== {label} ({n}) ===')
    print(f'  Score range: {min(scores):.3f} - {max(scores):.3f}')
    print(f'  Mean: {sum(scores)/n:.3f}')
    print(f'  Median: {scores[n//2]:.3f}')
    print(f'  P10: {scores[int(n*0.1)]:.3f}  P25: {scores[int(n*0.25)]:.3f}  P75: {scores[int(n*0.75)]:.3f}  P90: {scores[int(n*0.9)]:.3f}')

score_stats('BENCHMARK ENTRIES', bench)
score_stats('NON-BENCHMARK ENTRIES', others)

# Categorize junk patterns in benchmark entries
print('\n=== JUNK PATTERN ANALYSIS (benchmark) ===')
patterns = {
    'permission_check': 0,
    'grep_output_xml': 0,
    'command_output': 0,
    'let_me_search': 0,
    'codebase_exploration': 0,
    'implementation_detail': 0,
    'warmup_boilerplate': 0,
    'other': 0,
}
examples = collections.defaultdict(list)

for m in bench:
    c = m['content'].lower()
    if 'check if i have the permissions' in c or 'permission' in c and 'repository' in c:
        patterns['permission_check'] += 1
        examples['permission_check'].append(m)
    elif '<is_displaying_contents>' in c or '<filepaths>' in c:
        patterns['grep_output_xml'] += 1
        examples['grep_output_xml'].append(m)
    elif 'command:' in c[:50].lower() and ('grep' in c[:100].lower() or 'output:' in c[:200].lower()):
        patterns['command_output'] += 1
        examples['command_output'].append(m)
    elif 'let me' in c[:80] and ('search' in c[:150] or 'check' in c[:150] or 'look' in c[:150] or 'try' in c[:150] or 'examine' in c[:150]):
        patterns['let_me_search'] += 1
        examples['let_me_search'].append(m)
    elif 'warmup' in c[:100] or "i'll explore" in c[:100] or "i'll search" in c[:100]:
        patterns['warmup_boilerplate'] += 1
        examples['warmup_boilerplate'].append(m)
    elif 'hyperswitch' in c or '/workspace/archit' in c:
        patterns['codebase_exploration'] += 1
        examples['codebase_exploration'].append(m)
    elif any(kw in c for kw in ['implementation', 'based on my', 'key findings', 'here\'s what']):
        patterns['implementation_detail'] += 1
        examples['implementation_detail'].append(m)
    else:
        patterns['other'] += 1
        examples['other'].append(m)

for pattern, count in sorted(patterns.items(), key=lambda x: -x[1]):
    if count > 0:
        ex = examples[pattern][0]
        preview = ex['content'][:120].replace('\n', ' ')
        print(f'\n  {pattern}: {count} entries (score range: {min(e.get("content_score", 0.0) for e in examples[pattern]):.3f} - {max(e.get("content_score", 0.0) for e in examples[pattern]):.3f})')
        print(f'    Example: {preview}...')

# Show non-benchmark entries
if others:
    print('\n=== NON-BENCHMARK ENTRIES ===')
    for m in others[:20]:
        preview = m['content'][:150].replace('\n', ' ')
        print(f'\n  [{m["source"]}] score={m.get("content_score", 0.0):.3f}')
        print(f'    {preview}...')
