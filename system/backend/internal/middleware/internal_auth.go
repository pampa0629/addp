package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/addp/system/internal/config"
	"github.com/gin-gonic/gin"
)

func InternalAPIMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		provided := c.GetHeader("X-Internal-API-Key")
		expected := cfg.InternalAPIKey
		if expected == "" || len(provided) != len(expected) ||
			subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}
