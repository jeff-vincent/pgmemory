package crawler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Page represents a crawled web page.
type Page struct {
	URL         string
	Content     string // extracted text
	ContentHash string // SHA-256 of Content
	Links       []string
}

// Options configures the crawler.
type Options struct {
	MaxDepth int
	MaxPages int
	Delay    time.Duration
	Headers  map[string]string // custom headers sent with every request (e.g. Cookie, Authorization)
}

// DefaultOptions returns sensible defaults for crawling a company wiki.
func DefaultOptions() Options {
	return Options{
		MaxDepth: 3,
		MaxPages: 500,
		Delay:    100 * time.Millisecond,
	}
}

// Crawl fetches pages starting from baseURL, following internal links.
// Returns all successfully crawled pages.
func Crawl(ctx context.Context, baseURL string, opts Options) ([]Page, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	if base.Scheme == "" {
		base.Scheme = "https"
	}

	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 3
	}
	if opts.MaxPages <= 0 {
		opts.MaxPages = 500
	}
	if opts.Delay <= 0 {
		opts.Delay = 100 * time.Millisecond
	}

	client := &http.Client{Timeout: 30 * time.Second}
	visited := make(map[string]bool)
	var pages []Page
	headers := opts.Headers

	type task struct {
		url   string
		depth int
	}

	queue := []task{{url: base.String(), depth: 0}}

	for len(queue) > 0 && len(pages) < opts.MaxPages {
		select {
		case <-ctx.Done():
			return pages, ctx.Err()
		default:
		}

		current := queue[0]
		queue = queue[1:]

		if visited[current.url] {
			continue
		}
		visited[current.url] = true

		log.Printf("[crawler] fetching %s (depth %d, %d/%d pages)",
			current.url, current.depth, len(pages)+1, opts.MaxPages)

		page, err := fetchPage(ctx, client, current.url, base, headers)
		if err != nil {
			log.Printf("[crawler] skip %s: %v", current.url, err)
			continue
		}

		if len(strings.TrimSpace(page.Content)) < 50 {
			log.Printf("[crawler] skip %s: too little content", current.url)
			continue
		}

		pages = append(pages, *page)

		if current.depth < opts.MaxDepth {
			for _, link := range page.Links {
				if !visited[link] && isInternal(link, base) {
					queue = append(queue, task{url: link, depth: current.depth + 1})
				}
			}
		}

		if opts.Delay > 0 {
			time.Sleep(opts.Delay)
		}
	}

	return pages, nil
}

func fetchPage(ctx context.Context, client *http.Client, pageURL string, base *url.URL, headers map[string]string) (*Page, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "memoryd-crawler/1.0")
	req.Header.Set("Accept", "text/html")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") && ct != "" {
		return nil, fmt.Errorf("not HTML: %s", ct)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024)) // 5MB max
	if err != nil {
		return nil, err
	}

	htmlStr := string(body)
	text := ExtractText(htmlStr)
	links := ExtractLinks(htmlStr, base)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(text)))

	return &Page{
		URL:         pageURL,
		Content:     text,
		ContentHash: hash,
		Links:       links,
	}, nil
}

// isInternal checks if a URL is within the base URL scope.
func isInternal(link string, base *url.URL) bool {
	parsed, err := url.Parse(link)
	if err != nil {
		return false
	}
	return parsed.Host == base.Host && strings.HasPrefix(parsed.Path, base.Path)
}

var (
	scriptRe     = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe      = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	noscriptRe   = regexp.MustCompile(`(?is)<noscript[^>]*>.*?</noscript>`)
	tagRe        = regexp.MustCompile(`<[^>]+>`)
	whitespaceRe = regexp.MustCompile(`[\t ]+`)
	blankLinesRe = regexp.MustCompile(`\n{3,}`)
)

// ExtractText strips HTML tags and returns clean text content.
func ExtractText(htmlContent string) string {
	text := scriptRe.ReplaceAllString(htmlContent, "")
	text = styleRe.ReplaceAllString(text, "")
	text = noscriptRe.ReplaceAllString(text, "")
	text = tagRe.ReplaceAllString(text, " ")
	text = html.UnescapeString(text)
	text = whitespaceRe.ReplaceAllString(text, " ")
	text = blankLinesRe.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

// ExtractLinks finds all href links in HTML and resolves them against base.
func ExtractLinks(htmlContent string, base *url.URL) []string {
	hrefRe := regexp.MustCompile(`(?i)href\s*=\s*["']([^"']+)["']`)
	matches := hrefRe.FindAllStringSubmatch(htmlContent, -1)

	var links []string
	seen := make(map[string]bool)

	for _, m := range matches {
		href := m[1]
		if strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "mailto:") {
			continue
		}
		parsed, err := url.Parse(href)
		if err != nil {
			continue
		}
		resolved := base.ResolveReference(parsed)
		resolved.Fragment = ""
		key := resolved.String()
		if !seen[key] {
			seen[key] = true
			links = append(links, key)
		}
	}
	return links
}
