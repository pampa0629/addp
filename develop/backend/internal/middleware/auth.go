package middleware

import (
	commonAuth "github.com/addp/common/middleware/auth"
	commonCors "github.com/addp/common/middleware/cors"
	"github.com/gin-gonic/gin"
)

// AuthMiddleware JWT 认证中间件 (委托给 System 服务验证)
func AuthMiddleware(systemURL string) gin.HandlerFunc {
	return commonAuth.SystemAuthMiddleware(systemURL)
}

// CORS 中间件
func CORS() gin.HandlerFunc {
	return commonCors.CORS()
}
