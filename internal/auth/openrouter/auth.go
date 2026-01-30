package openrouter

import (
	"context"
	"fmt"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
)

// OpenRouterAuth handles OpenRouter authentication.
// Since OpenRouter primarily uses API keys, this is largely a placeholder
// or a wrapper for potential future OAuth flows if supported.
type OpenRouterAuth struct {
	httpClient *http.Client
}

// NewOpenRouterAuth creates a new OpenRouter authentication service.
func NewOpenRouterAuth(cfg *config.Config) *OpenRouterAuth {
	return &OpenRouterAuth{
		httpClient: util.SetProxy(&cfg.SDKConfig, &http.Client{}),
	}
}

// RefreshTokens is a no-op for API-key based auth, but included for interface consistency.
func (o *OpenRouterAuth) RefreshTokens(ctx context.Context, refreshToken string) (*OpenRouterTokenData, error) {
	return nil, fmt.Errorf("refresh not supported for OpenRouter API keys")
}

// UpdateTokenStorage updates the storage with new token data.
func (o *OpenRouterAuth) UpdateTokenStorage(storage *OpenRouterTokenStorage, tokenData *OpenRouterTokenData) {
	storage.AccessToken = tokenData.AccessToken
	storage.RefreshToken = tokenData.RefreshToken
	storage.Email = tokenData.Email
	storage.Expire = tokenData.Expire
}
