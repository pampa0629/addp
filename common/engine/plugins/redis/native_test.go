package redis

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
	rdb "github.com/redis/go-redis/v9"
)

func TestRedisCancelsInFlightRead(t *testing.T) {
	m := miniredis.RunT(t)
	m.RequireUserAuth("reader", "secret")
	m.Set("blocked", "value")
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	m.Server().SetPreHook(func(peer *server.Peer, command string, args ...string) bool {
		if command != "GETRANGE" {
			return false
		}
		close(entered)
		<-release
		peer.WriteBulk("value")
		return true
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &RedisPlugin{}
	path := keyTestPath(t, p, "blocked")
	c := redisTestConnection(t, m.Addr())
	done := make(chan error, 1)
	go func() { _, err := p.ReadKeyValue(ctx, c, path, plugin.KeyValueReadOptions{}); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("read did not reach native command")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation accepted as successful read")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt native socket read")
	}
}

func TestRedisRESPBudgetBeforeDecode(t *testing.T) {
	for _, frame := range []string{"$1073741824\r\n", "*1073741824\r\n", "$-2\r\n", "*" + strings.Repeat("9", 100) + "\r\n", strings.Repeat("*1\r\n", 34) + "+ok\r\n", "$3\r\naaaXX", "+" + strings.Repeat("x", 4096) + "\r\n"} {
		var out bytes.Buffer
		if err := readBudgetFrame(bufio.NewReader(strings.NewReader(frame)), &out, 0); err == nil {
			t.Fatalf("accepted unbounded/malformed frame %q", frame[:min(len(frame), 60)])
		}
		if out.Len() > maxReplyBytes {
			t.Fatal("budget exceeded before decode")
		}
	}
	frame := "*3\r\n$0\r\n\r\n$3\r\n\x00\xffa\r\n*2\r\n:1\r\n-ERR denied\r\n"
	var out bytes.Buffer
	if err := readBudgetFrame(bufio.NewReader(strings.NewReader(frame)), &out, 0); err != nil || out.String() != frame {
		t.Fatalf("valid frame: %v", err)
	}
}

func keyTestPath(t *testing.T, p *RedisPlugin, key string) plugin.EngineCatalogPath {
	t.Helper()
	name, err := plugin.EncodeKeyName([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	return p.entry(42, name).Path
}

func TestRedisNativeReadAndCatalog(t *testing.T) {
	m := miniredis.RunT(t)
	m.RequireUserAuth("reader", "secret")
	c := redisTestConnection(t, m.Addr())
	p := &RedisPlugin{}
	ctx := context.Background()
	admin := rdb.NewClient(&rdb.Options{Addr: m.Addr(), Username: "reader", Password: "secret"})
	defer admin.Close()
	commands := [][]interface{}{{"SET", "", ""}, {"SET", "app:a/b.c", "9223372036854775807"}, {"SET", "binary", "\x00\xff"}, {"HSET", "hash", "name", "张三", "binary", "\xff"}, {"RPUSH", "list", "1", "false", "{\"a\":1}"}, {"SADD", "set", "a", "b"}, {"ZADD", "zset", "1.25", "member"}, {"XADD", "stream", "1-0", "name", "first", "name", "second"}, {"SET", "long", strings.Repeat("x", 70<<10)}}
	for _, args := range commands {
		if err := admin.Do(ctx, args...).Err(); err != nil {
			t.Fatal(err)
		}
	}
	root := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), 42)
	entries, err := p.ListChildren(ctx, c, root, plugin.ListOptions{})
	if err != nil || len(entries) != 9 {
		t.Fatalf("catalog %d: %v", len(entries), err)
	}
	for _, entry := range entries {
		if len(entry.Path.Segments) != 2 || entry.Kind != "key" {
			t.Fatal(entry)
		}
		if _, err := p.ResolvePath(ctx, c, entry.Path); err != nil {
			t.Fatal(err)
		}
	}
	read := func(key string, opts plugin.KeyValueReadOptions) *plugin.KeyValuePreview {
		t.Helper()
		v, err := p.ReadKeyValue(ctx, c, keyTestPath(t, p, key), opts)
		if err != nil {
			t.Fatal(key, err)
		}
		return v
	}
	if v := read("app:a/b.c", plugin.KeyValueReadOptions{}); v.Value.Value != "9223372036854775807" || v.Facts.TTLMillis != -1 {
		t.Fatal(v)
	}
	if v := read("", plugin.KeyValueReadOptions{}); v.Value == nil || v.Value.Value != "" || v.Facts.Length != 0 {
		t.Fatal(v)
	}
	if v := read("binary", plugin.KeyValueReadOptions{}); v.Value.Encoding != "base64" || v.Value.Value != "AP8=" {
		t.Fatal(v)
	}
	for _, key := range []string{"hash", "list", "set", "zset", "stream"} {
		if v := read(key, plugin.KeyValueReadOptions{}); len(v.Entries) == 0 || v.Facts.NativeType != key {
			t.Fatal(v)
		}
	}
	if v := read("stream", plugin.KeyValueReadOptions{}); len(v.Entries[0].Fields) != 2 || v.Entries[0].Fields[1].Value.Value != "second" {
		t.Fatal(v)
	}
	if v := read("zset", plugin.KeyValueReadOptions{}); v.Entries[0].Score != "1.25" {
		t.Fatal(v)
	}
	if v := read("list", plugin.KeyValueReadOptions{MaxEntries: 1}); !v.Truncated || len(v.Entries) != 1 {
		t.Fatal(v)
	}
	if v := read("long", plugin.KeyValueReadOptions{}); !v.Truncated || v.Value.ByteLength != 64<<10 {
		t.Fatal(v)
	}
	if _, err := p.ReadKeyValue(ctx, c, keyTestPath(t, p, "hash"), plugin.KeyValueReadOptions{MaxBytes: 1}); err == nil {
		t.Fatal("byte budget ignored")
	}
	if _, err := p.ReadKeyValue(ctx, c, keyTestPath(t, p, "missing"), plugin.KeyValueReadOptions{}); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if err := admin.Set(ctx, strings.Repeat("x", 190), "too long", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if entries, err := p.ListChildren(ctx, c, root, plugin.ListOptions{Limit: 1}); err == nil || entries != nil {
		t.Fatal("partial/overlong catalog returned")
	}
}
