#!/usr/bin/env python3
"""Patch SynthesizeQA to handle empty questions and add SKIP rules."""

with open("internal/synthesizer/synthesizer.go", "r") as f:
    content = f.read()

# Restore from backup first if needed
if "exchangeBlock" in content:
    print("Already patched, restoring from backup first")
    with open("internal/synthesizer/synthesizer.go.bak", "r") as f:
        content = f.read()

old_start = "// SynthesizeQA distills a single user question + assistant answer into a"
old_end = "Output directly. No preamble.`, question, answer)"

start_idx = content.index(old_start)
end_idx = content.index(old_end) + len(old_end)

new_func = (
    '// SynthesizeQA distills a user question + assistant answer (or assistant-only\n'
    '// text) into a memory entry, or returns ("", nil) if it has no durable value.\n'
    '//\n'
    '// This is the mandatory quality gate for ALL proxy-captured content. The model\n'
    '// returns the sentinel "SKIP" for procedural exchanges ("I\'ll look at that",\n'
    '// "I\'ve made the changes") that carry no reusable knowledge.\n'
    '//\n'
    '// When question is empty (proxy couldn\'t extract a user message), the assistant\n'
    '// text is evaluated on its own merits \u2014 the same quality bar applies.\n'
    'func (s *Synthesizer) SynthesizeQA(ctx context.Context, question, answer string) (string, error) {\n'
    '\tif !s.Available() {\n'
    '\t\treturn "", nil\n'
    '\t}\n'
    '\n'
    '\t// When no user message is available, evaluate the assistant output alone\n'
    '\t// rather than framing it as a conversation with an empty USER: line.\n'
    '\tvar exchangeBlock string\n'
    '\tif strings.TrimSpace(question) != "" {\n'
    '\t\texchangeBlock = fmt.Sprintf("USER: %s\\n\\nASSISTANT: %s", question, answer)\n'
    '\t} else {\n'
    '\t\texchangeBlock = fmt.Sprintf("ASSISTANT OUTPUT:\\n%s", answer)\n'
    '\t}\n'
    '\n'
    '\tprompt := fmt.Sprintf(`You are a memory curator for an AI coding assistant. Assess whether this contains durable technical knowledge \u2014 and if so, distill it to its essence.\n'
    '\n'
    'A memory has durable value if a future AI instance starting fresh on this project would benefit: it saves exploration time, prevents a mistake, or answers a non-obvious question about the system that isn\u2019t obvious from reading the code.\n'
    '\n'
    '---\n'
    '%s\n'
    '---\n'
    '\n'
    'Does this contain any of:\n'
    '- A specific technical discovery (file location, config value, API behavior, architectural fact)\n'
    '- A decision made with rationale (why X was chosen over Y)\n'
    '- A non-obvious constraint, gotcha, or failure mode\n'
    '- A concrete implementation pattern with specific names (not just "I made the changes")\n'
    '\n'
    'If none of the above \u2014 the text is procedural narration, mid-task navigation, a generic explanation, or a status update \u2014 respond with exactly: SKIP\n'
    '\n'
    'Otherwise output a memory entry:\n'
    '\n'
    '**[module/area] specific topic**\n'
    '- fact with exact name (file path, function name, config key, error message)\n'
    '- decision: X over Y \u2014 because Z\n'
    '- gotcha: X breaks when Y \u2014 workaround is Z\n'
    '(3\u201310 bullets maximum; each bullet must be self-contained)\n'
    '\n'
    'Rules:\n'
    '- Never include "I looked at X" or "I made changes to Y" \u2014 extract the finding, not the process\n'
    '- If the text explains what was done without a specific technical anchor, it is SKIP\n'
    '- "Now fix...", "Let me check...", "Let me look at..." with no conclusion = SKIP\n'
    '- Include the "why" for any decision\n'
    '- Prefer file:line references and exact identifiers over prose descriptions\n'
    '\n'
    'Output directly. No preamble.`, exchangeBlock)'
)

content = content[:start_idx] + new_func + content[end_idx:]

with open("internal/synthesizer/synthesizer.go", "w") as f:
    f.write(content)

print("Done - SynthesizeQA patched successfully")
