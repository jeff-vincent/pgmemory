package export

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/memory-daemon/memoryd/internal/store"
)

// ExportStore is the minimal store surface the exporter needs.
type ExportStore interface {
	List(ctx context.Context, query string, limit int) ([]store.Memory, error)
	ListSources(ctx context.Context) ([]store.Source, error)
	ListBySource(ctx context.Context, sourcePrefix string, limit int) ([]store.Memory, error)
}

// Options controls the export output.
type Options struct {
	// Output directory for the generated doc set.
	OutputDir string

	// MinQualityScore filters out low-quality memories (0 = include all).
	MinQualityScore float64

	// Format: "markdown" (default) or "json".
	Format string
}

// Run exports the knowledge base to a directory of documents.
func Run(ctx context.Context, s ExportStore, opts Options) error {
	if opts.OutputDir == "" {
		opts.OutputDir = "memoryd-export"
	}
	if opts.Format == "" {
		opts.Format = "markdown"
	}

	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	// Fetch all memories (no query filter, generous limit).
	memories, err := s.List(ctx, "", 10000)
	if err != nil {
		return fmt.Errorf("listing memories: %w", err)
	}

	// Apply quality filter.
	if opts.MinQualityScore > 0 {
		var filtered []store.Memory
		for _, m := range memories {
			if m.QualityScore >= opts.MinQualityScore {
				filtered = append(filtered, m)
			}
		}
		memories = filtered
	}

	if len(memories) == 0 {
		return fmt.Errorf("no memories to export (total=0 after quality filter %.2f)", opts.MinQualityScore)
	}

	// Group by source.
	groups := map[string][]store.Memory{}
	for _, m := range memories {
		src := m.Source
		if src == "" {
			src = "captured"
		}
		groups[src] = append(groups[src], m)
	}

	// Sort group keys.
	var sources []string
	for k := range groups {
		sources = append(sources, k)
	}
	sort.Strings(sources)

	// Write one markdown file per source.
	var toc []string
	totalWritten := 0

	for _, src := range sources {
		mems := groups[src]

		// Sort memories within source by quality (desc), then created_at (asc).
		sort.Slice(mems, func(i, j int) bool {
			if mems[i].QualityScore != mems[j].QualityScore {
				return mems[i].QualityScore > mems[j].QualityScore
			}
			return mems[i].CreatedAt.Before(mems[j].CreatedAt)
		})

		filename := sanitizeFilename(src) + ".md"
		path := filepath.Join(opts.OutputDir, filename)

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("# %s\n\n", src))
		sb.WriteString(fmt.Sprintf("> %d memories | exported %s\n\n", len(mems), time.Now().Format("2006-01-02 15:04")))

		for i, m := range mems {
			sb.WriteString(fmt.Sprintf("## %d. %s\n\n", i+1, firstLine(m.Content)))
			sb.WriteString(m.Content)
			sb.WriteString("\n\n")
			sb.WriteString(fmt.Sprintf("_quality: %.2f | hits: %d | created: %s_\n\n",
				m.QualityScore, m.HitCount, m.CreatedAt.Format("2006-01-02")))
			sb.WriteString("---\n\n")
		}

		if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}

		toc = append(toc, fmt.Sprintf("- [%s](%s) (%d memories)", src, filename, len(mems)))
		totalWritten += len(mems)
	}

	// Write index.
	var idx strings.Builder
	idx.WriteString("# memoryd Knowledge Base Export\n\n")
	idx.WriteString(fmt.Sprintf("> %d memories across %d sources | exported %s\n\n",
		totalWritten, len(sources), time.Now().Format("2006-01-02 15:04")))

	if opts.MinQualityScore > 0 {
		idx.WriteString(fmt.Sprintf("> quality filter: >= %.2f\n\n", opts.MinQualityScore))
	}

	idx.WriteString("## Sources\n\n")
	for _, line := range toc {
		idx.WriteString(line + "\n")
	}
	idx.WriteString("\n")

	indexPath := filepath.Join(opts.OutputDir, "index.md")
	if err := os.WriteFile(indexPath, []byte(idx.String()), 0644); err != nil {
		return fmt.Errorf("writing index: %w", err)
	}

	fmt.Printf("Exported %d memories to %s/ (%d files)\n", totalWritten, opts.OutputDir, len(sources)+1)
	return nil
}

func sanitizeFilename(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		case r == '/' || r == ' ' || r == ':':
			return '-'
		default:
			return -1
		}
	}, s)
	// Collapse runs of dashes.
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if s == "" {
		s = "unnamed"
	}
	return s
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	if len(s) > 80 {
		s = s[:77] + "..."
	}
	return s
}
