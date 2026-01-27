package management

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	kiroauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/kiro"
)

type kiroQuotaEntry struct {
	ID                string   `json:"id"`
	Email             string   `json:"email,omitempty"`
	SubscriptionTitle string   `json:"subscription_title,omitempty"`
	CurrentUsage      float64  `json:"current_usage"`
	UsageLimit        float64  `json:"usage_limit"`
	UsagePercent      float64  `json:"usage_percent"`
	NextReset         string   `json:"next_reset,omitempty"`
	AvailableModels   []string `json:"available_models,omitempty"`
	Error             string   `json:"error,omitempty"`
}

// GetKiroQuotaStatus returns per-account Kiro quota usage and model availability.
// It scans the configured auth directory for kiro-*.json files and queries
// the upstream usage limits endpoint for each token.
func (h *Handler) GetKiroQuotaStatus(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(500, gin.H{"error": "management handler not initialized"})
		return
	}

	authDir := strings.TrimSpace(h.cfg.AuthDir)
	if authDir == "" {
		c.JSON(400, gin.H{"error": "auth-dir is not configured"})
		return
	}

	var tokenFiles []string
	_ = filepath.WalkDir(authDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if strings.HasPrefix(name, "kiro-") && strings.HasSuffix(name, ".json") {
			tokenFiles = append(tokenFiles, path)
		}
		return nil
	})

	if len(tokenFiles) == 0 {
		c.JSON(200, gin.H{"accounts": []kiroQuotaEntry{}})
		return
	}

	cwClient := kiroauth.NewCodeWhispererClient(h.cfg, "")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	accounts := make([]kiroQuotaEntry, 0, len(tokenFiles))
	for _, tokenPath := range tokenFiles {
		entry := kiroQuotaEntry{ID: filepath.Base(tokenPath)}

		storage, err := kiroauth.LoadFromFile(tokenPath)
		if err != nil {
			entry.Error = fmt.Sprintf("failed to load token: %v", err)
			accounts = append(accounts, entry)
			continue
		}
		entry.Email = strings.TrimSpace(storage.Email)

		tokenData := storage.ToTokenData()

		usageResp, err := cwClient.GetUsageLimits(ctx, tokenData.AccessToken)
		if err != nil {
			entry.Error = fmt.Sprintf("usage limits error: %v", err)
			accounts = append(accounts, entry)
			continue
		}

		if usageResp.SubscriptionInfo != nil {
			entry.SubscriptionTitle = usageResp.SubscriptionInfo.SubscriptionTitle
		}
		if usageResp.NextDateReset != nil {
			entry.NextReset = fmt.Sprintf("%v", *usageResp.NextDateReset)
		}
		if len(usageResp.UsageBreakdownList) > 0 {
			first := usageResp.UsageBreakdownList[0]
			if first.CurrentUsageWithPrecision != nil {
				entry.CurrentUsage = *first.CurrentUsageWithPrecision
			}
			if first.UsageLimitWithPrecision != nil {
				entry.UsageLimit = *first.UsageLimitWithPrecision
			}
		}
		if entry.UsageLimit > 0 {
			entry.UsagePercent = (entry.CurrentUsage / entry.UsageLimit) * 100
		}

		accounts = append(accounts, entry)
	}

	c.JSON(200, gin.H{
		"accounts": accounts,
	})
}
