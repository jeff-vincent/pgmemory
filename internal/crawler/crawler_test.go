package crawler

import (
	"net/url"
	"strings"
	"testing"
)

func TestExtractText_Basic(t *testing.T) {
	html := `<html><head><title>Test</title></head><body><h1>Hello</h1><p>World</p></body></html>`
	text := ExtractText(html)
	if !strings.Contains(text, "Hello") {
		t.Errorf("expected 'Hello' in text, got %q", text)
	}
	if !strings.Contains(text, "World") {
		t.Errorf("expected 'World' in text, got %q", text)
	}
	if strings.Contains(text, "<") {
		t.Error("text should not contain HTML tags")
	}
}

func TestExtractText_StripsScriptAndStyle(t *testing.T) {
	html := `<html><head><style>body{color:red}</style></head><body>
<script>alert('xss')</script>
<p>Safe content</p>
<noscript>Enable JS</noscript>
</body></html>`
	text := ExtractText(html)
	if strings.Contains(text, "alert") {
		t.Error("script content should be stripped")
	}
	if strings.Contains(text, "color:red") {
		t.Error("style content should be stripped")
	}
	if strings.Contains(text, "Enable JS") {
		t.Error("noscript content should be stripped")
	}
	if !strings.Contains(text, "Safe content") {
		t.Error("expected 'Safe content' to remain")
	}
}

func TestExtractText_DecodesEntities(t *testing.T) {
	html := `<p>Hello &amp; goodbye &lt;world&gt;</p>`
	text := ExtractText(html)
	if !strings.Contains(text, "Hello & goodbye <world>") {
		t.Errorf("entities not decoded: %q", text)
	}
}

func TestExtractLinks_Basic(t *testing.T) {
	base, _ := url.Parse("https://wiki.example.com/docs")
	html := `<a href="/docs/page1">Page 1</a>
<a href="/docs/page2">Page 2</a>
<a href="https://external.com">External</a>
<a href="#anchor">Anchor</a>
<a href="javascript:void(0)">JS</a>`

	links := ExtractLinks(html, base)

	// ExtractLinks resolves all URLs; filtering is done by isInternal during crawling.
	// Anchors and javascript: links are skipped.
	hasPage1 := false
	hasPage2 := false
	hasExternal := false
	for _, l := range links {
		if strings.Contains(l, "page1") {
			hasPage1 = true
		}
		if strings.Contains(l, "page2") {
			hasPage2 = true
		}
		if strings.Contains(l, "external.com") {
			hasExternal = true
		}
		if strings.Contains(l, "#anchor") {
			t.Error("should not include fragment-only links")
		}
		if strings.Contains(l, "javascript:") {
			t.Error("should not include javascript: links")
		}
	}
	if !hasPage1 {
		t.Error("missing page1 link")
	}
	if !hasPage2 {
		t.Error("missing page2 link")
	}
	if !hasExternal {
		t.Error("expected external link to be included (filtering is done during crawl)")
	}
}

func TestExtractLinks_RelativeURLs(t *testing.T) {
	base, _ := url.Parse("https://wiki.example.com/docs/")
	html := `<a href="subpage">Subpage</a><a href="../other">Other</a>`

	links := ExtractLinks(html, base)
	hasSubpage := false
	for _, l := range links {
		if strings.HasSuffix(l, "/docs/subpage") {
			hasSubpage = true
		}
	}
	if !hasSubpage {
		t.Errorf("expected resolved subpage link, got %v", links)
	}
}

func TestExtractLinks_Deduplicates(t *testing.T) {
	base, _ := url.Parse("https://wiki.example.com/")
	html := `<a href="/page">Link 1</a><a href="/page">Link 2</a><a href="/page#section">Link 3</a>`

	links := ExtractLinks(html, base)
	// /page and /page#section should deduplicate (fragment stripped)
	count := 0
	for _, l := range links {
		if strings.HasSuffix(l, "/page") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 unique /page link, got %d in %v", count, links)
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()
	if opts.MaxDepth != 3 {
		t.Errorf("MaxDepth = %d, want 3", opts.MaxDepth)
	}
	if opts.MaxPages != 500 {
		t.Errorf("MaxPages = %d, want 500", opts.MaxPages)
	}
}

func TestIsInternal(t *testing.T) {
	base, _ := url.Parse("https://wiki.example.com/docs")

	tests := []struct {
		link string
		want bool
	}{
		{"https://wiki.example.com/docs/page1", true},
		{"https://wiki.example.com/docs/sub/page", true},
		{"https://wiki.example.com/other", false},
		{"https://external.com/docs/page", false},
		{"https://wiki.example.com/docs", true},
	}

	for _, tt := range tests {
		got := isInternal(tt.link, base)
		if got != tt.want {
			t.Errorf("isInternal(%q) = %v, want %v", tt.link, got, tt.want)
		}
	}
}
