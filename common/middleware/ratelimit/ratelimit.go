package ratelimit

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ulule/limiter/v3"
	mgin "github.com/ulule/limiter/v3/drivers/middleware/gin"
	"github.com/ulule/limiter/v3/drivers/store/memory"
)

// LoginRateLimitMiddleware 登录接口限流中间件
// 15分钟内最多5次登录尝试
func LoginRateLimitMiddleware() gin.HandlerFunc {
	rate := limiter.Rate{
		Period: 15 * time.Minute,
		Limit:  5,
	}
	store := memory.NewStore()
	limiterInstance := limiter.New(store, rate)

	// 使用 gin 中间件包装器
	middleware := mgin.NewMiddleware(limiterInstance, mgin.WithKeyGetter(func(c *gin.Context) string {
		// 使用 IP 地址作为限流键
		return c.ClientIP()
	}))

	return func(c *gin.Context) {
		middleware(c)

		// 如果中间件已经设置了响应（即被限流），则不继续处理
		if c.Writer.Written() {
			return
		}

		c.Next()
	}
}

// GeneralRateLimitMiddleware 通用接口限流中间件
// 1分钟内最多100次请求
func GeneralRateLimitMiddleware() gin.HandlerFunc {
	rate := limiter.Rate{
		Period: 1 * time.Minute,
		Limit:  100,
	}
	store := memory.NewStore()
	limiterInstance := limiter.New(store, rate)

	middleware := mgin.NewMiddleware(limiterInstance, mgin.WithKeyGetter(func(c *gin.Context) string {
		return c.ClientIP()
	}))

	return func(c *gin.Context) {
		middleware(c)
		if c.Writer.Written() {
			return
		}
		c.Next()
	}
}

// CustomRateLimit 自定义限流中间件
func CustomRateLimit(period time.Duration, limit int64) gin.HandlerFunc {
	rate := limiter.Rate{
		Period: period,
		Limit:  limit,
	}
	store := memory.NewStore()
	limiterInstance := limiter.New(store, rate)

	middleware := mgin.NewMiddleware(limiterInstance, mgin.WithKeyGetter(func(c *gin.Context) string {
		return c.ClientIP()
	}), mgin.WithLimitReachedHandler(func(c *gin.Context) {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": "请求过于频繁，请稍后再试",
			"retry_after": rate.Period.Seconds(),
		})
		c.Abort()
	}))

	return middleware
}
