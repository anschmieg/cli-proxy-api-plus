package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIEmbeddingsRoutesThroughModelProviderSelection(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"text-embedding-3-small","input":"hello"}`))
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: got %d want %d; body=%s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, "invalid_request_error") {
		t.Fatalf("expected invalid_request_error, got: %s", body)
	}
	if !strings.Contains(body, "unknown provider for model text-embedding-3-small") {
		t.Fatalf("expected model provider selection error, got: %s", body)
	}
}
