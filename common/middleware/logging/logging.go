package logging

import (
	"github.com/addp/common/httpmetrics"
	"github.com/addp/common/logger"
	"github.com/addp/common/middleware/requestid"
	"github.com/gin-gonic/gin"
	"log/slog"
	"time"
)

// LoggingMiddleware is the sole bounded HTTP request log. Framework templates
// replace raw paths; body, credentials, peer addresses and raw errors stay out.
func LoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		defer func() {
			level := slog.LevelInfo
			if httpmetrics.FailureType(c) != "" {
				level = slog.LevelError
			} else if c.Writer.Status() >= 400 {
				level = slog.LevelWarn
			}
			logger.L().Log(c.Request.Context(), level, "http.request.completed", "request_id", requestid.FromGinContext(c), "method", httpmetrics.Process().CanonicalMethod(c.Request.Method), "route", c.FullPath(), "status", c.Writer.Status(), "duration_ms", time.Since(started).Milliseconds())
		}()
		c.Next()
	}
}
