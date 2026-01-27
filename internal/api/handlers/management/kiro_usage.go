package management

import (
	"context"
	"errors"
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

	client := kiroauth.NewKiroAuth(h.cfg)
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

		usage, err := client.GetUsageLimits(ctx, tokenData)
		if err != nil {
			entry.Error = fmt.Sprintf("usage limits error: %v", err)
			accounts = append(accounts, entry)
			continue
		}

		entry.SubscriptionTitle = usage.SubscriptionTitle
		entry.CurrentUsage = usage.CurrentUsage
		entry.UsageLimit = usage.UsageLimit
		entry.NextReset = usage.NextReset
		if usage.UsageLimit > 0 {
			entry.UsagePercent = (usage.CurrentUsage / usage.UsageLimit) * 100
		}

		models, errModels := client.ListAvailableModels(ctx, tokenData)
		if errModels == nil && len(models) > 0 {
			entry.AvailableModels = make([]string, 0, len(models))
			for _, m := range models {
				if m == nil {
					continue
				}
				id := strings.TrimSpace(m.ModelID)
				if id != "" {
					entry.AvailableModels = append(entry.AvailableModels, id)
				}
			}
		} else if errModels != nil && !errors.Is(errModels, context.DeadlineExceeded) {
			entry.Error = strings.TrimSpace(entry.Error + "; models error: " + errModels.Error())
		}

		accounts = append(accounts, entry)
	}

	c.JSON(200, gin.H{
		"accounts": accounts,
	})
}

