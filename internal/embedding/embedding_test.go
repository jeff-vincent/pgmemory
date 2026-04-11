package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mockEmbedServer(dim int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/v1/embeddings":
			vec := make([]float32, dim)
			for i := range vec {
				vec[i] = float32(i) * 0.01
			}
			resp := embeddingResp{
				Data: []struct {
					Embedding []float32 `json:"embedding"`
				}{
					{Embedding: vec},
				},
			}
			json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestLlamaEmbedder_Embed(t *testing.T) {
	dim := 512
	srv := mockEmbedServer(dim)
	defer srv.Close()

	e := &LlamaEmbedder{
		serverURL: srv.URL,
		dim:       dim,
		client:    srv.Client(),
	}

	vec, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed() error: %v", err)
	}
	if len(vec) != dim {
		t.Errorf("vector length = %d, want %d", len(vec), dim)
	}
	if vec[1] != 0.01 {
		t.Errorf("vec[1] = %f, want 0.01", vec[1])
	}
}

func TestLlamaEmbedder_Dim(t *testing.T) {
	e := &LlamaEmbedder{dim: 512}
	if e.Dim() != 512 {
		t.Errorf("Dim() = %d, want 512", e.Dim())
	}
}

func TestLlamaEmbedder_Embed_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	e := &LlamaEmbedder{
		serverURL: srv.URL,
		dim:       512,
		client:    srv.Client(),
	}

	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Error("expected error for 500 response, got nil")
	}
}

func TestLlamaEmbedder_Embed_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embeddingResp{Data: nil}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := &LlamaEmbedder{
		serverURL: srv.URL,
		dim:       512,
		client:    srv.Client(),
	}

	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Error("expected error for empty embedding response, got nil")
	}
}

func TestLlamaEmbedder_Embed_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	e := &LlamaEmbedder{
		serverURL: srv.URL,
		dim:       512,
		client:    srv.Client(),
	}

	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestLlamaEmbedder_Close_NilProcess(t *testing.T) {
	e := &LlamaEmbedder{}
	if err := e.Close(); err != nil {
		t.Errorf("Close() with nil cmd should not error: %v", err)
	}
}
