package audit

import (
	"bytes"
	"io"
	"log"

	"github.com/addp/common/client"
	"github.com/addp/common/models"
	"github.com/gin-gonic/gin"
)

// AuditMiddleware 统一审计日志中间件
// 用于各个模块记录审计日志到 System 模块
func AuditMiddleware(moduleName string, systemClient *client.SystemClient) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		c.Next()

		// 异步记录审计日志（仅记录非 GET 请求）
		if c.Request.Method != "GET" {
			userID, userIDExists := c.Get("user_id")
			username, _ := c.Get("username")
			tenantID, _ := c.Get("tenant_id")

			auditLog := &models.AuditLogCreateRequest{
				Action:     c.Request.Method + " " + c.Request.URL.Path,
				IPAddress:  c.ClientIP(),
				Details:    requestBody,
				ModuleName: moduleName, // 记录来源模块
			}

			// 设置用户信息
			if userIDExists {
				uid := userID.(uint)
				auditLog.UserID = &uid
			}
			if username != nil {
				uname := username.(string)
				auditLog.Username = &uname
			}
			if tenantID != nil {
				tid := tenantID.(uint)
				auditLog.TenantID = &tid
			}

			// 异步记录日志到 System 模块
			go func(logData *models.AuditLogCreateRequest) {
				if err := systemClient.CreateAuditLog(logData); err != nil {
					log.Printf("[%s] Failed to create audit log: %v", moduleName, err)
				}
			}(auditLog)
		}
	}
}
