package handlers_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers"
	claudehandlers "github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers/claude"
	openaihandlers "github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers/openai"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func TestOpenAIProfileMissingDefaultModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		Profiles: []config.Profile{
			{ID: "assistant"},
		},
	}

	base := handlers.NewBaseAPIHandlersWithConfig(cfg, nil)
	api := openaihandlers.NewOpenAIAPIHandler(base)

	router := gin.New()
	router.POST("/v1/chat/completions", api.ChatCompletions)

	body := []byte(`{"model":"assistant","messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestClaudeProfileMissingDefaultModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		Profiles: []config.Profile{
			{ID: "assistant"},
		},
	}

	base := handlers.NewBaseAPIHandlersWithConfig(cfg, nil)
	api := claudehandlers.NewClaudeCodeAPIHandler(base)

	router := gin.New()
	router.POST("/v1/messages", api.ClaudeMessages)

	body := []byte(`{"model":"assistant","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
