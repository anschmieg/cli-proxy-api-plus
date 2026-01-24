package management

import (
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// rate-limit: RateLimitConfig
func (h *Handler) GetRateLimit(c *gin.Context) {
	if h.cfg == nil {
		c.JSON(200, gin.H{"rate-limit": config.RateLimitConfig{}})
		return
	}
	c.JSON(200, gin.H{"rate-limit": h.cfg.RateLimit})
}

func (h *Handler) PutRateLimit(c *gin.Context) {
	var body struct {
		Value config.RateLimitConfig `json:"value"`
	}
	// Support direct object or wrapped in value
	if err := c.ShouldBindJSON(&body); err != nil {
		// Try binding directly
		var direct config.RateLimitConfig
		if err2 := c.ShouldBindJSON(&direct); err2 == nil {
			h.cfg.RateLimit = direct
			h.persist(c)
			return
		}
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	h.cfg.RateLimit = body.Value
	h.persist(c)
}

func (h *Handler) PatchRateLimit(c *gin.Context) {
	type rateLimitPatch struct {
		Enabled           *bool    `json:"enabled"`
		RequestsPerMinute *float64 `json:"requests-per-minute"`
		Burst             *int     `json:"burst"`
	}
	var body rateLimitPatch
	if err := c.ShouldBindJSON(&body); err != nil {
		// Try wrapped in value
		var wrapper struct {
			Value rateLimitPatch `json:"value"`
		}
		if err2 := c.ShouldBindJSON(&wrapper); err2 == nil {
			body = wrapper.Value
		} else {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
	}

	if body.Enabled != nil {
		h.cfg.RateLimit.Enabled = *body.Enabled
	}
	if body.RequestsPerMinute != nil {
		h.cfg.RateLimit.RequestsPerMinute = *body.RequestsPerMinute
	}
	if body.Burst != nil {
		h.cfg.RateLimit.Burst = *body.Burst
	}
	h.persist(c)
}
