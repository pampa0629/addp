package modulelifecycle

import (
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"testing"
)

func TestRedisReadinessUsesInjectedClientAndRecovers(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	defer client.Close()
	check := RedisCheck(client)
	if got := check(t.Context()); got.Status != CheckReady {
		t.Fatal(got)
	}
	server.RequireAuth("changed")
	got := check(t.Context())
	if got.Status != CheckNotReady || got.Name != "redis" || got.ErrorCode != "redis_unavailable" {
		t.Fatal(got)
	}
	server.RequireAuth("")
	if got := check(t.Context()); got.Status != CheckReady {
		t.Fatal("same client did not recover", got)
	}
	if got := RedisCheck(nil)(t.Context()); got.Status != CheckNotReady {
		t.Fatal(got)
	}
}
