package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"time"

	"github.com/addp/gateway/pkg/client"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AccessLoggerMiddleware 访问日志中间件
type AccessLoggerMiddleware struct {
	db *gorm.DB
}

// AccessLog 访问日志模型
type AccessLog struct {
	ID               uint      `gorm:"primaryKey;column:id"`
	ApplicationID    *uint     `gorm:"column:application_id"`
	APIKeyPrefix     string    `gorm:"column:api_key_prefix;size:20"`
	ServiceName      string    `gorm:"column:service_name;size:255"`
	RequestMethod    string    `gorm:"column:request_method;size:10"`
	RequestPath      string    `gorm:"column:request_path;type:text"`
	RequestParams    string    `gorm:"column:request_params;type:jsonb"` // JSONB stored as string
	ResponseStatus   int       `gorm:"column:response_status"`
	ResponseTimeMs   int       `gorm:"column:response_time_ms"`
	CacheHit         bool      `gorm:"column:cache_hit;default:false"`
	RateLimited      bool      `gorm:"column:rate_limited;default:false"`
	AccessedAt       time.Time `gorm:"column:accessed_at;default:now()"`
}

// TableName 指定表名
func (AccessLog) TableName() string {
	return "gateway.api_access_logs"
}

// NewAccessLoggerMiddleware 创建访问日志中间件
func NewAccessLoggerMiddleware(db *gorm.DB) *AccessLoggerMiddleware {
	return &AccessLoggerMiddleware{
		db: db,
	}
}

// Handler 中间件处理函数
func (m *AccessLoggerMiddleware) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 记录开始时间
		startTime := time.Now()

		// 读取请求体（用于日志）
		var requestBody []byte
		if c.Request.Body != nil {
			requestBody, _ = io.ReadAll(c.Request.Body)
			// 重置请求体供后续处理器使用
			c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
		}

		// 使用 ResponseWriter wrapper 捕获响应状态
		writer := &responseWriter{
			ResponseWriter: c.Writer,
			statusCode:     200, // 默认 200
		}
		c.Writer = writer

		// 执行请求
		c.Next()

		// 计算响应时间
		responseTime := time.Since(startTime).Milliseconds()

		// 异步记录日志（避免阻塞请求）
		go m.logAccess(c, writer.statusCode, int(responseTime), requestBody)
	}
}

// responseWriter 自定义 ResponseWriter 用于捕获状态码
type responseWriter struct {
	gin.ResponseWriter
	statusCode int
}

func (w *responseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// logAccess 记录访问日志
func (m *AccessLoggerMiddleware) logAccess(
	c *gin.Context,
	statusCode int,
	responseTimeMs int,
	requestBody []byte,
) {
	if m.db == nil {
		return
	}

	// 提取 API Key 信息（由 APIKeyAuthMiddleware 设置）
	var applicationID *uint
	var apiKeyPrefix string
	var cacheHit bool
	var rateLimited bool

	if apiKeyInfoRaw, exists := c.Get("api_key_info"); exists {
		if apiKeyInfo, ok := apiKeyInfoRaw.(*client.APIKeyValidationResponse); ok && apiKeyInfo != nil {
			applicationID = &apiKeyInfo.AppID
		}
	}

	if prefix, exists := c.Get("api_key_prefix"); exists {
		if p, ok := prefix.(string); ok {
			apiKeyPrefix = p
		}
	}

	if hit, exists := c.Get("cache_hit"); exists {
		if h, ok := hit.(bool); ok {
			cacheHit = h
		}
	}

	// 检查是否被限流（status 429）
	rateLimited = (statusCode == 429)

	// 提取服务名称（从路由推断）
	serviceName := extractServiceName(c.Request.URL.Path)

	// 构建请求参数 JSON
	requestParams := make(map[string]interface{})

	// 添加 query 参数
	if len(c.Request.URL.Query()) > 0 {
		requestParams["query"] = c.Request.URL.Query()
	}

	// 添加 body 参数（如果是 JSON）
	if len(requestBody) > 0 {
		var bodyJSON map[string]interface{}
		if err := json.Unmarshal(requestBody, &bodyJSON); err == nil {
			requestParams["body"] = bodyJSON
		}
	}

	// 序列化请求参数
	paramsJSON, _ := json.Marshal(requestParams)

	// 创建日志记录
	accessLog := AccessLog{
		ApplicationID:  applicationID,
		APIKeyPrefix:   apiKeyPrefix,
		ServiceName:    serviceName,
		RequestMethod:  c.Request.Method,
		RequestPath:    c.Request.URL.Path,
		RequestParams:  string(paramsJSON),
		ResponseStatus: statusCode,
		ResponseTimeMs: responseTimeMs,
		CacheHit:       cacheHit,
		RateLimited:    rateLimited,
		AccessedAt:     time.Now(),
	}

	// 写入数据库
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.db.WithContext(ctx).Create(&accessLog).Error; err != nil {
		log.Printf("Failed to write access log: %v", err)
	}
}

// extractServiceName 从请求路径推断服务名称
func extractServiceName(path string) string {
	// 简单的路径匹配规则
	if len(path) < 2 {
		return "unknown"
	}

	// 跳过 /api 前缀
	if len(path) > 4 && path[:4] == "/api" {
		path = path[4:]
	}

	// 提取第一个路径段
	for i := 1; i < len(path); i++ {
		if path[i] == '/' {
			return path[1:i]
		}
	}

	return path[1:]
}

// CleanupOldLogs 清理旧日志（建议定期调用，例如每天清理 30 天前的日志）
func (m *AccessLoggerMiddleware) CleanupOldLogs(days int) error {
	if m.db == nil {
		return nil
	}

	cutoffTime := time.Now().AddDate(0, 0, -days)

	result := m.db.Where("accessed_at < ?", cutoffTime).Delete(&AccessLog{})
	if result.Error != nil {
		return result.Error
	}

	log.Printf("Cleaned up %d old access logs (older than %d days)", result.RowsAffected, days)
	return nil
}
