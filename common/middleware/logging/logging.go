package logging

import (
	"time"

	"github.com/addp/common/logger"
	"github.com/addp/common/middleware/requestid"
	"github.com/gin-gonic/gin"
)

// LoggingMiddleware 记录请求日志，包含 request ID
func LoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 记录请求开始时间
		startTime := time.Now()

		// 获取 request ID
		reqID := requestid.FromGinContext(c)

		// 记录请求信息
		logger.L().Info("request started",
			"request_id", reqID,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"query", c.Request.URL.RawQuery,
			"client_ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
		)

		// 处理请求
		c.Next()

		// 计算请求耗时
		duration := time.Since(startTime)

		// 记录请求完成信息
		logger.L().Info("request completed",
			"request_id", reqID,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", duration.Milliseconds(),
			"client_ip", c.ClientIP(),
		)

		// 如果有错误，记录错误日志
		if len(c.Errors) > 0 {
			for _, e := range c.Errors {
				logger.L().Error("request error",
					"request_id", reqID,
					"method", c.Request.Method,
					"path", c.Request.URL.Path,
					"error", e.Error(),
				)
			}
		}
	}
}
