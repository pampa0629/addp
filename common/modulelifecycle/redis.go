package modulelifecycle

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCheck observes connectivity using the same client as actual operations.
// Successful Ping never substitutes for the operation's command/ACL checks.
func RedisCheck(client *redis.Client) CheckFunc {
	return func(ctx context.Context) CheckResult {
		result := CheckResult{Name: "redis", Status: CheckNotReady, ErrorCode: "redis_unavailable"}
		if client == nil {
			return result
		}
		check, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := client.Ping(check).Err(); err != nil {
			return result
		}
		return CheckResult{Name: "redis", Status: CheckReady}
	}
}
