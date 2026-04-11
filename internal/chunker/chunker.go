package chunker

import (
	"regexp"
	"strings"
	"unicode"
)

// DefaultMaxTokens is the target ceiling per chunk.
const DefaultMaxTokens = 512

const charsPerToken = 4 // rough approximation

// contentType classifies text for strategy selection.
type contentType int

const (
	contentProse        contentType = iota
	contentCode                     // fenced code or indented blocks
	contentMarkdown                 // headings, lists, tables
	contentConversation             // chat-style turn-taking
)

// segment is an atomic unit of text with structural metadata.
type segment struct {
	text    string
	kind    segmentKind
	heading string // nearest heading above this segment
	depth   int    // heading depth (1-6) or 0
}

type segmentKind int

const (
	segPlain segmentKind = iota
	segHeading
	segCodeBlock
	segListBlock
	segTableBlock
)

var (
	headingRe    = regexp.MustCompile(`^(#{1,6})\s+(.+)`)
	codeBlockRe  = regexp.MustCompile("^```")
	listItemRe   = regexp.MustCompile(`^(\s*[-*+]|\s*\d+[.)]\s)`)
	tableRowRe   = regexp.MustCompile(`^\|.*\|`)
	funcBoundary = regexp.MustCompile(`(?m)^(func |def |class |type |export |const |var |impl |pub fn |fn |module |package |interface |struct |enum )`)
)

// Chunk splits text into semantically coherent pieces under the token budget.
// It detects content structure (headings, code blocks, lists, tables) and
// preserves logical boundaries. Each chunk carries its nearest heading as
// context prefix when split under a heading.
func Chunk(text string, maxTokens int) []string {
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	maxChars := maxTokens * charsPerToken

	// Step 1: Parse into structural segments.
	segments := parseSegments(text)

	// Step 2: Merge adjacent compatible segments within budget.
	merged := mergeSegments(segments, maxChars)

	// Step 3: Split any oversized segments and apply heading context.
	var chunks []string
	for _, seg := range merged {
		formatted := formatSegment(seg, maxChars)
		chunks = append(chunks, formatted...)
	}

	return chunks
}

// parseSegments splits text into typed structural segments, tracking headings.
func parseSegments(text string) []segment {
	lines := strings.Split(text, "\n")
	var segments []segment
	var currentHeading string
	var currentDepth int

	i := 0
	for i < len(lines) {
		line := lines[i]

		// Heading
		if m := headingRe.FindStringSubmatch(line); m != nil {
			currentDepth = len(m[1])
			currentHeading = m[2]
			segments = append(segments, segment{
				text:    line,
				kind:    segHeading,
				heading: currentHeading,
				depth:   currentDepth,
			})
			i++
			continue
		}

		// Fenced code block — consume until closing fence.
		if codeBlockRe.MatchString(strings.TrimSpace(line)) {
			block, end := consumeCodeBlock(lines, i)
			segments = append(segments, segment{
				text:    block,
				kind:    segCodeBlock,
				heading: currentHeading,
				depth:   currentDepth,
			})
			i = end
			continue
		}

		// Table block — consecutive pipe-delimited lines.
		if tableRowRe.MatchString(strings.TrimSpace(line)) {
			block, end := consumeTable(lines, i)
			segments = append(segments, segment{
				text:    block,
				kind:    segTableBlock,
				heading: currentHeading,
				depth:   currentDepth,
			})
			i = end
			continue
		}

		// List block — consecutive lines starting with list markers.
		if listItemRe.MatchString(line) {
			block, end := consumeList(lines, i)
			segments = append(segments, segment{
				text:    block,
				kind:    segListBlock,
				heading: currentHeading,
				depth:   currentDepth,
			})
			i = end
			continue
		}

		// Plain text — consume until a structural boundary.
		block, end := consumeProse(lines, i)
		if strings.TrimSpace(block) != "" {
			segments = append(segments, segment{
				text:    block,
				kind:    segPlain,
				heading: currentHeading,
				depth:   currentDepth,
			})
		}
		i = end
	}

	return segments
}

// consumeCodeBlock consumes a fenced code block, returning the text and the
// line index after the block.
func consumeCodeBlock(lines []string, start int) (string, int) {
	var buf strings.Builder
	buf.WriteString(lines[start])
	i := start + 1
	for i < len(lines) {
		buf.WriteString("\n")
		buf.WriteString(lines[i])
		if codeBlockRe.MatchString(strings.TrimSpace(lines[i])) && i > start {
			i++
			break
		}
		i++
	}
	return buf.String(), i
}

// consumeTable consumes consecutive table rows.
func consumeTable(lines []string, start int) (string, int) {
	var buf strings.Builder
	i := start
	for i < len(lines) && tableRowRe.MatchString(strings.TrimSpace(lines[i])) {
		if buf.Len() > 0 {
			buf.WriteString("\n")
		}
		buf.WriteString(lines[i])
		i++
	}
	return buf.String(), i
}

// consumeList consumes a contiguous list block, including continuation lines.
func consumeList(lines []string, start int) (string, int) {
	var buf strings.Builder
	i := start
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		// A list continues if the line is a list item, a continuation (indented),
		// or a blank line followed by another list item.
		if listItemRe.MatchString(line) || (trimmed != "" && (strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t"))) {
			if buf.Len() > 0 {
				buf.WriteString("\n")
			}
			buf.WriteString(line)
			i++
			continue
		}
		// Blank line — peek ahead to see if list continues.
		if trimmed == "" && i+1 < len(lines) && listItemRe.MatchString(lines[i+1]) {
			buf.WriteString("\n")
			i++
			continue
		}
		break
	}
	return buf.String(), i
}

// consumeProse consumes plain text paragraphs until a structural marker.
func consumeProse(lines []string, start int) (string, int) {
	var buf strings.Builder
	i := start
	blankCount := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Stop at structural markers.
		if headingRe.MatchString(line) || codeBlockRe.MatchString(trimmed) ||
			tableRowRe.MatchString(trimmed) || listItemRe.MatchString(line) {
			break
		}

		if trimmed == "" {
			blankCount++
			// Two consecutive blanks = strong paragraph break — stop.
			if blankCount >= 2 {
				i++
				break
			}
			if buf.Len() > 0 {
				buf.WriteString("\n\n")
			}
			i++
			continue
		}

		blankCount = 0
		if buf.Len() > 0 && !strings.HasSuffix(buf.String(), "\n\n") {
			buf.WriteString("\n")
		}
		buf.WriteString(line)
		i++
	}

	return strings.TrimSpace(buf.String()), i
}

// mergeSegments combines adjacent compatible segments that fit within the budget.
// Segments are compatible when they share the same heading context and the
// second is not a new heading of equal or higher level.
func mergeSegments(segments []segment, maxChars int) []segment {
	if len(segments) == 0 {
		return nil
	}

	merged := []segment{segments[0]}
	for i := 1; i < len(segments); i++ {
		prev := &merged[len(merged)-1]
		cur := segments[i]

		// Never merge across heading boundaries of equal or higher level.
		if cur.kind == segHeading && cur.depth <= prev.depth {
			merged = append(merged, cur)
			continue
		}

		// Don't merge code blocks with non-code (keeps code self-contained).
		if (prev.kind == segCodeBlock) != (cur.kind == segCodeBlock) {
			merged = append(merged, cur)
			continue
		}

		// Check if merging would fit.
		combinedLen := len(prev.text) + 1 + len(cur.text)
		if combinedLen > maxChars {
			merged = append(merged, cur)
			continue
		}

		// Merge: heading segments get absorbed as context.
		sep := "\n\n"
		if cur.kind == segHeading {
			sep = "\n"
		}
		prev.text = prev.text + sep + cur.text
		// Promote heading if the merged segment includes one.
		if cur.heading != "" {
			prev.heading = cur.heading
		}
	}

	return merged
}

// formatSegment takes a (possibly oversized) segment and returns one or more
// chunks, each prefixed with heading context if applicable.
func formatSegment(seg segment, maxChars int) []string {
	// Calculate available space after heading prefix.
	prefix := ""
	if seg.heading != "" && seg.kind != segHeading &&
		!strings.Contains(seg.text, seg.heading) {
		prefix = "## " + seg.heading + "\n\n"
	}

	available := maxChars - len(prefix)
	if available < 100 {
		available = maxChars
		prefix = ""
	}

	text := seg.text
	if len(text) <= available {
		return []string{prefix + text}
	}

	// Oversized — split using type-appropriate strategy.
	var parts []string
	switch seg.kind {
	case segCodeBlock:
		parts = splitCode(text, available)
	case segListBlock:
		parts = splitList(text, available)
	default:
		parts = splitProse(text, available)
	}

	// Apply heading prefix to each part.
	if prefix != "" {
		for i := range parts {
			parts[i] = prefix + parts[i]
		}
	}
	return parts
}

// splitProse splits prose at paragraph and sentence boundaries.
func splitProse(text string, maxChars int) []string {
	// Try paragraph boundaries first.
	paragraphs := strings.Split(text, "\n\n")
	if len(paragraphs) > 1 {
		return groupByBudget(paragraphs, "\n\n", maxChars)
	}
	// Fall back to sentence boundaries.
	return splitSentences(text, maxChars)
}

// splitSentences breaks text on sentence-ending punctuation.
func splitSentences(text string, maxChars int) []string {
	// Split on sentence-ending punctuation followed by space or newline.
	sentenceEnders := regexp.MustCompile(`([.!?])\s+`)
	parts := sentenceEnders.Split(text, -1)
	delimiters := sentenceEnders.FindAllString(text, -1)

	// Reconstruct sentences with their terminators.
	var sentences []string
	for i, p := range parts {
		s := p
		if i < len(delimiters) {
			s += delimiters[i]
		}
		s = strings.TrimSpace(s)
		if s != "" {
			sentences = append(sentences, s)
		}
	}

	if len(sentences) == 0 {
		// No sentence boundaries — hard split at word boundaries.
		return splitAtWords(text, maxChars)
	}

	return groupByBudget(sentences, " ", maxChars)
}

// splitCode splits a code block at function/class/type boundaries.
func splitCode(text string, maxChars int) []string {
	// Check for fenced code — extract the fences and language tag.
	lines := strings.Split(text, "\n")
	fence := ""
	lang := ""
	inner := text

	if len(lines) >= 2 && codeBlockRe.MatchString(strings.TrimSpace(lines[0])) {
		fence = strings.TrimSpace(lines[0])
		lang = strings.TrimPrefix(fence, "```")
		// Find closing fence.
		closeIdx := len(lines) - 1
		for i := len(lines) - 1; i > 0; i-- {
			if codeBlockRe.MatchString(strings.TrimSpace(lines[i])) {
				closeIdx = i
				break
			}
		}
		inner = strings.Join(lines[1:closeIdx], "\n")
	}

	// Split at top-level declaration boundaries.
	locs := funcBoundary.FindAllStringIndex(inner, -1)
	if len(locs) <= 1 {
		// No internal boundaries — fall back to blank-line splitting.
		parts := strings.Split(inner, "\n\n")
		chunks := groupByBudget(parts, "\n\n", maxChars-len(fence)*2-10)
		if fence != "" {
			for i := range chunks {
				chunks[i] = "```" + lang + "\n" + chunks[i] + "\n```"
			}
		}
		return chunks
	}

	// Group declarations into chunks under budget.
	var blocks []string
	for i, loc := range locs {
		end := len(inner)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		block := strings.TrimSpace(inner[loc[0]:end])
		if block != "" {
			blocks = append(blocks, block)
		}
	}

	// Include any preamble before the first declaration.
	if locs[0][0] > 0 {
		preamble := strings.TrimSpace(inner[:locs[0][0]])
		if preamble != "" {
			blocks = append([]string{preamble}, blocks...)
		}
	}

	chunks := groupByBudget(blocks, "\n\n", maxChars-len(fence)*2-10)
	if fence != "" {
		for i := range chunks {
			chunks[i] = "```" + lang + "\n" + chunks[i] + "\n```"
		}
	}
	return chunks
}

// splitList splits a list block at item boundaries.
func splitList(text string, maxChars int) []string {
	items := splitListItems(text)
	return groupByBudget(items, "\n", maxChars)
}

// splitListItems breaks a list into individual items (may be multi-line).
func splitListItems(text string) []string {
	lines := strings.Split(text, "\n")
	var items []string
	var current strings.Builder

	for _, line := range lines {
		if listItemRe.MatchString(line) && current.Len() > 0 {
			items = append(items, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		items = append(items, current.String())
	}
	return items
}

// groupByBudget groups pieces into chunks that fit within maxChars.
func groupByBudget(pieces []string, sep string, maxChars int) []string {
	var chunks []string
	var buf strings.Builder

	for _, piece := range pieces {
		piece = strings.TrimSpace(piece)
		if piece == "" {
			continue
		}

		newLen := buf.Len() + len(sep) + len(piece)
		if buf.Len() > 0 && newLen > maxChars {
			chunks = append(chunks, strings.TrimSpace(buf.String()))
			buf.Reset()
		}

		if buf.Len() > 0 {
			buf.WriteString(sep)
		}
		buf.WriteString(piece)
	}

	if buf.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(buf.String()))
	}

	// Handle oversized individual pieces.
	var result []string
	for _, chunk := range chunks {
		if len(chunk) > maxChars+100 { // small tolerance
			result = append(result, splitAtWords(chunk, maxChars)...)
		} else {
			result = append(result, chunk)
		}
	}
	return result
}

// splitAtWords is the last-resort splitter for text with no structural boundaries.
func splitAtWords(text string, maxChars int) []string {
	words := strings.Fields(text)
	var chunks []string
	var buf strings.Builder

	for _, w := range words {
		if buf.Len()+1+len(w) > maxChars && buf.Len() > 0 {
			chunks = append(chunks, buf.String())
			buf.Reset()
		}
		if buf.Len() > 0 {
			buf.WriteString(" ")
		}
		buf.WriteString(w)
	}
	if buf.Len() > 0 {
		chunks = append(chunks, buf.String())
	}
	return chunks
}

// ChunkedSegment is a chunk with structural metadata preserved.
type ChunkedSegment struct {
	Text    string // formatted chunk text (same as what Chunk() returns)
	Heading string // nearest heading above this segment (empty if none)
	Kind    string // "prose", "code", "list", "table"
}

// ChunkStructured is like Chunk but returns structural metadata alongside
// each chunk. Use this when downstream callers need heading context for
// grouping (e.g., ingest section synthesis).
func ChunkStructured(text string, maxTokens int) []ChunkedSegment {
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	maxChars := maxTokens * charsPerToken
	segments := parseSegments(text)
	merged := mergeSegments(segments, maxChars)

	var result []ChunkedSegment
	for _, seg := range merged {
		formatted := formatSegment(seg, maxChars)
		for _, chunk := range formatted {
			result = append(result, ChunkedSegment{
				Text:    chunk,
				Heading: seg.heading,
				Kind:    segKindString(seg.kind),
			})
		}
	}
	return result
}

// segKindString converts a segmentKind to a human-readable string.
func segKindString(k segmentKind) string {
	switch k {
	case segCodeBlock:
		return "code"
	case segListBlock:
		return "list"
	case segTableBlock:
		return "table"
	default:
		return "prose"
	}
}

// detectContent classifies the dominant content type of text.
// Exported for testing.
func detectContent(text string) contentType {
	lines := strings.Split(text, "\n")
	var codeLines, headingLines, listLines, totalLines int

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		totalLines++
		if headingRe.MatchString(trimmed) {
			headingLines++
		}
		if listItemRe.MatchString(line) {
			listLines++
		}
		// Heuristic for code: high symbol density.
		symbolCount := 0
		for _, r := range trimmed {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsSpace(r) {
				symbolCount++
			}
		}
		if len(trimmed) > 0 && float64(symbolCount)/float64(len(trimmed)) > 0.25 {
			codeLines++
		}
	}

	if totalLines == 0 {
		return contentProse
	}

	if float64(codeLines)/float64(totalLines) > 0.5 {
		return contentCode
	}
	if headingLines > 0 || listLines > 2 {
		return contentMarkdown
	}
	return contentProse
}
