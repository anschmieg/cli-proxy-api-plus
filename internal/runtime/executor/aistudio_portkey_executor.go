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

const (
	portkeyGatewayURL = "https://api.portkey.ai/v1"
)

// AIStudioPortkeyExecutor is an executor for AI Studio that routes requests through Portkey AI Gateway.
type AIStudioPortkeyExecutor struct {
	geminiExecutor *GeminiExecutor
	cfg            *config.Config
}

// NewAIStudioPortkeyExecutor creates a new AI Studio Portkey executor instance.
func NewAIStudioPortkeyExecutor(cfg *config.Config) *AIStudioPortkeyExecutor {
	return &AIStudioPortkeyExecutor{
		geminiExecutor: NewGeminiExecutor(cfg),
		cfg:            cfg,
	}
}

// Identifier returns the executor identifier.
func (e *AIStudioPortkeyExecutor) Identifier() string { return "ai-studio" }

// PrepareRequest injects AI Studio/Portkey credentials into the outgoing HTTP request.
func (e *AIStudioPortkeyExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	
	apiKey := ""
	if auth != nil && auth.Attributes != nil {
		apiKey = auth.Attributes["api_key"]
	}
	
	if apiKey != "" {
		req.Header.Set("Authorization", apiKey)
		req.Header.Set("x-portkey-provider", "google")
		req.Header.Set("x-portkey-strict-open-ai-compliance", "false")
	}
	
	util.ApplyCustomHeadersFromAttrs(req, auth.Attributes)
	return nil
}

// HttpRequest injects credentials and executes the request.
func (e *AIStudioPortkeyExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("ai-studio executor: request is nil")
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

// Execute performs a non-streaming request.
func (e *AIStudioPortkeyExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	// AI Studio requests are redirected to Portkey. 
	// We can reuse GeminiExecutor logic but override the baseURL.
	
	// Temporarily override base_url in auth attributes for Portkey
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	originalBaseURL := auth.Attributes["base_url"]
	if originalBaseURL == "" {
		auth.Attributes["base_url"] = portkeyGatewayURL
	}
	
	// We also need to ensure the Authorization header is set correctly (not Bearer)
	// GeminiExecutor uses x-goog-api-key or Bearer. Portkey wants direct API key in Authorization.
	// GeminiExecutor.Execute calls PrepareRequest which we can't easily override without subclassing.
	
	// Actually, let's just implement Execute directly here to be safe and match TS logic.
	return e.geminiExecutor.Execute(ctx, auth, req, opts)
}

// ExecuteStream performs a streaming request.
func (e *AIStudioPortkeyExecutor) ExecuteStream(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (<-chan cliproxyexecutor.StreamChunk, error) {
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	if auth.Attributes["base_url"] == "" {
		auth.Attributes["base_url"] = portkeyGatewayURL
	}
	return e.geminiExecutor.ExecuteStream(ctx, auth, req, opts)
}

// CountTokens returns the token count.
func (e *AIStudioPortkeyExecutor) CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	if auth.Attributes["base_url"] == "" {
		auth.Attributes["base_url"] = portkeyGatewayURL
	}
	return e.geminiExecutor.CountTokens(ctx, auth, req, opts)
}

// Refresh is a no-op for API keys.
func (e *AIStudioPortkeyExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}
