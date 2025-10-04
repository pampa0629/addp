package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// UserInfo 从System服务返回的用户信息
type UserInfo struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	TenantID *uint  `json:"tenant_id"` // 可能为null
}

// AuthMiddleware 认证中间件 - 通过System服务验证token
func AuthMiddleware(systemServiceURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取Authorization头
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}

		// 检查Bearer格式
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header format"})
			c.Abort()
			return
		}

		// 调用System服务验证token
		req, err := http.NewRequest("GET", systemServiceURL+"/api/users/me", nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
			c.Abort()
			return
		}
		req.Header.Set("Authorization", authHeader)

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to verify token with system service", "details": err.Error()})
			c.Abort()
			return
		}
		defer resp.Body.Close()

		// 如果System返回非200，说明token无效
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token", "details": string(body)})
			c.Abort()
			return
		}

		// 解析用户信息
		var userInfo UserInfo
		if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse user info"})
			c.Abort()
			return
		}

		// 设置用户信息到上下文
		c.Set("user_id", userInfo.ID)
		c.Set("username", userInfo.Username)

		// tenant_id 可能为null，设置为0
		if userInfo.TenantID != nil {
			c.Set("tenant_id", *userInfo.TenantID)
		} else {
			c.Set("tenant_id", uint(0))
		}

		c.Next()
	}
}

// GetUserID 从上下文获取用户ID
func GetUserID(c *gin.Context) uint {
	if userID, exists := c.Get("user_id"); exists {
		return userID.(uint)
	}
	return 0
}

// GetTenantID 从上下文获取租户ID
func GetTenantID(c *gin.Context) uint {
	if tenantID, exists := c.Get("tenant_id"); exists {
		return tenantID.(uint)
	}
	return 0
}

// GetUsername 从上下文获取用户名
func GetUsername(c *gin.Context) string {
	if username, exists := c.Get("username"); exists {
		return username.(string)
	}
	return ""
}
