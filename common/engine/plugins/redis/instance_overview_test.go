package redis

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
)

func TestRedisInstanceOverviewPreservesZeroAndOmitsMissingOrMalformedINFOValues(t *testing.T) {
	m := miniredis.RunT(t)
	m.RequireUserAuth("reader", "secret")
	m.Server().SetPreHook(func(peer *server.Peer, command string, args ...string) bool {
		if command != "INFO" {
			return false
		}
		peer.WriteBulk("# Memory\r\nused_memory:9007199254740993\r\nconnected_clients:bad\r\nprivate_field:secret-sentinel\r\n")
		return true
	})
	result, err := (&RedisPlugin{}).ReadInstanceOverview(t.Context(), redisTestConnection(t, m.Addr()))
	if err != nil {
		t.Fatal(err)
	}
	if result.Metrics[0].Value == nil || *result.Metrics[0].Value != 0 || result.Metrics[0].Status != "available" {
		t.Fatal("zero became missing")
	}
	if result.Metrics[1].Value == nil || *result.Metrics[1].Value != 9007199254740993 {
		t.Fatal("integer precision lost")
	}
	for _, m := range result.Metrics[2:] {
		if m.Status != "unavailable" || m.Value != nil {
			t.Fatal("missing or malformed field became zero", m)
		}
	}
}
