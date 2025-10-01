package middleware

import (
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

func LoggerMiddleware(logService *service.LogService, userRepo *repository.UserRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// 记录审计日志（仅记录非 GET 请求）
		if c.Request.Method != "GET" {
			userID, exists := c.Get("user_id")
			username, _ := c.Get("username")

			log := &models.AuditLog{
				Action:    c.Request.Method + " " + c.Request.URL.Path,
				IPAddress: c.ClientIP(),
			}

			if exists {
				uid := userID.(uint)
				log.UserID = &uid
				if username != nil {
					log.Username = username.(string)
				}

				// 获取用户的租户ID
				user, err := userRepo.GetByID(uid)
				if err == nil && user.TenantID != nil {
					log.TenantID = user.TenantID
				}
			}

			logService.Create(log)
		}
	}
}