package audit

import (
	"bytes"
	"io"
	"log"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AuditMiddleware 统一审计日志中间件（增强版）
// 用于各个模块记录审计日志到 System 模块
//
// 新增功能：
// - 记录 HTTP 状态码和请求耗时
// - 自动判断日志级别（INFO/WARN/ERROR）
// - 自动提取资源类型和 ID
// - 支持系统用户（无认证请求）
// - 生成请求追踪 ID
func AuditMiddleware(moduleName string, systemClient *client.SystemClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 记录开始时间
		startTime := time.Now()

		// 生成请求追踪 ID
		requestID := uuid.New().String()
		c.Set("request_id", requestID)

		// 读取请求体（需要在 c.Next() 之前）
		var requestBody string
		if c.Request.Body != nil && c.Request.Method != "GET" {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err == nil {
				requestBody = string(bodyBytes)
				// 重新设置请求体，以便后续处理器可以读取
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}
		}

		// 执行业务逻辑
		c.Next()

		// 异步记录审计日志（仅记录非 GET 请求）
		if c.Request.Method != "GET" {
			// 计算请求耗时
			durationMs := int(time.Since(startTime).Milliseconds())

			// 获取 HTTP 状态码
			httpStatus := c.Writer.Status()

			// 判断日志级别
			logLevel := getLogLevel(httpStatus)

			// 提取错误信息（如果有）
			var errorMessage string
			if len(c.Errors) > 0 {
				errorMessage = c.Errors.String()
			}

			// 解析资源类型和 ID
			entityInfo := ParseEntityFromPath(c.Request.Method, c.Request.URL.Path)

			// 获取用户信息（或使用系统用户）
			userID, userIDExists := c.Get("user_id")
			username, usernameExists := c.Get("username")
			tenantID, tenantIDExists := c.Get("tenant_id")

			auditLog := &models.AuditLogCreateRequest{
				HTTPMethod:   c.Request.Method,
				ResourcePath: c.Request.URL.Path,
				QueryParams:  c.Request.URL.RawQuery,
				UserAgent:    c.Request.UserAgent(),
				IPAddress:    c.ClientIP(),
				RequestBody:  requestBody,
				ModuleName:   moduleName,
				HTTPStatus:   httpStatus,
				DurationMs:   durationMs,
				LogLevel:     logLevel,
				ErrorMessage: errorMessage,
				RequestID:    requestID,
			}

			// 设置资源类型和 ID
			if entityInfo != nil {
				auditLog.EntityType = entityInfo.Type
				auditLog.EntityID = entityInfo.ID
			}

			// 设置用户信息（或系统用户）
			if userIDExists && usernameExists {
				// 真实用户请求
				uid := userID.(uint)
				uname := username.(string)
				auditLog.UserID = &uid
				unameStr := uname
				auditLog.Username = &unameStr

				if tenantIDExists {
					tid := tenantID.(uint)
					auditLog.TenantID = &tid
				}
			} else {
				// 系统调用（无认证信息）
				sysUID := models.SystemUserIDInternal
				sysUname := models.GetSystemUserName(c.Request.URL.Path)
				auditLog.UserID = &sysUID
				auditLog.Username = &sysUname
				// tenantID 保持为 nil（系统调用无租户）
			}

			// 异步记录日志到 System 模块
			go func(logData *models.AuditLogCreateRequest) {
				if err := systemClient.CreateAuditLog(logData); err != nil {
					log.Printf("[%s] Failed to create audit log (RequestID: %s): %v",
						moduleName, requestID, err)
				}
			}(auditLog)
		}
	}
}

// getLogLevel 根据 HTTP 状态码判断日志级别
func getLogLevel(httpStatus int) string {
	switch {
	case httpStatus >= 200 && httpStatus < 400:
		return "INFO" // 成功和重定向
	case httpStatus >= 400 && httpStatus < 500:
		return "WARN" // 客户端错误
	case httpStatus >= 500:
		return "ERROR" // 服务器错误
	default:
		return "INFO"
	}
}
