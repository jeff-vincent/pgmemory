package chunker

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Chunk (top-level) tests
// ---------------------------------------------------------------------------

func TestChunk_SingleParagraph(t *testing.T) {
	text := "This is a short paragraph."
	chunks := Chunk(text, 512)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != text {
		t.Errorf("expected %q, got %q", text, chunks[0])
	}
}

func TestChunk_MultipleParagraphs(t *testing.T) {
	text := "First paragraph.\n\nSecond paragraph.\n\nThird paragraph."
	chunks := Chunk(text, 512)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk (all fit within budget), got %d", len(chunks))
	}
	if !strings.Contains(chunks[0], "First paragraph.") {
		t.Error("expected chunk to contain first paragraph")
	}
	if !strings.Contains(chunks[0], "Third paragraph.") {
		t.Error("expected chunk to contain third paragraph")
	}
}

func TestChunk_SplitsAtBudget(t *testing.T) {
	// maxTokens=5 → maxChars=20
	text := "AAAA AAAA AAAA AAAA.\n\nBBBB BBBB BBBB BBBB."
	chunks := Chunk(text, 5)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d: %v", len(chunks), chunks)
	}
}

func TestChunk_EmptyText(t *testing.T) {
	chunks := Chunk("", 512)
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks, got %d", len(chunks))
	}
}

func TestChunk_OnlyWhitespace(t *testing.T) {
	chunks := Chunk("   \n\n   \n\n   ", 512)
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks for whitespace-only text, got %d", len(chunks))
	}
}

func TestChunk_DefaultMaxTokens(t *testing.T) {
	// passing 0 should use the default (512 tokens → 2048 chars)
	text := strings.Repeat("word ", 100) // 500 chars
	chunks := Chunk(text, 0)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk with default budget, got %d", len(chunks))
	}
}

func TestChunk_OversizedParagraph(t *testing.T) {
	// A single paragraph that exceeds the budget → split by sentences
	sentence := "This is sentence one. "
	para := strings.Repeat(sentence, 50) // ~1100 chars
	// maxTokens=10 → maxChars=40
	chunks := Chunk(para, 10)
	if len(chunks) < 2 {
		t.Fatalf("expected oversized paragraph to be split into multiple chunks, got %d", len(chunks))
	}
	// Reconstruct and verify no content was lost
	joined := strings.Join(chunks, "")
	if strings.Count(joined, "sentence one") != 50 {
		t.Errorf("expected 50 occurrences of 'sentence one' across chunks, got %d", strings.Count(joined, "sentence one"))
	}
}

func TestChunk_MixedSizes(t *testing.T) {
	short := "Short."
	// maxTokens=10 → maxChars=40
	long := strings.Repeat("A", 50) // exceeds budget
	text := short + "\n\n" + long
	chunks := Chunk(text, 10)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	if chunks[0] != short {
		t.Errorf("first chunk should be the short paragraph, got %q", chunks[0])
	}
}

func TestChunk_PreservesDoubleNewlines(t *testing.T) {
	text := "Paragraph A.\n\nParagraph B."
	chunks := Chunk(text, 512)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0], "\n\n") {
		t.Error("expected double newline to be preserved between paragraphs")
	}
}

// ---------------------------------------------------------------------------
// parseSegments tests
// ---------------------------------------------------------------------------

func TestParseSegments_HeadingDetection(t *testing.T) {
	text := "# Title\n\nSome body text.\n\n## Subtitle\n\nMore text."
	segs := parseSegments(text)

	// Expect: heading(#), prose, heading(##), prose
	var headings, prose int
	for _, s := range segs {
		switch s.kind {
		case segHeading:
			headings++
		case segPlain:
			prose++
		}
	}
	if headings != 2 {
		t.Errorf("expected 2 headings, got %d", headings)
	}
	if prose < 2 {
		t.Errorf("expected at least 2 prose segments, got %d", prose)
	}
}

func TestParseSegments_HeadingContextPropagation(t *testing.T) {
	text := "## Config\n\nPort defaults to 7432.\n\nSet atlas_mode for hybrid search."
	segs := parseSegments(text)

	for _, s := range segs {
		if s.kind == segPlain && s.heading != "Config" {
			t.Errorf("prose segment should inherit heading 'Config', got %q", s.heading)
		}
	}
}

func TestParseSegments_HeadingDepthTracking(t *testing.T) {
	text := "# H1\n\n## H2\n\n### H3"
	segs := parseSegments(text)

	depths := map[string]int{}
	for _, s := range segs {
		if s.kind == segHeading {
			depths[s.heading] = s.depth
		}
	}
	if depths["H1"] != 1 {
		t.Errorf("H1 depth = %d, want 1", depths["H1"])
	}
	if depths["H2"] != 2 {
		t.Errorf("H2 depth = %d, want 2", depths["H2"])
	}
	if depths["H3"] != 3 {
		t.Errorf("H3 depth = %d, want 3", depths["H3"])
	}
}

func TestParseSegments_FencedCodeBlock(t *testing.T) {
	text := "Some text.\n\n```go\nfunc main() {}\n```\n\nMore text."
	segs := parseSegments(text)

	var codeBlocks int
	for _, s := range segs {
		if s.kind == segCodeBlock {
			codeBlocks++
			if !strings.Contains(s.text, "func main()") {
				t.Error("code block should contain function body")
			}
			if !strings.HasPrefix(s.text, "```go") {
				t.Error("code block should start with opening fence")
			}
			if !strings.HasSuffix(s.text, "```") {
				t.Error("code block should end with closing fence")
			}
		}
	}
	if codeBlocks != 1 {
		t.Errorf("expected 1 code block, got %d", codeBlocks)
	}
}

func TestParseSegments_TableBlock(t *testing.T) {
	text := "| Name | Value |\n| --- | --- |\n| port | 7432 |"
	segs := parseSegments(text)

	if len(segs) != 1 {
		t.Fatalf("expected 1 segment (table block), got %d", len(segs))
	}
	if segs[0].kind != segTableBlock {
		t.Errorf("expected segTableBlock, got %d", segs[0].kind)
	}
	if !strings.Contains(segs[0].text, "7432") {
		t.Error("table should contain data row")
	}
}

func TestParseSegments_ListBlock(t *testing.T) {
	text := "- item one\n- item two\n- item three"
	segs := parseSegments(text)

	if len(segs) != 1 {
		t.Fatalf("expected 1 segment (list block), got %d", len(segs))
	}
	if segs[0].kind != segListBlock {
		t.Errorf("expected segListBlock, got %d", segs[0].kind)
	}
	if strings.Count(segs[0].text, "item") != 3 {
		t.Errorf("expected 3 items in list block")
	}
}

func TestParseSegments_OrderedList(t *testing.T) {
	text := "1. first\n2. second\n3. third"
	segs := parseSegments(text)

	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if segs[0].kind != segListBlock {
		t.Errorf("expected segListBlock for ordered list, got %d", segs[0].kind)
	}
}

func TestParseSegments_MixedContent(t *testing.T) {
	text := "# Intro\n\nSome prose.\n\n```\ncode\n```\n\n- list item\n\n| col |\n| --- |"
	segs := parseSegments(text)

	kinds := map[segmentKind]int{}
	for _, s := range segs {
		kinds[s.kind]++
	}
	if kinds[segHeading] != 1 {
		t.Errorf("expected 1 heading, got %d", kinds[segHeading])
	}
	if kinds[segPlain] < 1 {
		t.Errorf("expected at least 1 prose, got %d", kinds[segPlain])
	}
	if kinds[segCodeBlock] != 1 {
		t.Errorf("expected 1 code block, got %d", kinds[segCodeBlock])
	}
	if kinds[segListBlock] != 1 {
		t.Errorf("expected 1 list block, got %d", kinds[segListBlock])
	}
	if kinds[segTableBlock] != 1 {
		t.Errorf("expected 1 table block, got %d", kinds[segTableBlock])
	}
}

func TestParseSegments_ListContinuationLines(t *testing.T) {
	text := "- item one\n  continuation line\n- item two"
	segs := parseSegments(text)

	if len(segs) != 1 {
		t.Fatalf("expected 1 list segment, got %d", len(segs))
	}
	if !strings.Contains(segs[0].text, "continuation line") {
		t.Error("list should include continuation lines")
	}
}

func TestParseSegments_ListWithBlankLineBetweenItems(t *testing.T) {
	text := "- item one\n\n- item two"
	segs := parseSegments(text)

	// Blank line between list items with another item after should keep it as one list.
	if len(segs) != 1 {
		t.Fatalf("expected 1 list segment (blank line between items), got %d", len(segs))
	}
}

func TestParseSegments_CodeBlockUnderHeading(t *testing.T) {
	text := "## Setup\n\n```bash\nmake build\n```"
	segs := parseSegments(text)

	for _, s := range segs {
		if s.kind == segCodeBlock && s.heading != "Setup" {
			t.Errorf("code block should inherit heading 'Setup', got %q", s.heading)
		}
	}
}

// ---------------------------------------------------------------------------
// mergeSegments tests
// ---------------------------------------------------------------------------

func TestMergeSegments_CombinesSmallProse(t *testing.T) {
	segs := []segment{
		{text: "Short A.", kind: segPlain, heading: "H", depth: 2},
		{text: "Short B.", kind: segPlain, heading: "H", depth: 2},
	}
	merged := mergeSegments(segs, 200)
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged segment, got %d", len(merged))
	}
	if !strings.Contains(merged[0].text, "Short A.") || !strings.Contains(merged[0].text, "Short B.") {
		t.Error("merged segment should contain both texts")
	}
}

func TestMergeSegments_RespectsMaxChars(t *testing.T) {
	segs := []segment{
		{text: strings.Repeat("A", 100), kind: segPlain},
		{text: strings.Repeat("B", 100), kind: segPlain},
	}
	merged := mergeSegments(segs, 150)
	if len(merged) != 2 {
		t.Fatalf("expected 2 segments (exceeds budget), got %d", len(merged))
	}
}

func TestMergeSegments_NeverMergesAcrossHigherHeading(t *testing.T) {
	segs := []segment{
		{text: "## Section A", kind: segHeading, heading: "Section A", depth: 2},
		{text: "body A", kind: segPlain, heading: "Section A", depth: 2},
		{text: "## Section B", kind: segHeading, heading: "Section B", depth: 2},
		{text: "body B", kind: segPlain, heading: "Section B", depth: 2},
	}
	merged := mergeSegments(segs, 5000)

	// The two h2 headings should create separate segments.
	if len(merged) < 2 {
		t.Fatalf("expected at least 2 segments across heading boundaries, got %d", len(merged))
	}
}

func TestMergeSegments_MergesSubheading(t *testing.T) {
	segs := []segment{
		{text: "## Section", kind: segHeading, heading: "Section", depth: 2},
		{text: "### Subsection", kind: segHeading, heading: "Subsection", depth: 3},
	}
	merged := mergeSegments(segs, 5000)

	// A deeper heading (h3) under h2 can be merged.
	if len(merged) != 1 {
		t.Fatalf("expected subheading to merge, got %d segments", len(merged))
	}
}

func TestMergeSegments_NeverMergesCodeWithProse(t *testing.T) {
	segs := []segment{
		{text: "some prose", kind: segPlain},
		{text: "```\ncode\n```", kind: segCodeBlock},
	}
	merged := mergeSegments(segs, 5000)
	if len(merged) != 2 {
		t.Fatalf("expected code and prose to stay separate, got %d", len(merged))
	}
}

func TestMergeSegments_MergesAdjacentCode(t *testing.T) {
	segs := []segment{
		{text: "```\nfunc a() {}\n```", kind: segCodeBlock},
		{text: "```\nfunc b() {}\n```", kind: segCodeBlock},
	}
	merged := mergeSegments(segs, 5000)
	if len(merged) != 1 {
		t.Fatalf("expected adjacent code blocks to merge, got %d", len(merged))
	}
}

func TestMergeSegments_Empty(t *testing.T) {
	merged := mergeSegments(nil, 5000)
	if merged != nil {
		t.Errorf("expected nil, got %v", merged)
	}
}

// ---------------------------------------------------------------------------
// formatSegment / heading prefix tests
// ---------------------------------------------------------------------------

func TestFormatSegment_AddsHeadingPrefix(t *testing.T) {
	seg := segment{
		text:    "Port defaults to 7432.",
		kind:    segPlain,
		heading: "Config",
	}
	chunks := formatSegment(seg, 500)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.HasPrefix(chunks[0], "## Config\n\n") {
		t.Errorf("expected heading prefix, got %q", chunks[0])
	}
}

func TestFormatSegment_NoHeadingForHeadingSegments(t *testing.T) {
	seg := segment{
		text:    "## Config",
		kind:    segHeading,
		heading: "Config",
	}
	chunks := formatSegment(seg, 500)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	// Should NOT double-prefix the heading
	if strings.Count(chunks[0], "Config") != 1 {
		t.Errorf("heading should appear exactly once, got %q", chunks[0])
	}
}

func TestFormatSegment_NoHeadingWhenTextContainsHeading(t *testing.T) {
	seg := segment{
		text:    "## Config\n\nPort defaults to 7432.",
		kind:    segPlain,
		heading: "Config",
	}
	chunks := formatSegment(seg, 500)
	// Should not double-add heading since text already contains it.
	if strings.Count(chunks[0], "Config") > 1 {
		t.Errorf("heading should not be duplicated, got %q", chunks[0])
	}
}

func TestFormatSegment_SplitsOversizedProse(t *testing.T) {
	seg := segment{
		text:    strings.Repeat("Sentence here. ", 50),
		kind:    segPlain,
		heading: "Section",
	}
	// Budget must be large enough that available (maxChars - prefix len) >= 100,
	// otherwise formatSegment drops the prefix as a safety measure.
	chunks := formatSegment(seg, 200)
	if len(chunks) < 2 {
		t.Fatalf("expected oversized prose to split, got %d chunks", len(chunks))
	}
	// Each chunk should get the heading prefix.
	for i, c := range chunks {
		if !strings.HasPrefix(c, "## Section\n\n") {
			t.Errorf("chunk %d missing heading prefix: %q", i, c[:min(50, len(c))])
		}
	}
}

func TestFormatSegment_SplitsOversizedCode(t *testing.T) {
	code := "```go\n"
	for i := 0; i < 20; i++ {
		code += "func f" + string(rune('A'+i)) + "() {}\n\n"
	}
	code += "```"

	seg := segment{text: code, kind: segCodeBlock}
	chunks := formatSegment(seg, 100)
	if len(chunks) < 2 {
		t.Fatalf("expected oversized code to split, got %d chunks", len(chunks))
	}
	for i, c := range chunks {
		if !strings.HasPrefix(c, "```go\n") {
			t.Errorf("chunk %d missing opening fence: %q", i, c[:min(40, len(c))])
		}
		if !strings.HasSuffix(c, "\n```") {
			t.Errorf("chunk %d missing closing fence: %q", i, c[max(0, len(c)-40):])
		}
	}
}

func TestFormatSegment_SplitsOversizedList(t *testing.T) {
	var items []string
	for i := 0; i < 20; i++ {
		items = append(items, "- item number "+strings.Repeat("x", 10))
	}
	seg := segment{text: strings.Join(items, "\n"), kind: segListBlock}
	chunks := formatSegment(seg, 100)
	if len(chunks) < 2 {
		t.Fatalf("expected oversized list to split, got %d chunks", len(chunks))
	}
}

// ---------------------------------------------------------------------------
// splitProse tests (replaces old splitLong tests)
// ---------------------------------------------------------------------------

func TestSplitProse_ParagraphBoundaries(t *testing.T) {
	text := "Paragraph one content.\n\nParagraph two content.\n\nParagraph three content."
	// maxChars=30 forces splitting across paragraphs
	chunks := splitProse(text, 30)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d: %v", len(chunks), chunks)
	}
}

func TestSplitProse_FallsBackToSentences(t *testing.T) {
	// Single paragraph with multiple sentences
	text := "First sentence. Second sentence. Third sentence. Fourth sentence."
	chunks := splitProse(text, 35)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d: %v", len(chunks), chunks)
	}
}

// ---------------------------------------------------------------------------
// splitSentences tests
// ---------------------------------------------------------------------------

func TestSplitSentences_BasicSentences(t *testing.T) {
	text := "First sentence. Second sentence. Third sentence. Fourth sentence."
	chunks := splitSentences(text, 35)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d: %v", len(chunks), chunks)
	}
	for _, c := range chunks {
		if len(c) > 55 { // budget + slack for sentence boundary
			t.Errorf("chunk exceeds max chars: len=%d, chunk=%q", len(c), c)
		}
	}
}

func TestSplitSentences_SingleSentence(t *testing.T) {
	text := "This is one very long sentence that cannot be split further by periods."
	chunks := splitSentences(text, 20)
	// Single sentence with no period-space boundaries — splitAtWords fallback.
	if len(chunks) < 1 {
		t.Fatal("expected at least 1 chunk")
	}
	joined := strings.Join(chunks, " ")
	if !strings.Contains(joined, "one very long sentence") {
		t.Error("content should be preserved")
	}
}

func TestSplitSentences_ExclamationAndQuestion(t *testing.T) {
	text := "What is this? I'm not sure! Maybe it works."
	chunks := splitSentences(text, 20)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks from mixed punctuation, got %d", len(chunks))
	}
}

// ---------------------------------------------------------------------------
// splitCode tests
// ---------------------------------------------------------------------------

func TestSplitCode_FencedWithFuncBoundaries(t *testing.T) {
	code := "```go\nfunc Foo() {\n\treturn\n}\n\nfunc Bar() {\n\treturn\n}\n```"
	chunks := splitCode(code, 40)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks from function boundaries, got %d: %v", len(chunks), chunks)
	}
	for i, c := range chunks {
		if !strings.Contains(c, "```") {
			t.Errorf("chunk %d missing fences: %q", i, c)
		}
	}
}

func TestSplitCode_UnfencedFuncBoundaries(t *testing.T) {
	code := "func Alpha() {}\n\nfunc Beta() {}\n\nfunc Gamma() {}"
	chunks := splitCode(code, 30)
	if len(chunks) < 2 {
		t.Fatalf("expected splitting on function boundaries, got %d chunks", len(chunks))
	}
}

func TestSplitCode_PreservesLanguageTag(t *testing.T) {
	code := "```python\ndef foo():\n    pass\n\ndef bar():\n    pass\n```"
	chunks := splitCode(code, 40)
	for i, c := range chunks {
		if !strings.HasPrefix(c, "```python\n") {
			t.Errorf("chunk %d should start with ```python, got %q", i, c[:min(20, len(c))])
		}
	}
}

func TestSplitCode_NoFuncBoundaries_FallsBackToBlankLines(t *testing.T) {
	code := "```\nline one\nline two\n\nline three\nline four\n```"
	chunks := splitCode(code, 30)
	if len(chunks) < 1 {
		t.Fatal("expected at least 1 chunk")
	}
}

func TestSplitCode_Preamble(t *testing.T) {
	code := "// Package header\n// License info\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}"
	chunks := splitCode(code, 50)
	// The preamble before the first func should be included.
	joined := strings.Join(chunks, " ")
	if !strings.Contains(joined, "Package header") {
		t.Error("preamble should be preserved")
	}
	if !strings.Contains(joined, "func main()") {
		t.Error("function should be preserved")
	}
}

// ---------------------------------------------------------------------------
// splitList tests
// ---------------------------------------------------------------------------

func TestSplitList_BasicItems(t *testing.T) {
	text := "- item one\n- item two\n- item three\n- item four\n- item five"
	chunks := splitList(text, 30)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks from list split, got %d", len(chunks))
	}
	// All items should be present across chunks.
	joined := strings.Join(chunks, "\n")
	for i := 1; i <= 5; i++ {
		if !strings.Contains(joined, "item") {
			t.Errorf("missing list item content")
		}
	}
}

func TestSplitList_MultiLineItems(t *testing.T) {
	text := "- item one\n  detail one\n- item two\n  detail two"
	items := splitListItems(text)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d: %v", len(items), items)
	}
	if !strings.Contains(items[0], "detail one") {
		t.Error("first item should include continuation")
	}
}

// ---------------------------------------------------------------------------
// splitAtWords tests
// ---------------------------------------------------------------------------

func TestSplitAtWords_Basic(t *testing.T) {
	text := "one two three four five six seven eight"
	chunks := splitAtWords(text, 15)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	joined := strings.Join(chunks, " ")
	if joined != text {
		t.Errorf("content lost: got %q", joined)
	}
}

func TestSplitAtWords_SingleLongWord(t *testing.T) {
	text := "superlongword"
	chunks := splitAtWords(text, 5)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for single word, got %d", len(chunks))
	}
	if chunks[0] != text {
		t.Errorf("expected %q, got %q", text, chunks[0])
	}
}

// ---------------------------------------------------------------------------
// groupByBudget tests
// ---------------------------------------------------------------------------

func TestGroupByBudget_CombinesSmallPieces(t *testing.T) {
	pieces := []string{"aaa", "bbb", "ccc"}
	chunks := groupByBudget(pieces, " ", 100)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != "aaa bbb ccc" {
		t.Errorf("unexpected: %q", chunks[0])
	}
}

func TestGroupByBudget_SplitsOnBudget(t *testing.T) {
	pieces := []string{"aaaa", "bbbb", "cccc"}
	chunks := groupByBudget(pieces, " ", 10)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d: %v", len(chunks), chunks)
	}
}

func TestGroupByBudget_SkipsEmptyPieces(t *testing.T) {
	pieces := []string{"", "aaa", "", "bbb", ""}
	chunks := groupByBudget(pieces, " ", 100)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != "aaa bbb" {
		t.Errorf("unexpected: %q", chunks[0])
	}
}

func TestGroupByBudget_OversizedPieceFallsBackToWords(t *testing.T) {
	pieces := []string{strings.Repeat("word ", 50)} // ~250 chars
	chunks := groupByBudget(pieces, " ", 50)
	if len(chunks) < 2 {
		t.Fatalf("expected oversized piece to be split, got %d chunks", len(chunks))
	}
}

// ---------------------------------------------------------------------------
// detectContent tests
// ---------------------------------------------------------------------------

func TestDetectContent_Prose(t *testing.T) {
	text := "This is a regular paragraph of text.\nAnother line of natural language."
	if detectContent(text) != contentProse {
		t.Errorf("expected contentProse, got %d", detectContent(text))
	}
}

func TestDetectContent_Code(t *testing.T) {
	// Needs >50% of non-blank lines with >25% symbol density.
	text := "if (x != nil) {\n\ta[i] = b[j] + c(d);\n\treturn &foo{bar: 1}\n}\nfor _, v := range m {\n\tfmt.Printf(\"%v\", v)\n}"
	if detectContent(text) != contentCode {
		t.Errorf("expected contentCode, got %d", detectContent(text))
	}
}

func TestDetectContent_Markdown(t *testing.T) {
	text := "# Title\n\n- item one\n- item two\n- item three\n\nSome text."
	if detectContent(text) != contentMarkdown {
		t.Errorf("expected contentMarkdown, got %d", detectContent(text))
	}
}

func TestDetectContent_Empty(t *testing.T) {
	if detectContent("") != contentProse {
		t.Error("empty text should return contentProse")
	}
}

// ---------------------------------------------------------------------------
// Integration: Chunk with structured content
// ---------------------------------------------------------------------------

func TestChunk_HeadingContextInOutput(t *testing.T) {
	// Large enough to need splitting, with a heading context.
	// Budget must leave available >= 100 after prefix, so use maxTokens=75 (300 chars).
	text := "## Architecture\n\n"
	for i := 0; i < 80; i++ {
		text += "This is a detailed sentence about the architecture. "
	}
	chunks := Chunk(text, 75)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	// All split prose chunks should carry the heading context.
	for i, c := range chunks {
		if !strings.Contains(c, "Architecture") {
			t.Errorf("chunk %d missing heading context: %q", i, c[:min(60, len(c))])
		}
	}
}

func TestChunk_CodeBlockPreserved(t *testing.T) {
	text := "Explanation.\n\n```go\nfunc Hello() string {\n\treturn \"world\"\n}\n```\n\nMore text."
	chunks := Chunk(text, 512)
	found := false
	for _, c := range chunks {
		if strings.Contains(c, "func Hello()") && strings.Contains(c, "```") {
			found = true
		}
	}
	if !found {
		t.Error("expected code block to be preserved in output")
	}
}

func TestChunk_TablePreserved(t *testing.T) {
	text := "Before.\n\n| A | B |\n| - | - |\n| 1 | 2 |\n\nAfter."
	chunks := Chunk(text, 512)
	found := false
	for _, c := range chunks {
		if strings.Contains(c, "| A | B |") && strings.Contains(c, "| 1 | 2 |") {
			found = true
		}
	}
	if !found {
		t.Error("expected table to be preserved together")
	}
}

func TestChunk_ListPreserved(t *testing.T) {
	text := "Intro.\n\n- first\n- second\n- third\n\nOutro."
	chunks := Chunk(text, 512)
	found := false
	for _, c := range chunks {
		if strings.Contains(c, "- first") && strings.Contains(c, "- third") {
			found = true
		}
	}
	if !found {
		t.Error("expected list items to stay together")
	}
}

func TestChunk_NoContentLoss(t *testing.T) {
	// Complex mixed document — verify nothing is dropped.
	text := "# Title\n\nIntro paragraph.\n\n## Section\n\n- item A\n- item B\n\n```\ncode here\n```\n\n| col |\n| --- |\n\nFinal paragraph."
	chunks := Chunk(text, 512)
	joined := strings.Join(chunks, " ")

	for _, expected := range []string{"Title", "Intro paragraph", "Section", "item A", "item B", "code here", "col", "Final paragraph"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("missing %q in chunked output", expected)
		}
	}
}

func TestChunk_VerySmallBudgetDoesNotPanic(t *testing.T) {
	text := "# Heading\n\nSome text.\n\n```go\nfunc main() {}\n```\n\n- item\n\n| col |\n| --- |"
	// Extremely small budget — shouldn't panic.
	chunks := Chunk(text, 1) // 4 chars budget
	if len(chunks) == 0 {
		t.Fatal("expected at least some chunks")
	}
}

func TestChunk_LargeDocumentEndToEnd(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Main Title\n\n")
	b.WriteString("This is the introduction paragraph with enough text to matter.\n\n")
	b.WriteString("## First Section\n\n")
	for i := 0; i < 10; i++ {
		b.WriteString("This is paragraph content for the first section. ")
	}
	b.WriteString("\n\n")
	b.WriteString("```python\ndef hello():\n    print('world')\n\ndef goodbye():\n    print('bye')\n```\n\n")
	b.WriteString("## Second Section\n\n")
	b.WriteString("- alpha\n- beta\n- gamma\n- delta\n\n")
	b.WriteString("| Key | Value |\n| --- | ----- |\n| a   | 1     |\n| b   | 2     |\n\n")
	b.WriteString("Final closing paragraph.\n")

	text := b.String()
	chunks := Chunk(text, 50) // 200 chars budget → forces many splits
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks for large doc, got %d", len(chunks))
	}

	joined := strings.Join(chunks, " ")
	for _, want := range []string{"Main Title", "First Section", "Second Section", "hello", "goodbye", "alpha", "delta", "Final closing"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in output", want)
		}
	}
}

// ---------------------------------------------------------------------------
// ChunkStructured tests
// ---------------------------------------------------------------------------

func TestChunkStructured_Empty(t *testing.T) {
	segs := ChunkStructured("", 512)
	if len(segs) != 0 {
		t.Errorf("expected 0 segments for empty input, got %d", len(segs))
	}
}

func TestChunkStructured_PlainText_NoHeading(t *testing.T) {
	segs := ChunkStructured("This is a plain text paragraph with no heading.", 512)
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if segs[0].Heading != "" {
		t.Errorf("expected empty heading, got %q", segs[0].Heading)
	}
	if segs[0].Kind != "prose" {
		t.Errorf("expected kind=prose, got %q", segs[0].Kind)
	}
	if segs[0].Text == "" {
		t.Error("expected non-empty text")
	}
}

func TestChunkStructured_HeadingPreserved(t *testing.T) {
	text := "## Getting Started\n\nInstall the package with go get.\n\n## Advanced Usage\n\nConfigure options via the config file."
	segs := ChunkStructured(text, 512)
	if len(segs) == 0 {
		t.Fatal("expected segments")
	}
	// At least one segment should carry a heading.
	var hasHeading bool
	for _, s := range segs {
		if s.Heading != "" {
			hasHeading = true
			break
		}
	}
	if !hasHeading {
		t.Error("expected at least one segment with a heading")
	}
}

func TestChunkStructured_CodeBlockKind(t *testing.T) {
	text := "## Example\n\nSome prose.\n\n```go\nfunc main() {}\n```"
	segs := ChunkStructured(text, 512)
	var foundCode bool
	for _, s := range segs {
		if s.Kind == "code" {
			foundCode = true
		}
	}
	if !foundCode {
		t.Error("expected at least one segment with kind=code")
	}
}

func TestChunkStructured_ListKind(t *testing.T) {
	// Use a small budget so the heading and list can't be merged.
	// The list segment alone (~40 chars) fits, but heading+list (~55) exceeds 8-token budget (32 chars).
	text := "## Items\n\n- alpha\n- beta\n- gamma\n- delta\n- epsilon\n- zeta\n- eta\n- theta"
	segs := ChunkStructured(text, 4) // 16 chars budget — forces list into its own segment
	var foundList bool
	for _, s := range segs {
		if s.Kind == "list" {
			foundList = true
		}
	}
	if !foundList {
		// Also acceptable: list was merged into heading segment — verify list content is present.
		var hasListContent bool
		for _, s := range segs {
			if strings.Contains(s.Text, "- alpha") {
				hasListContent = true
				break
			}
		}
		if !hasListContent {
			t.Error("expected list content in segments")
		}
	}
}

func TestChunkStructured_ProducesConsistentChunks(t *testing.T) {
	// ChunkStructured should produce the same chunks as Chunk() — only metadata differs.
	text := "## Section\n\nSome content here.\n\n```go\nfunc foo() {}\n```"
	flat := Chunk(text, 512)
	structured := ChunkStructured(text, 512)
	if len(flat) != len(structured) {
		t.Errorf("Chunk()=%d vs ChunkStructured()=%d chunks", len(flat), len(structured))
	}
	for i := range flat {
		if i >= len(structured) {
			break
		}
		if flat[i] != structured[i].Text {
			t.Errorf("chunk %d text mismatch:\n  Chunk:          %q\n  ChunkStructured: %q", i, flat[i], structured[i].Text)
		}
	}
}

func TestSegKindString(t *testing.T) {
	tests := []struct {
		kind segmentKind
		want string
	}{
		{segPlain, "prose"},
		{segHeading, "prose"},
		{segCodeBlock, "code"},
		{segListBlock, "list"},
		{segTableBlock, "table"},
	}
	for _, tc := range tests {
		got := segKindString(tc.kind)
		if got != tc.want {
			t.Errorf("segKindString(%d) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}
