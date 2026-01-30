package executor

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

type OpenRouterExecutor struct {
	cfg *config.Config
	*OpenAICompatExecutor
}

func NewOpenRouterExecutor(cfg *config.Config) *OpenRouterExecutor {
	return &OpenRouterExecutor{
		cfg:                  cfg,
		OpenAICompatExecutor: NewOpenAICompatExecutor("openrouter", cfg),
	}
}

func (e *OpenRouterExecutor) Identifier() string { return "openrouter" }

func (e *OpenRouterExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	apiKey, _ := e.openRouterCreds(auth)
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Add OpenRouter specific headers
	req.Header.Set("HTTP-Referer", "https://github.com/router-for-me/CLIProxyAPI")
	req.Header.Set("X-Title", "CLI Proxy API")

	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(req, attrs)
	return nil
}

func (e *OpenRouterExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("openrouter executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	if err := e.PrepareRequest(httpReq, auth); err != nil {
		return nil, err
	}
	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}

func (e *OpenRouterExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	// Re-use OpenAI Compat logic but ensure we set the correct base URL if not present
	apiKey, baseURL := e.openRouterCreds(auth)
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}

	// Update auth with resolved base URL for the OpenAICompatExecutor to use
	if auth != nil {
		if auth.Attributes == nil {
			auth.Attributes = make(map[string]string)
		}
		auth.Attributes["base_url"] = baseURL
		if apiKey != "" {
			auth.Attributes["api_key"] = apiKey
		}
	}

	// Delegate to OpenAICompatExecutor
	return e.OpenAICompatExecutor.Execute(ctx, auth, req, opts)
}

func (e *OpenRouterExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (stream <-chan cliproxyexecutor.StreamChunk, err error) {
	// Re-use OpenAI Compat logic but ensure we set the correct base URL if not present
	apiKey, baseURL := e.openRouterCreds(auth)
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}

	// Update auth with resolved base URL for the OpenAICompatExecutor to use
	if auth != nil {
		if auth.Attributes == nil {
			auth.Attributes = make(map[string]string)
		}
		auth.Attributes["base_url"] = baseURL
		if apiKey != "" {
			auth.Attributes["api_key"] = apiKey
		}
	}

	// Delegate to OpenAICompatExecutor
	return e.OpenAICompatExecutor.ExecuteStream(ctx, auth, req, opts)
}

func (e *OpenRouterExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.OpenAICompatExecutor.CountTokens(ctx, auth, req, opts)
}

func (e *OpenRouterExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return auth, nil
}

func (e *OpenRouterExecutor) openRouterCreds(a *cliproxyauth.Auth) (apiKey, baseURL string) {
	if a == nil {
		return "", ""
	}
	if a.Attributes != nil {
		apiKey = a.Attributes["api_key"]
		baseURL = a.Attributes["base_url"]
	}

	// Also check config if not in auth attributes
	if apiKey == "" && e.cfg != nil {
		// Attempt to find in general config if we had a specific openrouter config section,
		// but since we don't, we rely on auth attributes or OpenAICompatibility config if mapped.
		// For now, we assume credentials come via auth attributes or are injected before calling execute.
	}

	return
}
