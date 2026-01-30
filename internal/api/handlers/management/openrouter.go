package management

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type openRouterUsageResponse struct {
	TotalCredits float64 `json:"total_credits"`
	TotalUsage   float64 `json:"total_usage"`
	Limit        *float64 `json:"limit"`
	IsFreeTier   bool    `json:"is_free_tier"`
}

// GetOpenRouterUsage fetches usage and credit information from OpenRouter API.
// It looks for a configured OpenAI-compatible provider with 'openrouter' in the base URL
// to get the API key.
func (h *Handler) GetOpenRouterUsage(c *gin.Context) {
	if h.cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "configuration not available"})
		return
	}

	apiKey := ""
	// Find OpenRouter API key in config
	// Logic: Look for OpenAICompatibility entries where BaseURL contains "openrouter.ai"
	for _, provider := range h.cfg.OpenAICompatibility {
		if strings.Contains(strings.ToLower(provider.BaseURL), "openrouter.ai") {
			if len(provider.APIKeyEntries) > 0 {
				apiKey = provider.APIKeyEntries[0].APIKey
				break
			}
		}
	}

	if apiKey == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "OpenRouter API key not configured"})
		return
	}

	// Call OpenRouter API
	// OpenRouter endpoint for credits: https://openrouter.ai/api/v1/credits
	req, err := http.NewRequestWithContext(c.Request.Context(), "GET", "https://openrouter.ai/api/v1/credits", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request: " + err.Error()})
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// Use a standard http client
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to contact OpenRouter: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.JSON(http.StatusBadGateway, gin.H{
			"error":       "OpenRouter returned error status",
			"status_code": resp.StatusCode,
			"details":     string(body),
		})
		return
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decode OpenRouter response"})
		return
	}

	// Transform to our response format if needed, or pass through data part
	// OpenRouter /credits response format:
	// { "data": { "total_credits": 123.45, "total_usage": 67.89, "limit": null, "is_free_tier": false } }
	if dataData, ok := data["data"].(map[string]interface{}); ok {
		c.JSON(http.StatusOK, dataData)
	} else {
		// Fallback if format is unexpected
		c.JSON(http.StatusOK, data)
	}
}
