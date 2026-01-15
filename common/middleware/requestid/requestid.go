package requestid

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// contextKey 定义请求 ID 在 context 中的 key 类型
type contextKey string

const (
	// RequestIDKey 是 request ID 在 context 中的 key
	RequestIDKey contextKey = "request_id"
	// RequestIDHeader 是 HTTP 头中的 request ID 字段名
	RequestIDHeader = "X-Request-ID"
)

// RequestIDMiddleware 为每个请求生成唯一的 request ID 并注入到 context 中
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 尝试从请求头获取 request ID
		requestID := c.GetHeader(RequestIDHeader)

		// 如果请求头中没有，生成新的 request ID
		if requestID == "" {
			requestID = uuid.New().String()
		}

		// 注入到 gin.Context 中
		c.Set(string(RequestIDKey), requestID)

		// 注入到 context.Context 中
		ctx := context.WithValue(c.Request.Context(), RequestIDKey, requestID)
		c.Request = c.Request.WithContext(ctx)

		// 设置响应头
		c.Header(RequestIDHeader, requestID)

		c.Next()
	}
}

// FromContext 从 context 中获取 request ID
func FromContext(ctx context.Context) string {
	if requestID, ok := ctx.Value(RequestIDKey).(string); ok {
		return requestID
	}
	return ""
}

// FromGinContext 从 gin.Context 中获取 request ID
func FromGinContext(c *gin.Context) string {
	if requestID, exists := c.Get(string(RequestIDKey)); exists {
		if id, ok := requestID.(string); ok {
			return id
		}
	}
	return ""
}
