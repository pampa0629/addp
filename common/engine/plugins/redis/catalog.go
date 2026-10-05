package redis

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	rdb "github.com/redis/go-redis/v9"
)

func rootPath(path plugin.EngineCatalogPath) bool {
	return path.EngineID > 0 && path.Version == plugin.EngineCatalogPathVersion && plugin.IsEngineCatalogRootPath(path) && path.Segments[0].Term == plugin.EngineCatalogTermServer && path.Segments[0].Kind == plugin.EngineCatalogTermServer && path.Segments[0].Name == ""
}

func keyspacePath(path plugin.EngineCatalogPath) bool {
	return len(path.Segments) == 2 && rootPath(plugin.EngineCatalogPath{EngineID: path.EngineID, Version: path.Version, Segments: path.Segments[:1]}) && path.Segments[1].Term == plugin.EngineCatalogTermKeyspace && path.Segments[1].Kind == plugin.EngineCatalogTermKeyspace && path.Segments[1].Name == "keyspace"
}

func (p *RedisPlugin) entry(engineID uint) plugin.EngineCatalogEntry {
	path := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), engineID)
	path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: plugin.EngineCatalogTermKeyspace, Kind: plugin.EngineCatalogTermKeyspace, Name: "keyspace"})
	return plugin.EngineCatalogEntry{Name: "keyspace", Path: path, Term: plugin.EngineCatalogTermKeyspace, Kind: plugin.EngineCatalogTermKeyspace, Role: plugin.EngineCatalogRoleLeaf}
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
	if opts.Offset < 0 || opts.Limit < 0 || opts.Recursive {
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("invalid keyspace listing options"))
	}
	if !rootPath(parent) {
		if _, err := p.ResolvePath(ctx, c, parent); err != nil {
			return nil, err
		}
		return []plugin.EngineCatalogEntry{}, nil
	}
	if err := p.TestConnection(ctx, c); err != nil {
		return nil, operationError(err)
	}
	if opts.Offset > 0 {
		return []plugin.EngineCatalogEntry{}, nil
	}
	return []plugin.EngineCatalogEntry{p.entry(parent.EngineID)}, nil
}

func (p *RedisPlugin) ResolvePath(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	if rootPath(path) {
		entry := plugin.EngineCatalogRootEntry(p.EngineCatalogModel(), path.EngineID, "")
		return &entry, nil
	}
	if _, err := p.DescribeEngineCatalogFacts(ctx, c, path, plugin.EngineCatalogFactsOptions{}); err != nil {
		return nil, err
	}
	entry := p.entry(path.EngineID)
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
	if !keyspacePath(path) {
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("exact keyspace path required"))
	}
	if err := p.TestConnection(ctx, c); err != nil {
		return nil, operationError(err)
	}
	database, err := connectionInteger(c, "database", 0, 0, 2147483647)
	if err != nil {
		return nil, err
	}
	return &plugin.EngineCatalogFacts{Path: path, Kind: plugin.EngineCatalogTermKeyspace, Keyspace: &plugin.KeyspaceFacts{Database: database}}, nil
}
