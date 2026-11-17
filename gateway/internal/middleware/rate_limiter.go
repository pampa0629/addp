package middleware

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/gateway/pkg/client"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// RateLimiterMiddleware 限流中间件（基于 Redis 固定窗口算法）
type RateLimiterMiddleware struct {
	redisClient *redis.Client
}

// NewRateLimiterMiddleware 创建限流中间件
func NewRateLimiterMiddleware(redisClient *redis.Client) *RateLimiterMiddleware {
	return &RateLimiterMiddleware{
		redisClient: redisClient,
	}
}

// Handler 中间件处理函数
func (m *RateLimiterMiddleware) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		infoRaw, exists := c.Get("api_consumer_info")
		if !exists {
			// 没有 API 消费方上下文时，不执行消费方级限流。
			c.Next()
			return
		}

		info, ok := infoRaw.(*client.APIConsumerCredentialValidationResponse)
		if !ok || info == nil {
			c.Next()
			return
		}

		// 执行限流检查
		allowed, err := m.checkRateLimit(c.Request.Context(), info.APIConsumerID, info.RateLimitPerMinute)
		if err != nil {
			log.Print("rate_limit_unavailable")
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": commoni18n.T(c, "rate_limit.unavailable"), "error_code": "rate_limit_unavailable",
			})
			return
		}

		if !allowed {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":  "Rate limit exceeded",
				"limit":  info.RateLimitPerMinute,
				"window": "1 minute",
			})
			return
		}

		c.Next()
	}
}

// checkRateLimit 检查是否超过速率限制（Redis 固定窗口算法）
// apiConsumerID: API 消费方 ID
// limit: 每分钟请求数限制
// 返回: true=允许通过, false=超过限制
func (m *RateLimiterMiddleware) checkRateLimit(ctx context.Context, apiConsumerID uint, limit int) (bool, error) {
	if m.redisClient == nil {
		return false, errors.New("Redis client unavailable")
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	key := fmt.Sprintf("ratelimit:api-consumer:%d", apiConsumerID)

	// 使用 Lua 脚本实现原子操作
	script := `
		local key = KEYS[1]
		local limit = tonumber(ARGV[1])
		local window = 60  -- 1 分钟窗口（秒）

		local current = redis.call('INCR', key)
		if current == 1 then
			redis.call('EXPIRE', key, window)
		end

		if current <= limit then
			return 1  -- 允许通过
		else
			return 0  -- 超过限制
		end
	`

	result, err := m.redisClient.Eval(ctx, script, []string{key}, limit).Int()
	if err != nil {
		return false, fmt.Errorf("redis eval failed: %w", err)
	}

	return result == 1, nil
}

// GetCurrentCount 获取当前计数（用于监控）
func (m *RateLimiterMiddleware) GetCurrentCount(apiConsumerID uint) (int, error) {
	if m.redisClient == nil {
		return 0, fmt.Errorf("redis client is nil")
	}

	ctx := context.Background()
	key := fmt.Sprintf("ratelimit:api-consumer:%d", apiConsumerID)

	val, err := m.redisClient.Get(ctx, key).Int()
	if err == redis.Nil {
		return 0, nil // 键不存在，计数为 0
	}
	if err != nil {
		return 0, err
	}

	return val, nil
}

// ResetCount 重置计数（用于管理）
func (m *RateLimiterMiddleware) ResetCount(apiConsumerID uint) error {
	if m.redisClient == nil {
		return fmt.Errorf("redis client is nil")
	}

	ctx := context.Background()
	key := fmt.Sprintf("ratelimit:api-consumer:%d", apiConsumerID)

	return m.redisClient.Del(ctx, key).Err()
}

// GetTTL 获取当前窗口剩余时间
func (m *RateLimiterMiddleware) GetTTL(apiConsumerID uint) (time.Duration, error) {
	if m.redisClient == nil {
		return 0, fmt.Errorf("redis client is nil")
	}

	ctx := context.Background()
	key := fmt.Sprintf("ratelimit:api-consumer:%d", apiConsumerID)

	return m.redisClient.TTL(ctx, key).Result()
}
