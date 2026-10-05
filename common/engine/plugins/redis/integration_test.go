package redis

import (
	"context"
	"github.com/addp/common/engine/plugin"
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

func TestIntegrationRedisNativeAccessAndACL(t *testing.T) {
	if os.Getenv("ADDP_REDIS_INTEGRATION") != "1" {
		t.Skip("run make test-common-redis")
	}
	ctx := context.Background()
	p := &RedisPlugin{}
	c := redisTestConnection(t, os.Getenv("ADDP_REDIS_T2_ENDPOINT"))
	c["user"] = "addp_business_reader"
	c["password"] = os.Getenv("BUSINESS_REDIS_READER_PASSWORD")
	admin := rdb.NewClient(&rdb.Options{Addr: os.Getenv("ADDP_REDIS_T2_ENDPOINT"), Username: "addp_business_admin", Password: os.Getenv("BUSINESS_REDIS_ADMIN_PASSWORD")})
	defer admin.Close()
	root := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), 42)
	entries, err := p.ListChildren(ctx, c, root, plugin.ListOptions{})
	if err != nil || len(entries) != 1 {
		t.Fatalf("native catalog %d: %v", len(entries), err)
	}
	batch, err := p.ListKeyValues(ctx, c, entries[0].Path, plugin.KeyValueReadOptions{MaxEntries: 100})
	if err != nil || len(batch.Keys) != 9 || !batch.Complete {
		t.Fatal(batch, err)
	}
	for _, key := range batch.Keys {
		value, err := p.ReadKeyValue(ctx, c, entries[0].Path, plugin.KeyValueReadOptions{Key: key.Key})
		if err != nil || value.Facts.NativeType != key.Facts.NativeType {
			t.Fatal(value, err)
		}
	}
	if err := admin.Do(ctx, "ACL", "SETUSER", "addp_native_limited", "reset", "on", ">native-limited", "~addp:sample:counter", "+hello", "+select", "+scan", "+type", "+strlen", "+pttl", "+getrange").Err(); err != nil {
		t.Fatal(err)
	}
	defer admin.Do(ctx, "ACL", "DELUSER", "addp_native_limited")
	limited := plugin.ConnectionInfo{}
	for k, v := range c {
		limited[k] = v
	}
	limited["user"] = "addp_native_limited"
	limited["password"] = "native-limited"
	visible, err := p.ListKeyValues(ctx, limited, entries[0].Path, plugin.KeyValueReadOptions{MaxEntries: 100})
	if err != nil || visible == nil || len(visible.Keys) != 1 {
		t.Fatalf("key ACL contents %v: %v", visible, err)
	}
	if raw, err := plugin.DecodeKeyName(visible.Keys[0].Key); err != nil || string(raw) != "addp:sample:counter" {
		t.Fatal(visible, err)
	}
	if v, err := readTestKey(t, p, ctx, limited, "addp:sample:hash", plugin.KeyValueReadOptions{}); err == nil || v != nil {
		t.Fatal("key permission denial returned data")
	}
	if err := admin.Do(ctx, "ACL", "SETUSER", "addp_native_limited", "-type").Err(); err != nil {
		t.Fatal(err)
	}
	if items, err := p.ListKeyValues(ctx, limited, entries[0].Path, plugin.KeyValueReadOptions{}); err == nil || items != nil {
		t.Fatal("command ACL denial treated as empty catalog")
	}
	for _, test := range []struct {
		key  string
		args []interface{}
	}{
		{"addp:sample:budget", []interface{}{"RPUSH", "addp:sample:budget", strings.Repeat("x", 2<<20)}},
		{"addp:sample:duplicates", []interface{}{"XADD", "addp:sample:duplicates", "1-0", "f", "first", "f", "second"}},
	} {
		if err := admin.Do(ctx, test.args...).Err(); err != nil {
			t.Fatal(err)
		}
		defer admin.Del(ctx, test.key)
	}
	if v, err := readTestKey(t, p, ctx, c, "addp:sample:budget", plugin.KeyValueReadOptions{MaxEntries: 1}); err == nil || v != nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("large native allocation accepted: %v", err)
	}
	v, err := readTestKey(t, p, ctx, c, "addp:sample:duplicates", plugin.KeyValueReadOptions{})
	if err != nil || len(v.Entries) != 1 || len(v.Entries[0].Fields) != 2 || v.Entries[0].Fields[0].Name.Value != "f" || v.Entries[0].Fields[1].Value.Value != "second" {
		t.Fatalf("stream field pairs lost: %v", err)
	}
}
