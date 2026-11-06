package logging

import (
	"log/slog"
	"time"

	"github.com/addp/common/httpmetrics"
	"github.com/addp/common/logger"
	"github.com/addp/common/middleware/requestid"
	"github.com/gin-gonic/gin"
)

const completionDiagnosticsKey = "addp.http.completion_diagnostics"

// SetCompletionDiagnostics attaches one bounded, server-owned diagnostic group
// to the request completion log. Callers must supply allowlisted fields only.
func SetCompletionDiagnostics(c *gin.Context, diagnostics ...slog.Attr) {
	var fields []slog.Attr
	for _, diagnostic := range diagnostics {
		if diagnostic.Key != "" {
			fields = append(fields, diagnostic)
		}
	}
	if len(fields) != 0 {
		c.Set(completionDiagnosticsKey, slog.Attr{Key: "diagnostics", Value: slog.GroupValue(fields...)})
	}
}

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
			attrs := []slog.Attr{
				slog.String("request_id", requestid.FromGinContext(c)),
				slog.String("method", httpmetrics.Process().CanonicalMethod(c.Request.Method)),
				slog.String("route", c.FullPath()),
				slog.Int("status", c.Writer.Status()),
				slog.Int64("duration_ms", time.Since(started).Milliseconds()),
			}
			if diagnostic, exists := c.Get(completionDiagnosticsKey); exists {
				attrs = append(attrs, diagnostic.(slog.Attr))
			}
			logger.L().LogAttrs(c.Request.Context(), level, "http.request.completed", attrs...)
		}()
		c.Next()
	}
}
