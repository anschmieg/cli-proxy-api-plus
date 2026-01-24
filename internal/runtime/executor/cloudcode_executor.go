package executor

import (
	"context"
	"strings"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	log "github.com/sirupsen/logrus"
)

// CloudCodeExecutor is a unified executor that routes requests to either
// Antigravity or Gemini CLI based on the requested model.
type CloudCodeExecutor struct {
	cfg                *config.Config
	antigravityExecutor *AntigravityExecutor
	geminiCLIExecutor   *GeminiCLIExecutor
}

// NewCloudCodeExecutor creates a new CloudCode executor instance.
func NewCloudCodeExecutor(cfg *config.Config) *CloudCodeExecutor {
	return &CloudCodeExecutor{
		cfg:                cfg,
		antigravityExecutor: NewAntigravityExecutor(cfg),
		geminiCLIExecutor:   NewGeminiCLIExecutor(cfg),
	}
}

// Identifier returns the executor identifier.
func (e *CloudCodeExecutor) Identifier() string { return "cloudcode" }

// getTargetExecutor determines which executor to use for a given model.
func (e *CloudCodeExecutor) getTargetExecutor(model string) cliproxyauth.ProviderExecutor {
	normalizedModel := strings.ToLower(model)

	// Route Claude and Gemini 3 models to Antigravity (Sandbox)
	if strings.Contains(normalizedModel, "claude") || strings.Contains(normalizedModel, "gemini-3") || strings.HasPrefix(normalizedModel, "antigravity-") {
		return e.antigravityExecutor
	}

	// Default to Gemini CLI (Production) for other models
	return e.geminiCLIExecutor
}

// PrepareRequest injects credentials into the outgoing HTTP request.
func (e *CloudCodeExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	// We don't know the model from the *http.Request easily here without parsing body,
	// but CloudCode usually uses GeminiCLI credentials for everything in our TS implementation.
	// Actually, in our TS implementation, buildCloudCodeHeaders just uses the accessToken.
	
	// Let's use GeminiCLI's PrepareRequest as default for CloudCode
	return e.geminiCLIExecutor.PrepareRequest(req, auth)
}

// HttpRequest executes an arbitrary HTTP request.
func (e *CloudCodeExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	// Delegate to GeminiCLI for arbitrary HTTP requests
	return e.geminiCLIExecutor.HttpRequest(ctx, auth, req)
}

// Execute performs a non-streaming request.
func (e *CloudCodeExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName
	target := e.getTargetExecutor(baseModel)
	
	log.Debugf("CloudCode routing model %s to executor %s", baseModel, target.Identifier())
	return target.Execute(ctx, auth, req, opts)
}

// ExecuteStream performs a streaming request.
func (e *CloudCodeExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (<-chan cliproxyexecutor.StreamChunk, error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName
	target := e.getTargetExecutor(baseModel)
	
	log.Debugf("CloudCode routing model %s to executor %s (stream)", baseModel, target.Identifier())
	return target.ExecuteStream(ctx, auth, req, opts)
}

// CountTokens returns the token count for the given request.
func (e *CloudCodeExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName
	target := e.getTargetExecutor(baseModel)
	
	return target.CountTokens(ctx, auth, req, opts)
}

// Refresh attempts to refresh provider credentials.
func (e *CloudCodeExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	// GeminiCLI and Antigravity use different refresh logic, but they both use Google OAuth.
	// GeminiCLI uses golang.org/x/oauth2 while Antigravity has custom logic.
	// In our TS project, they share the same token.
	
	// Delegate refresh to GeminiCLI executor as it's the more standard one for Google OAuth.
	return e.geminiCLIExecutor.Refresh(ctx, auth)
}
