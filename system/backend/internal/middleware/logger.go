package middleware

import (
	"bytes"
	"io"
	"strings"
	"time"

	"github.com/addp/common/logger"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/addp/system/pkg/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func LoggerMiddleware(logService *service.LogService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 过滤内部 API (不记录)
		if strings.HasPrefix(c.Request.URL.Path, "/internal/") {
			c.Next()
			return
		}

		// 过滤 GET 请求 (可选,根据需求调整)
		if c.Request.Method == "GET" {
			c.Next()
			return
		}

		// ✅ 1. 记录开始时间
		startTime := time.Now()

		// ✅ 2. 生成 request_id
		requestID := uuid.New().String()
		c.Set("request_id", requestID) // 可在业务代码中使用

		// 3. 读取请求体
		var requestBody string
		if c.Request.Body != nil {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err == nil {
				requestBody = string(bodyBytes)
				// 重新设置请求体,以便后续处理器可以读取
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}
		}

		// 4. 执行业务逻辑
		c.Next()

		// ✅ 5. 计算耗时
		duration := time.Since(startTime).Milliseconds()

		// ✅ 6. 获取响应状态码
		statusCode := c.Writer.Status()

		// ✅ 7. 提取用户信息 (SuperAdmin 处理)
		var userID *uint
		var username string
		var tenantID *uint

		if uid, exists := c.Get("user_id"); exists {
			id := uid.(uint)
			userID = &id

			// ✅ SuperAdmin 也有 username
			if uname, ok := c.Get("username"); ok {
				username = uname.(string)
			}

			// ✅ tenant_id 可能为 NULL (SuperAdmin)
			if tid, ok := c.Get("tenant_id"); ok && tid != nil {
				// 修复: tid 是 uint 类型，不是 *uint
				tidUint := tid.(uint)
				tenantID = &tidUint
			}
			// ✅ SuperAdmin 的 tenant_id 保持 NULL
		}

		// ✅ 8. 提取 entity 信息
		entityType, entityID := utils.ExtractResourceInfo(
			c.Request.Method,
			c.Request.URL.Path,
			requestBody,
		)

		// ✅ 9. 确定日志级别
		logLevel := "INFO"
		if statusCode >= 500 {
			logLevel = "ERROR"
		} else if statusCode >= 400 {
			logLevel = "WARN"
		}

		// ✅ 10. 提取错误信息 (从 context 或响应)
		var errorMessage string
		if errMsg, exists := c.Get("error"); exists {
			if err, ok := errMsg.(error); ok {
				errorMessage = err.Error()
			} else if msg, ok := errMsg.(string); ok {
				errorMessage = msg
			}
		}

		// ✅ 11. 构建审计日志
		auditLog := &models.AuditLog{
			UserID:       userID,
			Username:     username,
			TenantID:     tenantID,
			HTTPMethod:   c.Request.Method,
			ResourcePath: c.Request.URL.Path,
			HTTPStatus:   statusCode,           // ✅ 必填
			DurationMs:   int(duration),        // ✅ 必填
			EntityType:   entityType,
			EntityID:     entityID,
			RequestBody:  utils.MaskSensitiveData(requestBody),
			QueryParams:  c.Request.URL.RawQuery,
			UserAgent:    c.Request.UserAgent(),
			IPAddress:    c.ClientIP(),
			LogLevel:     logLevel,
			ErrorMessage: errorMessage,
			RequestID:    requestID,            // ✅ 必填
			ModuleName:   "system",
		}

		// ✅ 12. 异步写入 (保持性能)
		go func(logData *models.AuditLog) {
			if err := logService.Create(logData); err != nil {
				logger.L().Error("Failed to create audit log", "error", err)
			}
		}(auditLog)
	}
}