package middleware

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"strings"
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
	ID             uint      `gorm:"primaryKey;column:id"`
	ApplicationID  *uint     `gorm:"column:application_id"`
	APIKeyPrefix   string    `gorm:"column:api_key_prefix;size:20"`
	ServiceName    string    `gorm:"column:service_name;size:255"`
	RequestMethod  string    `gorm:"column:request_method;size:10"`
	RequestPath    string    `gorm:"column:request_path;type:text"`
	RequestParams  string    `gorm:"column:request_params;type:jsonb"` // JSONB stored as string
	ResponseStatus int       `gorm:"column:response_status"`
	ResponseTimeMs int       `gorm:"column:response_time_ms"`
	CacheHit       bool      `gorm:"column:cache_hit;default:false"`
	RateLimited    bool      `gorm:"column:rate_limited;default:false"`
	AccessedAt     time.Time `gorm:"column:accessed_at;default:now()"`
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
		startTime := time.Now()

		// 使用 ResponseWriter wrapper 捕获响应状态
		writer := &responseWriter{
			ResponseWriter: c.Writer,
			statusCode:     200, // 默认 200
		}
		c.Writer = writer

		// 执行请求
		c.Next()

		responseTime := time.Since(startTime).Milliseconds()
		accessLog := m.buildAccessLog(c, writer.statusCode, int(responseTime))
		if accessLog != nil {
			go m.writeAccess(accessLog)
		}
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

func (m *AccessLoggerMiddleware) buildAccessLog(
	c *gin.Context,
	statusCode int,
	responseTimeMs int,
) *AccessLog {
	if m.db == nil {
		return nil
	}

	apiKeyInfoRaw, exists := c.Get("api_key_info")
	apiKeyInfo, ok := apiKeyInfoRaw.(*client.APIKeyValidationResponse)
	if !exists || !ok || apiKeyInfo == nil {
		return nil
	}

	apiKeyPrefix := ""
	if prefix, exists := c.Get("api_key_prefix"); exists {
		if p, ok := prefix.(string); ok {
			apiKeyPrefix = p
		}
	}

	cacheHit := false
	if hit, exists := c.Get("cache_hit"); exists {
		if h, ok := hit.(bool); ok {
			cacheHit = h
		}
	}

	serviceName := extractServiceName(c.Request.URL.Path)
	return &AccessLog{
		ApplicationID:  &apiKeyInfo.AppID,
		APIKeyPrefix:   apiKeyPrefix,
		ServiceName:    serviceName,
		RequestMethod:  c.Request.Method,
		RequestPath:    c.Request.URL.Path,
		RequestParams:  safeRequestParams(c.Request.URL.Query()),
		ResponseStatus: statusCode,
		ResponseTimeMs: responseTimeMs,
		CacheHit:       cacheHit,
		RateLimited:    statusCode == 429,
		AccessedAt:     time.Now(),
	}
}

func safeRequestParams(query url.Values) string {
	requestParams := make(map[string]any)
	if len(query) > 0 {
		safeQuery := make(url.Values, len(query))
		for key, values := range query {
			if isSensitiveParameterName(key) {
				safeQuery[key] = []string{"[REDACTED]"}
				continue
			}
			safeQuery[key] = append([]string(nil), values...)
		}
		requestParams["query"] = safeQuery
	}
	paramsJSON, _ := json.Marshal(requestParams)
	return string(paramsJSON)
}

func isSensitiveParameterName(name string) bool {
	normalized := strings.NewReplacer("-", "_", ".", "_", "[", "_", "]", "_").Replace(strings.ToLower(name))
	for _, marker := range []string{
		"password", "passwd", "secret", "token", "credential", "authorization", "cookie",
		"challenge", "verifier", "signature", "private_key", "api_key", "nonce", "otp",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return normalized == "code" || normalized == "state" || normalized == "key" ||
		strings.HasSuffix(normalized, "_code") || strings.HasSuffix(normalized, "_state") ||
		strings.HasSuffix(normalized, "_key")
}

func (m *AccessLoggerMiddleware) writeAccess(accessLog *AccessLog) {
	if m == nil || m.db == nil || accessLog == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.db.WithContext(ctx).Create(accessLog).Error; err != nil {
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
