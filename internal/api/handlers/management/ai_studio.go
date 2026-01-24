package management

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// ai-studio-key: []AIStudioKey
func (h *Handler) GetAIStudioKeys(c *gin.Context) {
	c.JSON(200, gin.H{"ai-studio-key": h.cfg.AIStudioKey})
}

func (h *Handler) PutAIStudioKeys(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []config.AIStudioKey
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []config.AIStudioKey `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil || len(obj.Items) == 0 {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	h.cfg.AIStudioKey = append([]config.AIStudioKey(nil), arr...)
	h.cfg.SanitizeAIStudioKeys()
	h.persist(c)
}

func (h *Handler) PatchAIStudioKey(c *gin.Context) {
	type aiStudioKeyPatch struct {
		APIKey         *string               `json:"api-key"`
		Prefix         *string               `json:"prefix"`
		BaseURL        *string               `json:"base-url"`
		ProxyURL       *string               `json:"proxy-url"`
		Headers        *map[string]string    `json:"headers"`
		ExcludedModels *[]string             `json:"excluded-models"`
		Models         *[]config.AIStudioModel `json:"models"`
	}
	var body struct {
		Index *int              `json:"index"`
		Match *string           `json:"match"`
		Value *aiStudioKeyPatch `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	targetIndex := -1
	if body.Index != nil && *body.Index >= 0 && *body.Index < len(h.cfg.AIStudioKey) {
		targetIndex = *body.Index
	}
	if targetIndex == -1 && body.Match != nil {
		match := strings.TrimSpace(*body.Match)
		if match != "" {
			for i := range h.cfg.AIStudioKey {
				if h.cfg.AIStudioKey[i].APIKey == match {
					targetIndex = i
					break
				}
			}
		}
	}
	if targetIndex == -1 {
		c.JSON(404, gin.H{"error": "item not found"})
		return
	}

	entry := h.cfg.AIStudioKey[targetIndex]
	if body.Value.APIKey != nil {
		trimmed := strings.TrimSpace(*body.Value.APIKey)
		if trimmed == "" {
			h.cfg.AIStudioKey = append(h.cfg.AIStudioKey[:targetIndex], h.cfg.AIStudioKey[targetIndex+1:]...)
			h.cfg.SanitizeAIStudioKeys()
			h.persist(c)
			return
		}
		entry.APIKey = trimmed
	}
	if body.Value.Prefix != nil {
		entry.Prefix = strings.TrimSpace(*body.Value.Prefix)
	}
	if body.Value.BaseURL != nil {
		entry.BaseURL = strings.TrimSpace(*body.Value.BaseURL)
	}
	if body.Value.ProxyURL != nil {
		entry.ProxyURL = strings.TrimSpace(*body.Value.ProxyURL)
	}
	if body.Value.Headers != nil {
		entry.Headers = config.NormalizeHeaders(*body.Value.Headers)
	}
	if body.Value.ExcludedModels != nil {
		entry.ExcludedModels = config.NormalizeExcludedModels(*body.Value.ExcludedModels)
	}
	if body.Value.Models != nil {
		entry.Models = append([]config.AIStudioModel(nil), (*body.Value.Models)...)
	}
	h.cfg.AIStudioKey[targetIndex] = entry
	h.cfg.SanitizeAIStudioKeys()
	h.persist(c)
}

func (h *Handler) DeleteAIStudioKey(c *gin.Context) {
	if val := strings.TrimSpace(c.Query("api-key")); val != "" {
		out := make([]config.AIStudioKey, 0, len(h.cfg.AIStudioKey))
		for _, v := range h.cfg.AIStudioKey {
			if v.APIKey != val {
				out = append(out, v)
			}
		}
		if len(out) != len(h.cfg.AIStudioKey) {
			h.cfg.AIStudioKey = out
			h.cfg.SanitizeAIStudioKeys()
			h.persist(c)
		} else {
			c.JSON(404, gin.H{"error": "item not found"})
		}
		return
	}
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		if _, err := fmt.Sscanf(idxStr, "%d", &idx); err == nil && idx >= 0 && idx < len(h.cfg.AIStudioKey) {
			h.cfg.AIStudioKey = append(h.cfg.AIStudioKey[:idx], h.cfg.AIStudioKey[idx+1:]...)
			h.cfg.SanitizeAIStudioKeys()
			h.persist(c)
			return
		}
	}
	c.JSON(400, gin.H{"error": "missing api-key or index"})
}
