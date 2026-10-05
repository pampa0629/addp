package redis

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
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
	c := redisTestConnection(t, m.Addr())
	done := make(chan error, 1)
	go func() { _, err := readTestKey(t, p, ctx, c, "blocked", plugin.KeyValueReadOptions{}); done <- err }()
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

func readTestKey(t *testing.T, p *RedisPlugin, ctx context.Context, c plugin.ConnectionInfo, key string, opts plugin.KeyValueReadOptions) (*plugin.KeyValuePreview, error) {
	t.Helper()
	name, err := plugin.EncodeKeyName([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	opts.Key = name
	return p.ReadKeyValue(ctx, c, p.entry(42).Path, opts)
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
	if err != nil || len(entries) != 1 {
		t.Fatalf("catalog %d: %v", len(entries), err)
	}
	for _, entry := range entries {
		if len(entry.Path.Segments) != 2 || entry.Kind != "keyspace" {
			t.Fatal(entry)
		}
		if _, err := p.ResolvePath(ctx, c, entry.Path); err != nil {
			t.Fatal(err)
		}
	}
	read := func(key string, opts plugin.KeyValueReadOptions) *plugin.KeyValuePreview {
		t.Helper()
		v, err := readTestKey(t, p, ctx, c, key, opts)
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
	if _, err := readTestKey(t, p, ctx, c, "hash", plugin.KeyValueReadOptions{MaxBytes: 1}); err == nil {
		t.Fatal("byte budget ignored")
	}
	if _, err := readTestKey(t, p, ctx, c, "missing", plugin.KeyValueReadOptions{}); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if err := admin.Set(ctx, strings.Repeat("x", 190), "too long", 0).Err(); err != nil {
		t.Fatal(err)
	}
	batch, err := p.ListKeyValues(ctx, c, p.entry(42).Path, plugin.KeyValueReadOptions{})
	if err != nil || len(batch.Keys) != 10 || !batch.Complete {
		t.Fatal(batch, err)
	}
	if _, err := p.ReadKeyValue(ctx, c, root, plugin.KeyValueReadOptions{Key: "k:YQ"}); err == nil {
		t.Fatal("root accepted as dataset")
	}
	if _, err := p.ListKeyValues(ctx, c, p.entry(42).Path, plugin.KeyValueReadOptions{Cursor: "-1"}); err == nil {
		t.Fatal("invalid cursor")
	}
	if _, err := p.ListKeyValues(ctx, c, p.entry(42).Path, plugin.KeyValueReadOptions{MaxBytes: 1}); err == nil {
		t.Fatal("browse budget ignored")
	}
}

func TestRedisEmptyKeyspaceAndCursorBatches(t *testing.T) {
	m := miniredis.RunT(t)
	m.RequireUserAuth("reader", "secret")
	p := &RedisPlugin{}
	c := redisTestConnection(t, m.Addr())
	ctx := context.Background()
	root := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), 42)
	entries, err := p.ListChildren(ctx, c, root, plugin.ListOptions{})
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	path := entries[0].Path
	empty, err := p.ListKeyValues(ctx, c, path, plugin.KeyValueReadOptions{})
	if err != nil || !empty.Complete || len(empty.Keys) != 0 {
		t.Fatal(empty, err)
	}
	for i := 0; i < 75; i++ {
		m.Set(fmt.Sprintf("sample:%d", i), "value")
	}
	seen := map[string]bool{}
	cursor := ""
	for step := 0; ; step++ {
		if step > 100 {
			t.Fatal("cursor never completed")
		}
		batch, err := p.ListKeyValues(ctx, c, path, plugin.KeyValueReadOptions{Cursor: cursor, MaxEntries: 3})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range batch.Keys {
			seen[key.Key] = true
		}
		if batch.Complete {
			break
		}
		cursor = batch.NextCursor
	}
	if len(seen) != 75 {
		t.Fatal("cursor lost keys", len(seen))
	}
	// A server may return more than COUNT. Verify no key is silently dropped.
	m.Server().SetPreHook(func(peer *server.Peer, command string, args ...string) bool {
		if command != "SCAN" {
			return false
		}
		peer.WriteLen(2)
		peer.WriteBulk("0")
		peer.WriteStrings([]string{"sample:0", "sample:1", "sample:2"})
		return true
	})
	batch, err := p.ListKeyValues(ctx, c, path, plugin.KeyValueReadOptions{MaxEntries: 1})
	if err != nil || len(batch.Keys) != 3 {
		t.Fatal("COUNT hint dropped keys", batch, err)
	}
}
