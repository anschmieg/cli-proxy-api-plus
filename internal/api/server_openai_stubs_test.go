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

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("unexpected status code: got %d want %d; body=%s", rr.Code, http.StatusNotImplemented, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, "unsupported_endpoint") {
		t.Fatalf("expected unsupported_endpoint error, got: %s", body)
	}
	if !strings.Contains(body, "/v1/embeddings") {
		t.Fatalf("expected endpoint hint in body, got: %s", body)
	}
}
