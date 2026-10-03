package redis

import (
	"context"
	"os"
	"strings"
	"testing"

	rdb "github.com/redis/go-redis/v9"
)

func TestIntegrationRedisConnection(t *testing.T) {
	if os.Getenv("ADDP_REDIS_INTEGRATION") != "1" {
		t.Skip("run make test-common-redis")
	}
	c := redisTestConnection(t, os.Getenv("ADDP_REDIS_T2_ENDPOINT"))
	c["user"] = "addp_business_reader"
	c["password"] = os.Getenv("BUSINESS_REDIS_READER_PASSWORD")
	p := &RedisPlugin{}
	if err := p.TestConnection(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	c["password"] = "incorrect"
	if err := p.TestConnection(context.Background(), c); err == nil {
		t.Fatal("accepted wrong credential")
	}
	c["password"] = os.Getenv("BUSINESS_REDIS_READER_PASSWORD")
	c["database"] = 1
	if err := p.TestConnection(context.Background(), c); err == nil {
		t.Fatal("accepted unavailable database")
	}
	c["database"] = 0
	admin := rdb.NewClient(&rdb.Options{Addr: os.Getenv("ADDP_REDIS_T2_ENDPOINT"), Username: "addp_business_admin", Password: os.Getenv("BUSINESS_REDIS_ADMIN_PASSWORD")})
	defer admin.Close()
	ctx := context.Background()
	for _, command := range []string{"dbsize", "select"} {
		if err := admin.Do(ctx, "ACL", "SETUSER", "addp_probe_limited", "reset", "on", ">limited-secret", "+hello", "+ping", "+select", "+dbsize", "-"+command).Err(); err != nil {
			t.Fatal(err)
		}
		c["user"] = "addp_probe_limited"
		c["password"] = "limited-secret"
		if err := p.TestConnection(ctx, c); err == nil || !strings.Contains(err.Error(), "NOPERM") {
			t.Fatalf("probe accepted user without %s: %v", command, err)
		}
	}
	if err := admin.Do(ctx, "ACL", "DELUSER", "addp_probe_limited").Err(); err != nil {
		t.Fatal(err)
	}
}
