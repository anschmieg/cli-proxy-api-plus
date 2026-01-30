package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIEmbeddingsUnsupportedEndpoint(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"text-embedding-3-small","input":"hello"}`))
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status code: got %d want %d; body=%s", rr.Code, http.StatusServiceUnavailable, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, "service_unavailable_error") {
		t.Fatalf("expected service_unavailable_error error, got: %s", body)
	}
	if !strings.Contains(body, "Embedder not configured") {
		t.Fatalf("expected Embedder not configured hint in body, got: %s", body)
	}
}
