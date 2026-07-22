package ratelimit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	redisv9 "github.com/redis/go-redis/v9"
)

var fixedWindowScript = redisv9.NewScript(`
local count = redis.call("INCR", KEYS[1])
if count == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
local ttl = redis.call("PTTL", KEYS[1])
return {count, ttl}
`)

type RedisFixedWindowOptions struct {
	Prefix         string
	Period         time.Duration
	Limit          int64
	KeyGetter      func(*gin.Context) string
	OnLimitReached func(*gin.Context, int64)
	OnUnavailable  func(*gin.Context, error)
}

// RedisFixedWindowMiddleware provides a shared, multi-instance fixed-window limiter.
// Callers define the response contract through the two handlers.
func RedisFixedWindowMiddleware(client redisv9.Scripter, options RedisFixedWindowOptions) (gin.HandlerFunc, error) {
	if strings.TrimSpace(options.Prefix) == "" || options.Period <= 0 || options.Limit <= 0 || options.KeyGetter == nil {
		return nil, errors.New("invalid redis rate limit options")
	}
	return func(c *gin.Context) {
		if client == nil {
			handleRateLimitUnavailable(c, options, errors.New("redis rate limit store is unavailable"))
			return
		}
		rawKey := options.KeyGetter(c)
		digest := sha256.Sum256([]byte(rawKey))
		key := options.Prefix + ":" + hex.EncodeToString(digest[:])
		values, err := fixedWindowScript.Run(c.Request.Context(), client, []string{key}, options.Period.Milliseconds()).Int64Slice()
		if err != nil || len(values) != 2 {
			if err == nil {
				err = errors.New("invalid redis rate limit response")
			}
			handleRateLimitUnavailable(c, options, err)
			return
		}

		count, ttlMillis := values[0], values[1]
		if ttlMillis < 0 {
			ttlMillis = options.Period.Milliseconds()
		}
		remaining := options.Limit - count
		if remaining < 0 {
			remaining = 0
		}
		resetAt := time.Now().Add(time.Duration(ttlMillis) * time.Millisecond).Unix()
		c.Header("X-RateLimit-Limit", strconv.FormatInt(options.Limit, 10))
		c.Header("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))
		if count > options.Limit {
			retryAfter := (ttlMillis + 999) / 1000
			if retryAfter < 1 {
				retryAfter = 1
			}
			c.Header("Retry-After", strconv.FormatInt(retryAfter, 10))
			if options.OnLimitReached != nil {
				options.OnLimitReached(c, retryAfter)
			} else {
				c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate_limit_exceeded"})
				c.Abort()
			}
			return
		}
		c.Next()
	}, nil
}

func handleRateLimitUnavailable(c *gin.Context, options RedisFixedWindowOptions, err error) {
	if options.OnUnavailable != nil {
		options.OnUnavailable(c, err)
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": "rate_limit_unavailable"})
	c.Abort()
}
