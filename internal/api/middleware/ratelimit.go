package middleware

import (
	"net/http"
	"sync"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/ratelimit"
)

type RateLimitMiddleware struct {
	ips    sync.Map // map[string]*ratelimit.TokenBucket
	rate   float64
	burst  float64
}

func NewRateLimitMiddleware(rate float64, burst float64) *RateLimitMiddleware {
	return &RateLimitMiddleware{
		rate:  rate,
		burst: burst,
	}
}

func (m *RateLimitMiddleware) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		
		// If manual approval is present, skip rate limit check
		if approved, ok := c.Get("manual_approval"); ok && approved.(bool) {
			c.Next()
			return
		}

		// Get or create limiter for IP
		limiterI, _ := m.ips.LoadOrStore(ip, ratelimit.NewTokenBucket(m.rate, m.burst))
		limiter := limiterI.(*ratelimit.TokenBucket)

		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{
					"message": "Rate limit exceeded",
					"type":    "rate_limit_error",
					"code":    "rate_limit_exceeded",
				},
			})
			return
		}

		c.Next()
	}
}

// Cleanup removes old entries to prevent memory leaks (optional, run periodically)
func (m *RateLimitMiddleware) Cleanup() {
	// Implementation would require tracking last access time in the map value
	// For simplicity, we skip this for now or could implement a TTL cache later.
}
