package redis

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	rdb "github.com/redis/go-redis/v9"
)

func rootPath(path plugin.EngineCatalogPath) bool {
	return path.EngineID > 0 && path.Version == plugin.EngineCatalogPathVersion && plugin.IsEngineCatalogRootPath(path) && path.Segments[0].Term == plugin.EngineCatalogTermServer && path.Segments[0].Kind == plugin.EngineCatalogTermServer && path.Segments[0].Name == ""
}

func keyName(path plugin.EngineCatalogPath) (string, error) {
	if len(path.Segments) != 2 || !rootPath(plugin.EngineCatalogPath{EngineID: path.EngineID, Version: path.Version, Segments: path.Segments[:1]}) || path.Segments[1].Term != plugin.EngineCatalogTermKey || path.Segments[1].Kind != plugin.EngineCatalogTermKey {
		return "", plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("exact key path required"))
	}
	raw, err := plugin.DecodeKeyName(path.Segments[1].Name)
	if err != nil {
		return "", plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, err)
	}
	return string(raw), nil
}

func (p *RedisPlugin) entry(engineID uint, name string) plugin.EngineCatalogEntry {
	path := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), engineID)
	path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: plugin.EngineCatalogTermKey, Kind: plugin.EngineCatalogTermKey, Name: name})
	return plugin.EngineCatalogEntry{Name: name, Path: path, Term: plugin.EngineCatalogTermKey, Kind: plugin.EngineCatalogTermKey, Role: plugin.EngineCatalogRoleLeaf}
}

func operationError(err error) error {
	if err == nil {
		return nil
	}
	if err == rdb.Nil {
		return plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorNotFound, fmt.Errorf("Redis key no longer exists"))
	}
	if strings.Contains(err.Error(), "NOPERM") {
		return plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorUnavailable, fmt.Errorf("Redis operation denied by ACL"))
	}
	if strings.Contains(err.Error(), "WRONGTYPE") {
		return plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorUnavailable, fmt.Errorf("Redis key type changed during read"))
	}
	return plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorUnavailable, fmt.Errorf("Redis operation failed: %w", err))
}

func (p *RedisPlugin) ListChildren(ctx context.Context, c plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	if !rootPath(parent) {
		if _, err := p.ResolvePath(ctx, c, parent); err != nil {
			return nil, err
		}
		return []plugin.EngineCatalogEntry{}, nil
	}
	if opts.Offset < 0 || opts.Limit < 0 || opts.Recursive {
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("invalid key listing options"))
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client, conn, err := p.open(ctx, c)
	if err != nil {
		return nil, operationError(err)
	}
	defer client.Close()
	defer conn.Close()
	seen := map[string]bool{}
	entries := []plugin.EngineCatalogEntry{}
	var cursor uint64
	bytesRead := 0
	for steps := 0; ; steps++ {
		if steps >= 1000 {
			return nil, operationError(fmt.Errorf("Redis catalog iteration exceeds budget"))
		}
		keys, next, err := conn.Scan(ctx, cursor, "", 100).Result()
		if err != nil {
			return nil, operationError(err)
		}
		for _, raw := range keys {
			bytesRead += len(raw)
			if bytesRead > 8<<20 {
				return nil, operationError(fmt.Errorf("Redis catalog key bytes exceed budget"))
			}
			if seen[raw] {
				continue
			}
			seen[raw] = true
			if len(seen) > 10000 {
				return nil, operationError(fmt.Errorf("Redis catalog key count exceeds budget"))
			}
			nativeType, err := conn.Type(ctx, raw).Result()
			if err != nil {
				// Redis distinguishes key-pattern denial from denial of the TYPE command.
				if err.Error() == "NOPERM No permissions to access a key" {
					continue
				}
				return nil, operationError(err)
			}
			if nativeType == "none" {
				continue
			}
			name, err := plugin.EncodeKeyName([]byte(raw))
			if err != nil {
				return nil, operationError(err)
			}
			entries = append(entries, p.entry(parent.EngineID, name))
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	start, end := min(opts.Offset, len(entries)), len(entries)
	if opts.Limit > 0 {
		end = start + min(opts.Limit, end-start)
	}
	return entries[start:end], nil
}

func (p *RedisPlugin) ResolvePath(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	if rootPath(path) {
		entry := plugin.EngineCatalogRootEntry(p.EngineCatalogModel(), path.EngineID, "")
		return &entry, nil
	}
	if _, err := p.DescribeEngineCatalogFacts(ctx, c, path, plugin.EngineCatalogFactsOptions{}); err != nil {
		return nil, err
	}
	entry := p.entry(path.EngineID, path.Segments[1].Name)
	return &entry, nil
}

func describe(ctx context.Context, conn *rdb.Conn, key string) (*plugin.KeyValueFacts, error) {
	nativeType, err := conn.Type(ctx, key).Result()
	if err != nil {
		return nil, operationError(err)
	}
	if nativeType == "none" {
		return nil, operationError(rdb.Nil)
	}
	facts := &plugin.KeyValueFacts{NativeType: nativeType, Length: -1}
	var command string
	switch nativeType {
	case "string":
		command = "STRLEN"
	case "hash":
		command = "HLEN"
	case "list":
		command = "LLEN"
	case "set":
		command = "SCARD"
	case "zset":
		command = "ZCARD"
	case "stream":
		command = "XLEN"
	}
	if command != "" {
		facts.Length, err = do(ctx, conn, command, key).Int64()
		if err != nil {
			return nil, operationError(err)
		}
	}
	// Use the integer reply directly; -1 and -2 are semantic values, not durations.
	facts.TTLMillis, err = do(ctx, conn, "PTTL", key).Int64()
	if err != nil {
		return nil, operationError(err)
	}
	if facts.TTLMillis == -2 {
		return nil, operationError(rdb.Nil)
	}
	return facts, nil
}

func (p *RedisPlugin) DescribeEngineCatalogFacts(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath, _ plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	key, err := keyName(path)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client, conn, err := p.open(ctx, c)
	if err != nil {
		return nil, operationError(err)
	}
	defer client.Close()
	defer conn.Close()
	facts, err := describe(ctx, conn, key)
	if err != nil {
		return nil, err
	}
	return &plugin.EngineCatalogFacts{Path: path, Kind: plugin.EngineCatalogTermKey, KeyValue: facts}, nil
}
