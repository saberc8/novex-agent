package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// MaxRequestBodySize bounds every request body before logging, multipart
// parsing, or JSON decoding can allocate memory for it.
func MaxRequestBodySize(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if maxBytes <= 0 || c.Request.Body == nil {
			c.Next()
			return
		}
		if c.Request.ContentLength > maxBytes {
			Fail(c, "413", "请求体超过允许大小")
			c.Abort()
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
