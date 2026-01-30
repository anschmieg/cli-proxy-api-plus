package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	// ManualApprovalHeader is the header name used to indicate manual approval.
	ManualApprovalHeader = "X-Manual-Approval"
	// ManualApprovalQueryParam is the query parameter name used to indicate manual approval.
	ManualApprovalQueryParam = "manual_approval"
	// ManualApprovalValue is the expected value for manual approval header/query param.
	ManualApprovalValue = "true"
)

// ManualApprovalMiddleware creates a middleware that allows requests to bypass certain
// restrictions (e.g., rate limits) if a specific header or query parameter is present.
func ManualApprovalMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check for header
		headerValue := c.GetHeader(ManualApprovalHeader)
		if strings.EqualFold(headerValue, ManualApprovalValue) {
			c.Set("manual_approval", true)
			c.Next()
			return
		}

		// Check for query parameter
		queryValue := c.Query(ManualApprovalQueryParam)
		if strings.EqualFold(queryValue, ManualApprovalValue) {
			c.Set("manual_approval", true)
			c.Next()
			return
		}

		c.Set("manual_approval", false)
		c.Next()
	}
}
