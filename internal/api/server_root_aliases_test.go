package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRootModelsAlias(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if body := rr.Body.String(); !strings.Contains(body, `"object":"list"`) {
		t.Fatalf("response body missing list object: %s", body)
	}
}

func TestRootMessagesAliasNotFoundGuard(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(`{"model":"unknown-model","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code == http.StatusNotFound {
		t.Fatalf("expected /messages to be routed, got 404; body=%s", rr.Body.String())
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: got %d want %d; body=%s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
	if body := rr.Body.String(); !strings.Contains(body, "unknown provider for model") {
		t.Fatalf("expected unknown provider error, got: %s", body)
	}
}
