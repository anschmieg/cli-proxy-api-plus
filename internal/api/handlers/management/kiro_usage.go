package management

import (
	"context"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
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

func (h *Handler) getCachedKiroQuota(authDir string, now time.Time) ([]kiroQuotaEntry, bool) {
	if h == nil {
		return nil, false
	}
	h.kiroQuotaCacheMu.Lock()
	defer h.kiroQuotaCacheMu.Unlock()
	if h.kiroQuotaCacheAuthDir != authDir {
		return nil, false
	}
	if h.kiroQuotaCacheUntil.IsZero() || now.After(h.kiroQuotaCacheUntil) {
		return nil, false
	}
	if len(h.kiroQuotaCacheData) == 0 {
		return []kiroQuotaEntry{}, true
	}
	out := append([]kiroQuotaEntry(nil), h.kiroQuotaCacheData...)
	return out, true
}

func (h *Handler) setCachedKiroQuota(authDir string, now time.Time, accounts []kiroQuotaEntry) {
	if h == nil {
		return
	}
	copied := append([]kiroQuotaEntry(nil), accounts...)
	h.kiroQuotaCacheMu.Lock()
	h.kiroQuotaCacheAuthDir = authDir
	h.kiroQuotaCacheUntil = now.Add(kiroQuotaCacheTTL)
	h.kiroQuotaCacheData = copied
	h.kiroQuotaCacheMu.Unlock()
}

func epochSecondsFromUpstream(epoch float64) (int64, bool) {
	if epoch <= 0 {
		return 0, false
	}
	// Upstream has been observed to return either epoch seconds or epoch milliseconds.
	// Use a simple heuristic: values >= 1e12 are milliseconds, otherwise seconds.
	seconds := int64(epoch)
	if epoch >= 1e12 {
		seconds = int64(epoch / 1000)
	}
	if seconds <= 0 {
		return 0, false
	}
	return seconds, true
}

func formatEpochMillis(epochMillis float64) string {
	seconds, ok := epochSecondsFromUpstream(epochMillis)
	if !ok {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
}

func usageValues(b kiroauth.UsageBreakdown) (current, limit float64) {
	if b.CurrentUsageWithPrecision != nil {
		current = *b.CurrentUsageWithPrecision
	} else if b.CurrentUsage != nil {
		current = float64(*b.CurrentUsage)
	}
	if b.UsageLimitWithPrecision != nil {
		limit = *b.UsageLimitWithPrecision
	} else if b.UsageLimit != nil {
		limit = float64(*b.UsageLimit)
	}
	if current < 0 {
		current = 0
	}
	if limit < 0 {
		limit = 0
	}
	return current, limit
}

func aggregateUsage(breakdowns []kiroauth.UsageBreakdown) (current, limit float64) {
	for _, breakdown := range breakdowns {
		c, l := usageValues(breakdown)
		if l <= 0 {
			continue
		}
		current += c
		limit += l
	}
	return current, limit
}

func chooseResetEpochSeconds(breakdowns []kiroauth.UsageBreakdown, fallback *float64, now time.Time) int64 {
	nowSeconds := now.Unix()
	var bestFuture int64
	var bestAny int64

	consider := func(epoch float64) {
		seconds, ok := epochSecondsFromUpstream(epoch)
		if !ok {
			return
		}
		if seconds >= nowSeconds {
			if bestFuture == 0 || seconds < bestFuture {
				bestFuture = seconds
			}
			return
		}
		if bestAny == 0 || seconds < bestAny {
			bestAny = seconds
		}
	}

	for _, breakdown := range breakdowns {
		if breakdown.NextDateReset != nil {
			consider(*breakdown.NextDateReset)
		}
	}
	if fallback != nil {
		consider(*fallback)
	}

	if bestFuture != 0 {
		return bestFuture
	}
	return bestAny
}

func usagePercent(current, limit float64) float64 {
	if limit <= 0 {
		return 0
	}
	percent := (current / limit) * 100
	if math.IsNaN(percent) || math.IsInf(percent, 0) {
		return 0
	}
	return percent
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

	if stat, err := os.Stat(authDir); err != nil || !stat.IsDir() {
		c.JSON(200, gin.H{"accounts": []kiroQuotaEntry{}})
		return
	}

	now := time.Now().UTC()
	if cached, ok := h.getCachedKiroQuota(authDir, now); ok {
		c.JSON(200, gin.H{"accounts": cached})
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
	sort.Strings(tokenFiles)

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
		entry.CurrentUsage, entry.UsageLimit = aggregateUsage(usageResp.UsageBreakdownList)

		resetEpoch := chooseResetEpochSeconds(
			usageResp.UsageBreakdownList,
			usageResp.NextDateReset,
			time.Now().UTC(),
		)
		if resetEpoch > 0 {
			entry.NextReset = time.Unix(resetEpoch, 0).UTC().Format(time.RFC3339)
		}
		entry.UsagePercent = usagePercent(entry.CurrentUsage, entry.UsageLimit)

		accounts = append(accounts, entry)
	}

	h.setCachedKiroQuota(authDir, now, accounts)

	c.JSON(200, gin.H{
		"accounts": accounts,
	})
}
