package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/memory-daemon/memoryd/internal/ingest"
	"github.com/memory-daemon/memoryd/internal/quality"
	"github.com/memory-daemon/memoryd/internal/store"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type sourceAPIHandler struct {
	sourceStore store.SourceStore
	memStore    store.Store
	ingester    *ingest.Ingester
	quality     *quality.Tracker
}

func registerSourceAPI(mux *http.ServeMux, ss store.SourceStore, ms store.Store, ing *ingest.Ingester, qt *quality.Tracker) {
	h := &sourceAPIHandler{sourceStore: ss, memStore: ms, ingester: ing, quality: qt}
	mux.HandleFunc("/api/sources", h.handleSources)
	mux.HandleFunc("/api/sources/upload", h.handleUpload)
	mux.HandleFunc("/api/sources/", h.handleSourceByID)
	mux.HandleFunc("/api/quality", h.handleQuality)
}

func (h *sourceAPIHandler) handleSources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		sources, err := h.sourceStore.ListSources(ctx)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if sources == nil {
			sources = []store.Source{}
		}
		writeJSON(w, 200, sources)

	case http.MethodPost:
		var req struct {
			Name     string            `json:"name"`
			BaseURL  string            `json:"base_url"`
			MaxDepth int               `json:"max_depth,omitempty"`
			MaxPages int               `json:"max_pages,omitempty"`
			Headers  map[string]string `json:"headers,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.Name == "" || req.BaseURL == "" {
			writeJSON(w, 400, map[string]string{"error": "name and base_url are required"})
			return
		}
		if req.MaxDepth <= 0 {
			req.MaxDepth = 3
		}
		if req.MaxPages <= 0 {
			req.MaxPages = 500
		}

		src := store.Source{
			Name:     req.Name,
			BaseURL:  req.BaseURL,
			Status:   "crawling",
			MaxDepth: req.MaxDepth,
			MaxPages: req.MaxPages,
			Headers:  req.Headers,
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		id, err := h.sourceStore.InsertSource(ctx, src)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		src.ID, _ = primitive.ObjectIDFromHex(id)

		// Start crawl asynchronously.
		go func() {
			crawlCtx, crawlCancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer crawlCancel()
			if err := h.ingester.IngestSource(crawlCtx, src); err != nil {
				log.Printf("[source-api] crawl error for %s: %v", src.Name, err)
				if e := h.sourceStore.UpdateSourceStatus(context.Background(), id, "error", err.Error(), 0, 0); e != nil {
					log.Printf("[source-api] status update error: %v", e)
				}
			}
		}()

		writeJSON(w, 200, map[string]string{"status": "ok", "id": id, "message": "crawl started"})

	default:
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}

// handleUpload accepts bulk document uploads as either multipart files or JSON.
func (h *sourceAPIHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}

	var name string
	var files []ingest.FileContent

	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		// Multipart form: field "name" + file parts.
		const maxUpload = 50 * 1024 * 1024 // 50 MB
		if err := r.ParseMultipartForm(maxUpload); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid multipart form: " + err.Error()})
			return
		}
		name = r.FormValue("name")
		for _, fh := range r.MultipartForm.File["files"] {
			f, err := fh.Open()
			if err != nil {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(f, 5*1024*1024))
			f.Close()
			if err != nil {
				continue
			}
			files = append(files, ingest.FileContent{Filename: fh.Filename, Content: string(data)})
		}
	} else {
		// JSON body: {"name": "...", "files": [{"filename": "...", "content": "..."}]}
		var req struct {
			Name  string `json:"name"`
			Files []struct {
				Filename string `json:"filename"`
				Content  string `json:"content"`
			} `json:"files"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
			return
		}
		name = req.Name
		for _, f := range req.Files {
			files = append(files, ingest.FileContent{Filename: f.Filename, Content: f.Content})
		}
	}

	if name == "" || len(files) == 0 {
		writeJSON(w, 400, map[string]string{"error": "name and at least one file are required"})
		return
	}

	src := store.Source{
		Name:     name,
		BaseURL:  "upload://" + name,
		Status:   "processing",
		MaxDepth: 0,
		MaxPages: len(files),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	id, err := h.sourceStore.InsertSource(ctx, src)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	src.ID, _ = primitive.ObjectIDFromHex(id)

	go func() {
		uploadCtx, uploadCancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer uploadCancel()
		if err := h.ingester.IngestFiles(uploadCtx, src, files); err != nil {
			log.Printf("[source-api] upload error for %s: %v", src.Name, err)
			if e := h.sourceStore.UpdateSourceStatus(context.Background(), id, "error", err.Error(), 0, 0); e != nil {
				log.Printf("[source-api] status update error: %v", e)
			}
		}
	}()

	writeJSON(w, 200, map[string]string{"status": "ok", "id": id, "message": fmt.Sprintf("%d files queued for processing", len(files))})
}

func (h *sourceAPIHandler) handleSourceByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract ID or action from /api/sources/{id} or /api/sources/{id}/refresh
	path := strings.TrimPrefix(r.URL.Path, "/api/sources/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if id == "" {
		writeJSON(w, 400, map[string]string{"error": "source id required"})
		return
	}

	switch {
	case action == "memories" && r.Method == http.MethodGet:
		// List memories belonging to this source.
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		sources, err := h.sourceStore.ListSources(ctx)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		var sourceName string
		for _, s := range sources {
			if s.ID.Hex() == id {
				sourceName = s.Name
				break
			}
		}
		if sourceName == "" {
			writeJSON(w, 404, map[string]string{"error": "source not found"})
			return
		}
		mems, err := h.memStore.ListBySource(ctx, "source:"+sourceName, 200)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if mems == nil {
			mems = []store.Memory{}
		}
		writeJSON(w, 200, mems)

	case action == "refresh" && r.Method == http.MethodPost:
		// Re-crawl the source.
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		sources, err := h.sourceStore.ListSources(ctx)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		var found *store.Source
		for i := range sources {
			if sources[i].ID.Hex() == id {
				found = &sources[i]
				break
			}
		}
		if found == nil {
			writeJSON(w, 404, map[string]string{"error": "source not found"})
			return
		}

		if err := h.sourceStore.UpdateSourceStatus(ctx, id, "crawling", "", found.PageCount, found.MemoryCount); err != nil {
			log.Printf("[source-api] status update error: %v", err)
		}

		src := *found
		go func() {
			crawlCtx, crawlCancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer crawlCancel()
			if err := h.ingester.IngestSource(crawlCtx, src); err != nil {
				log.Printf("[source-api] refresh error for %s: %v", src.Name, err)
				if e := h.sourceStore.UpdateSourceStatus(context.Background(), id, "error", err.Error(), 0, 0); e != nil {
					log.Printf("[source-api] status update error: %v", e)
				}
			}
		}()

		writeJSON(w, 200, map[string]string{"status": "ok", "message": "re-crawl started"})

	case r.Method == http.MethodDelete:
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		// Find source name for cleanup.
		sources, err := h.sourceStore.ListSources(ctx)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		var sourceName string
		for _, s := range sources {
			if s.ID.Hex() == id {
				sourceName = s.Name
				break
			}
		}
		if sourceName == "" {
			writeJSON(w, 404, map[string]string{"error": "source not found"})
			return
		}

		if err := h.ingester.RemoveSource(ctx, id, sourceName); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})

	default:
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}

func (h *sourceAPIHandler) handleQuality(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	learning := true
	var eventCount int64
	var threshold int64

	if h.quality != nil {
		learning = h.quality.IsLearning(ctx)
		eventCount = h.quality.EventCount(ctx)
		threshold = h.quality.Threshold()
	}

	writeJSON(w, 200, map[string]any{
		"learning":    learning,
		"event_count": eventCount,
		"threshold":   threshold,
	})
}
