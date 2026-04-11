package ingest

import (
	"strings"
	"testing"

	"github.com/memory-daemon/memoryd/internal/chunker"
)

func TestGroupByHeading_NoHeadings(t *testing.T) {
	segs := []chunker.ChunkedSegment{
		{Text: "First chunk.", Heading: "", Kind: "prose"},
		{Text: "Second chunk.", Heading: "", Kind: "prose"},
	}
	groups := groupByHeading(segs)
	// Both share the same empty heading — they get merged into one entry.
	if len(groups) != 1 {
		t.Fatalf("expected 1 group for same-heading chunks, got %d", len(groups))
	}
	if !strings.Contains(groups[0], "First chunk.") {
		t.Error("expected first chunk in group")
	}
	if !strings.Contains(groups[0], "Second chunk.") {
		t.Error("expected second chunk in group")
	}
}

func TestGroupByHeading_TwoHeadings(t *testing.T) {
	segs := []chunker.ChunkedSegment{
		{Text: "Intro content.", Heading: "Introduction", Kind: "prose"},
		{Text: "More intro.", Heading: "Introduction", Kind: "prose"},
		{Text: "Usage content.", Heading: "Usage", Kind: "prose"},
	}
	groups := groupByHeading(segs)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups (one per heading), got %d", len(groups))
	}
	if !strings.HasPrefix(groups[0], "## Introduction") {
		t.Errorf("first group should start with heading, got: %q", groups[0][:40])
	}
	if !strings.HasPrefix(groups[1], "## Usage") {
		t.Errorf("second group should start with Usage heading, got: %q", groups[1])
	}
	if !strings.Contains(groups[0], "Intro content.") {
		t.Error("expected intro content in first group")
	}
	if !strings.Contains(groups[0], "More intro.") {
		t.Error("expected both intro chunks in first group")
	}
}

func TestGroupByHeading_SingleChunk(t *testing.T) {
	segs := []chunker.ChunkedSegment{
		{Text: "Only chunk.", Heading: "Section", Kind: "prose"},
	}
	groups := groupByHeading(segs)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if !strings.Contains(groups[0], "Only chunk.") {
		t.Error("expected chunk text in group")
	}
}

func TestGroupByHeading_Empty(t *testing.T) {
	groups := groupByHeading(nil)
	if len(groups) != 0 {
		t.Errorf("expected 0 groups for nil input, got %d", len(groups))
	}
}

func TestGroupByHeading_HeadingPrefix(t *testing.T) {
	segs := []chunker.ChunkedSegment{
		{Text: "Some text.", Heading: "My Section", Kind: "prose"},
	}
	groups := groupByHeading(segs)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if !strings.HasPrefix(groups[0], "## My Section\n\n") {
		t.Errorf("expected heading prefix, got: %q", groups[0])
	}
}

func TestGroupByHeading_SkipsEmptyText(t *testing.T) {
	segs := []chunker.ChunkedSegment{
		{Text: "   ", Heading: "Section", Kind: "prose"},
		{Text: "Real content.", Heading: "Section", Kind: "prose"},
	}
	groups := groupByHeading(segs)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if strings.Contains(groups[0], "   ") {
		t.Error("whitespace-only chunk should be excluded")
	}
}

func TestBuildSections_StructuredDoc(t *testing.T) {
	doc := "## Installation\n\nRun go get to install the package.\n\n## Configuration\n\nEdit the config.yaml file with your settings."
	sections := buildSections(doc)
	if len(sections) < 2 {
		t.Fatalf("expected at least 2 sections, got %d: %v", len(sections), sections)
	}
	var hasInstall, hasConfig bool
	for _, s := range sections {
		if strings.Contains(s, "Installation") {
			hasInstall = true
		}
		if strings.Contains(s, "Configuration") {
			hasConfig = true
		}
	}
	if !hasInstall {
		t.Error("expected an Installation section")
	}
	if !hasConfig {
		t.Error("expected a Configuration section")
	}
}

func TestBuildSections_Empty(t *testing.T) {
	sections := buildSections("")
	if len(sections) != 0 {
		t.Errorf("expected 0 sections for empty content, got %d", len(sections))
	}
}

func TestBuildSections_TooShortFiltered(t *testing.T) {
	// A page with very short chunks that don't meet minSectionLen.
	sections := buildSections("hi")
	if len(sections) != 0 {
		t.Errorf("expected 0 sections for too-short content, got %d: %v", len(sections), sections)
	}
}
