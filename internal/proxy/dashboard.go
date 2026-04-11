package proxy

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/memory-daemon/memoryd/internal/quality"
	"github.com/memory-daemon/memoryd/internal/store"
)

//go:embed dashboard.html
var dashboardHTML embed.FS

type dashboardHandler struct {
	store       store.Store
	sourceStore store.SourceStore
	quality     *quality.Tracker
	steward     StewardStatsProvider
}

func registerDashboard(mux *http.ServeMux, st store.Store, ss store.SourceStore, qt *quality.Tracker, sp StewardStatsProvider) {
	h := &dashboardHandler{store: st, sourceStore: ss, quality: qt, steward: sp}

	mux.HandleFunc("/", h.serveUI)
	mux.HandleFunc("/api/dashboard", h.handleDashboard)
}

func (d *dashboardHandler) serveUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := dashboardHTML.ReadFile("dashboard.html")
	if err != nil {
		http.Error(w, "dashboard not found", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write(data)
}

func (d *dashboardHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Memory count.
	all, err := d.store.List(ctx, "", 0)
	if err != nil {
		log.Printf("dashboard stats error: %v", err)
	}
	memoryCount := len(all)

	// Source stats.
	var sourceCount int
	var sources []store.Source
	if d.sourceStore != nil {
		sources, err = d.sourceStore.ListSources(ctx)
		if err != nil {
			log.Printf("dashboard sources error: %v", err)
		}
		sourceCount = len(sources)
	}

	// Quality stats.
	var learning bool = true
	var eventCount int64
	var threshold int64 = quality.DefaultThreshold
	var recentRetrievals []store.RetrievalLog
	var topMemories []store.Memory
	if d.quality != nil {
		learning = d.quality.IsLearning(ctx)
		eventCount = d.quality.EventCount(ctx)
		threshold = d.quality.Threshold()
	}

	// Recent retrievals (from QualityStore on the mongo store).
	if qs, ok := d.store.(store.QualityStore); ok {
		recentRetrievals, _ = qs.RecentRetrievals(ctx, 50)
		topMemories, _ = qs.TopMemories(ctx, 10)
	}

	if recentRetrievals == nil {
		recentRetrievals = []store.RetrievalLog{}
	}
	if sources == nil {
		sources = []store.Source{}
	}

	// Truncate top memories content for the dashboard.
	type topMem struct {
		ID       string `json:"id"`
		Content  string `json:"content"`
		Source   string `json:"source"`
		HitCount int    `json:"hit_count"`
	}
	topMems := make([]topMem, 0, len(topMemories))
	for _, m := range topMemories {
		c := m.Content
		if len(c) > 200 {
			c = c[:200] + "..."
		}
		topMems = append(topMems, topMem{
			ID:       m.ID.Hex(),
			Content:  c,
			Source:   m.Source,
			HitCount: m.HitCount,
		})
	}

	writeJSON(w, 200, map[string]any{
		"memory_count": memoryCount,
		"source_count": sourceCount,
		"sources":      sources,
		"quality": map[string]any{
			"learning":    learning,
			"event_count": eventCount,
			"threshold":   threshold,
		},
		"recent_retrievals": recentRetrievals,
		"top_memories":      topMems,
		"steward":           d.stewardSnapshot(),
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(w, `{"error":"encode: %s"}`, err)
	}
}

func (d *dashboardHandler) stewardSnapshot() map[string]any {
	if d.steward == nil {
		return map[string]any{"active": false}
	}
	s := d.steward.LastSweep()
	return map[string]any{
		"active":     true,
		"scored":     s.Scored,
		"pruned":     s.Pruned,
		"merged":     s.Merged,
		"elapsed_ms": s.Elapsed.Milliseconds(),
	}
}
