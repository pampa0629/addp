package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"time"

	"context"
	"encoding/json"
	"github.com/addp/gateway/internal/cache"
	"github.com/addp/gateway/pkg/client"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// APIKeyAuthMiddleware API Key 验证中间件
type APIKeyAuthMiddleware struct {
	systemClient *client.SystemClient
	localCache   *cache.LocalCache
	redisClient  *redis.Client
	redisTTL     time.Duration
}

// NewAPIKeyAuthMiddleware 创建 API Key 验证中间件
func NewAPIKeyAuthMiddleware(
	systemClient *client.SystemClient,
	localCache *cache.LocalCache,
	redisClient *redis.Client,
) *APIKeyAuthMiddleware {
	return &APIKeyAuthMiddleware{
		systemClient: systemClient,
		localCache:   localCache,
		redisClient:  redisClient,
		redisTTL:     1 * time.Hour, // Redis 缓存 1 小时
	}
}

// Handler 中间件处理函数
func (m *APIKeyAuthMiddleware) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 检查请求头中的 API Key
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			// 没有 API Key 时透明传递，由目标模块处理 Bearer Token 或公开访问。
			c.Next()
			return
		}

		// 计算 API Key 的 SHA256 hash
		keyHash := m.hashAPIKey(apiKey)

		// 三层缓存验证
		info, err := m.validateWithCache(keyHash)
		if err != nil {
			log.Printf("API Key validation failed: %v", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid API Key",
			})
			return
		}

		if !info.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "API Key is not valid or has been revoked",
			})
			return
		}

		// 将 API Key 信息存入上下文
		c.Set("api_key_info", info)
		c.Set("app_id", info.AppID)
		c.Set("app_name", info.AppName)

		c.Next()
	}
}

// validateWithCache 三层缓存验证
// 1. 本地缓存（< 1ms）
// 2. Redis 缓存（< 5ms）
// 3. System API（< 20ms）
func (m *APIKeyAuthMiddleware) validateWithCache(keyHash string) (*client.APIKeyValidationResponse, error) {
	ctx := context.Background()

	// 1. 查询本地缓存
	if info := m.localCache.Get(keyHash); info != nil {
		return info, nil
	}

	// 2. 查询 Redis 缓存
	if m.redisClient != nil {
		redisKey := fmt.Sprintf("gateway:apikey:%s", keyHash)
		val, err := m.redisClient.Get(ctx, redisKey).Result()
		if err == nil {
			var info client.APIKeyValidationResponse
			if err := json.Unmarshal([]byte(val), &info); err == nil {
				// 写入本地缓存
				m.localCache.Set(keyHash, &info)
				return &info, nil
			}
		}
	}

	// 3. 查询 System API
	info, err := m.systemClient.ValidateAPIKey(keyHash)
	if err != nil {
		return nil, fmt.Errorf("system API validation failed: %w", err)
	}

	// 写入缓存（反向传播）
	if info.Valid {
		// 写入 Redis
		if m.redisClient != nil {
			redisKey := fmt.Sprintf("gateway:apikey:%s", keyHash)
			data, _ := json.Marshal(info)
			m.redisClient.Set(ctx, redisKey, data, m.redisTTL)
		}

		// 写入本地缓存
		m.localCache.Set(keyHash, info)
	}

	return info, nil
}

// hashAPIKey 计算 API Key 的 SHA256 hash
func (m *APIKeyAuthMiddleware) hashAPIKey(apiKey string) string {
	hash := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(hash[:])
}

// InvalidateCache 主动清理缓存（用于 Key 撤销时）
func (m *APIKeyAuthMiddleware) InvalidateCache(keyHash string) {
	ctx := context.Background()

	// 清理本地缓存
	m.localCache.Delete(keyHash)

	// 清理 Redis 缓存
	if m.redisClient != nil {
		redisKey := fmt.Sprintf("gateway:apikey:%s", keyHash)
		m.redisClient.Del(ctx, redisKey)
	}
}
