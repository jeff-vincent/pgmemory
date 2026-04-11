package proxy

import "net/http"

type openaiHandler struct{}

func newOpenAIHandler() *openaiHandler { return &openaiHandler{} }

func (h *openaiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "OpenAI-compatible endpoint not yet implemented", http.StatusNotImplemented)
}
