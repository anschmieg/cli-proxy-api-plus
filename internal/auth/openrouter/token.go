package openrouter

// OpenRouterTokenStorage defines the structure for storing OpenRouter tokens
type OpenRouterTokenStorage struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	LastRefresh  string `json:"last_refresh,omitempty"`
	Email        string `json:"email,omitempty"`
	Expire       string `json:"expire,omitempty"`
}

// SaveTokenToFile saves the token storage to a file
func (s *OpenRouterTokenStorage) SaveTokenToFile(authFilePath string) error {
	// Implementation would use json.Marshal and os.WriteFile
	// For now, this is a placeholder to satisfy the interface if needed
	return nil
}

// OpenRouterAuthBundle represents the bundle of auth data
type OpenRouterAuthBundle struct {
	TokenData   OpenRouterTokenData
	LastRefresh string
}

// OpenRouterTokenData represents the token response data
type OpenRouterTokenData struct {
	AccessToken  string
	RefreshToken string
	Email        string
	Expire       string
}
