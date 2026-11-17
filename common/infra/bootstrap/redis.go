package bootstrap

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/config"
	"github.com/redis/go-redis/v9"
)

// OpenRequiredRedis binds one deployment endpoint for the lifetime of a process.
// The caller owns Close. No client is returned when authentication or Ping fails.
func OpenRequiredRedis(ctx context.Context, cfg config.RedisConfig) (*redis.Client, error) {
	port, err := strconv.Atoi(cfg.Port)
	if strings.TrimSpace(cfg.Host) == "" || cfg.Password == "" || err != nil || port < 1 || port > 65535 || cfg.DB < 0 {
		return nil, errors.New("invalid required Redis configuration")
	}
	client := redis.NewClient(&redis.Options{
		Addr: net.JoinHostPort(cfg.Host, cfg.Port), Password: cfg.Password, DB: cfg.DB,
		DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
		ContextTimeoutEnabled: true, MaxRetries: -1,
	})
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(check).Err(); err != nil {
		_ = client.Close()
		return nil, errors.New("required Redis unavailable")
	}
	return client, nil
}
