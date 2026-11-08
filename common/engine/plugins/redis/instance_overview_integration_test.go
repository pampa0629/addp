package redis

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	rdb "github.com/redis/go-redis/v9"
)

func TestIntegrationRedisInstanceOverview(t *testing.T) {
	if os.Getenv("ADDP_REDIS_INTEGRATION") != "1" {
		t.Skip("run make test-common-redis")
	}
	admin := rdb.NewClient(&rdb.Options{Addr: os.Getenv("ADDP_REDIS_T2_ENDPOINT"), Username: "addp_business_admin", Password: os.Getenv("BUSINESS_REDIS_ADMIN_PASSWORD")})
	t.Cleanup(func() { _ = admin.Close() })
	const user = "addp_overview_fixture"
	if err := admin.Do(t.Context(), "ACL", "SETUSER", user, "reset", "on", ">overview-fixture-secret", "~*", "+hello", "+select", "+dbsize").Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := admin.Do(ctx, "ACL", "DELUSER", user).Err(); err != nil {
			t.Error(err)
		}
	})
	info := redisTestConnection(t, os.Getenv("ADDP_REDIS_T2_ENDPOINT"))
	info["user"] = user
	info["password"] = "overview-fixture-secret"
	p := &RedisPlugin{}
	result, err := p.ReadInstanceOverview(t.Context(), info)
	if err != nil || result == nil || len(result.Metrics) != 4 {
		t.Fatalf("native overview failed: %v", err)
	}
	for i, metric := range result.Metrics {
		if i == 0 {
			if metric.Scope != "database" || metric.ScopeName != "0" || metric.Status != "available" || metric.Value == nil {
				t.Fatal(metric)
			}
		} else if metric.Scope != "server" || metric.Status != "permission_denied" || metric.Value != nil {
			t.Fatal(metric)
		}
	}
	if err := admin.Do(t.Context(), "ACL", "SETUSER", user, "+info", "-dbsize").Err(); err != nil {
		t.Fatal(err)
	}
	result, err = p.ReadInstanceOverview(t.Context(), plugin.ConnectionInfo(info))
	if err != nil {
		t.Fatal(err)
	}
	if result.Metrics[0].Status != "permission_denied" || result.Metrics[0].Value != nil {
		t.Fatal("DBSIZE permission was ignored")
	}
	for _, metric := range result.Metrics[1:] {
		if metric.Status != "available" || metric.Value == nil {
			t.Fatal(metric)
		}
	}
}
