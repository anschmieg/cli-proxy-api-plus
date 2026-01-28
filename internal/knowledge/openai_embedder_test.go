package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestOpenAIEmbedder_Embed(t *testing.T) {
	t.Parallel()
	if os.Getenv("ALLOW_HTTP_TESTS") == "" {
		t.Skip("http listener not allowed in this environment")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var payload struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if payload.Model != "text-embedding-3-small" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(payload.Input) != 2 || !strings.Contains(payload.Input[0], "hello") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 0, "embedding": []float64{1, 0}},
				{"index": 1, "embedding": []float64{0, 1}},
			},
		})
	}))
	defer server.Close()

	embedder := NewOpenAIEmbedder(OpenAIEmbedderOptions{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "text-embedding-3-small",
	})
	if embedder == nil {
		t.Fatal("expected embedder")
	}

	out, err := embedder.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("embed error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 embeddings")
	}
	if len(out[0]) != 2 || out[0][0] != 1 {
		t.Fatalf("unexpected embedding data")
	}
}
