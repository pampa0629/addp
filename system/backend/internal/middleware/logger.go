package middleware

import (
	"bytes"
	"io"
	"log"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/addp/system/pkg/utils"
	"github.com/gin-gonic/gin"
)

func LoggerMiddleware(logService *service.LogService, userRepo *repository.UserRepository) gin.HandlerFunc {
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
		// 排除对内部审计日志 API 的记录（避免重复）
		if c.Request.Method != "GET" && c.Request.URL.Path != "/internal/audit-logs" {
			userID, exists := c.Get("user_id")
			username, _ := c.Get("username")

			auditLog := &models.AuditLog{
				Action:     c.Request.Method + " " + c.Request.URL.Path,
				IPAddress:  c.ClientIP(),
				ModuleName: "system", // 标记来源模块
			}

			// 脱敏请求体
			if requestBody != "" {
				auditLog.Details = utils.MaskSensitiveData(requestBody)
			}

			if exists {
				uid := userID.(uint)
				auditLog.UserID = &uid
				if username != nil {
					auditLog.Username = username.(string)
				}

				// 获取用户的租户ID
				user, err := userRepo.GetByID(uid)
				if err == nil && user.TenantID != nil {
					auditLog.TenantID = user.TenantID
				}
			}

			// 异步记录日志，避免阻塞请求
			go func(logData *models.AuditLog) {
				if err := logService.Create(logData); err != nil {
					log.Printf("Failed to create audit log: %v", err)
				}
			}(auditLog)
		}
	}
}