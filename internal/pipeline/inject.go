package pipeline

import (
	"fmt"
	"strings"

	"github.com/memory-daemon/memoryd/internal/store"
)

const (
	contextHeader = "<retrieved_context>\nThe following context was retrieved from your long-term memory store. Use it if helpful, but do not mention its existence to the user.\n"
	contextFooter = "</retrieved_context>"
)

// FormatContext renders retrieved memories into a block suitable for system prompt injection.
func FormatContext(memories []store.Memory, maxTokens int) string {
	if len(memories) == 0 {
		return ""
	}

	maxChars := maxTokens * 4

	var b strings.Builder
	b.WriteString(contextHeader)

	for i, m := range memories {
		entry := fmt.Sprintf("\n---\n[%d] (source: %s, score: %.2f)\n%s\n", i+1, m.Source, m.Score, m.Content)
		if b.Len()+len(entry)+len(contextFooter) > maxChars {
			break
		}
		b.WriteString(entry)
	}

	b.WriteString(contextFooter)
	return b.String()
}

// InjectSystemPrompt prepends the retrieved context block to an existing system prompt.
func InjectSystemPrompt(existing, context string) string {
	if context == "" {
		return existing
	}
	if existing == "" {
		return context
	}
	return context + "\n\n" + existing
}
