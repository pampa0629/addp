package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/addp/common/engine/plugin"
	rdb "github.com/redis/go-redis/v9"
)

func do(ctx context.Context, conn *rdb.Conn, args ...interface{}) *rdb.Cmd {
	cmd := rdb.NewCmd(ctx, args...)
	_ = conn.Process(ctx, cmd)
	return cmd
}

func (p *RedisPlugin) ReadKeyValue(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.KeyValueReadOptions) (*plugin.KeyValuePreview, error) {
	key, err := keyName(path)
	if err != nil {
		return nil, err
	}
	limit := opts.MaxEntries
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 50)
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = maxReplyBytes
	}
	maxBytes = min(maxBytes, maxReplyBytes)
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
	result := &plugin.KeyValuePreview{Facts: *facts, Entries: []plugin.KeyValueEntry{}}
	switch facts.NativeType {
	case "string":
		var raw string
		raw, err = conn.GetRange(ctx, key, 0, int64(min(maxBytes, 64<<10)-1)).Result()
		value := plugin.NewByteValue(raw)
		result.Value = &value
		result.Truncated = int64(len(raw)) < facts.Length
	case "hash", "set":
		err = readScanSample(ctx, conn, key, facts.NativeType, limit, result)
	case "list":
		var values []string
		values, err = conn.LRange(ctx, key, 0, int64(limit-1)).Result()
		for i, raw := range values {
			index, value := int64(i), plugin.NewByteValue(raw)
			result.Entries = append(result.Entries, plugin.KeyValueEntry{Index: &index, Value: &value})
		}
	case "zset":
		var values []string
		values, err = do(ctx, conn, "ZRANGE", key, 0, limit-1, "WITHSCORES").StringSlice()
		if err == nil && len(values)%2 != 0 {
			err = fmt.Errorf("invalid Redis sorted set response")
		}
		if err == nil {
			for i := 0; i < len(values); i += 2 {
				value := plugin.NewByteValue(values[i])
				result.Entries = append(result.Entries, plugin.KeyValueEntry{Value: &value, Score: values[i+1]})
			}
		}
	case "stream":
		var response interface{}
		response, err = do(ctx, conn, "XRANGE", key, "-", "+", "COUNT", limit).Result()
		if err == nil {
			err = readStreamSample(response, result)
		}
	default:
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorUnsupported, fmt.Errorf("Redis native value type is not supported for preview"))
	}
	if err != nil {
		return nil, operationError(err)
	}
	currentType, err := conn.Type(ctx, key).Result()
	if err != nil {
		return nil, operationError(err)
	}
	if currentType == "none" {
		return nil, operationError(rdb.Nil)
	}
	if currentType != facts.NativeType {
		return nil, operationError(fmt.Errorf("Redis key type changed during read"))
	}
	if facts.NativeType != "string" {
		result.Truncated = result.Truncated || int64(len(result.Entries)) < facts.Length
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, operationError(err)
	}
	if len(encoded) > maxBytes {
		return nil, operationError(fmt.Errorf("Redis preview bytes exceed budget"))
	}
	return result, nil
}

func readScanSample(ctx context.Context, conn *rdb.Conn, key, nativeType string, limit int, result *plugin.KeyValuePreview) error {
	seen := map[string]bool{}
	var cursor uint64
	for steps := 0; ; steps++ {
		if steps >= 1000 {
			return fmt.Errorf("Redis value iteration exceeds budget")
		}
		var values []string
		var next uint64
		var err error
		if nativeType == "hash" {
			values, next, err = conn.HScan(ctx, key, cursor, "", int64(limit)).Result()
		} else {
			values, next, err = conn.SScan(ctx, key, cursor, "", int64(limit)).Result()
		}
		if err != nil {
			return err
		}
		stride := 1
		if nativeType == "hash" {
			stride = 2
		}
		if len(values)%stride != 0 {
			return fmt.Errorf("invalid Redis value response")
		}
		for i := 0; i < len(values); i += stride {
			if seen[values[i]] {
				continue
			}
			seen[values[i]] = true
			if len(result.Entries) >= limit {
				result.Truncated = true
				return nil
			}
			var entry plugin.KeyValueEntry
			if nativeType == "hash" {
				field, value := plugin.NewByteValue(values[i]), plugin.NewByteValue(values[i+1])
				entry.Field, entry.Value = &field, &value
			} else {
				value := plugin.NewByteValue(values[i])
				entry.Value = &value
			}
			result.Entries = append(result.Entries, entry)
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
		if len(result.Entries) >= limit {
			result.Truncated = true
			return nil
		}
	}
}

func readStreamSample(response interface{}, result *plugin.KeyValuePreview) error {
	entries, ok := response.([]interface{})
	if !ok {
		return fmt.Errorf("invalid Redis stream response")
	}
	for _, raw := range entries {
		pair, ok := raw.([]interface{})
		if !ok || len(pair) != 2 {
			return fmt.Errorf("invalid Redis stream entry")
		}
		id, ok := pair[0].(string)
		if !ok {
			return fmt.Errorf("invalid Redis stream ID")
		}
		fields, ok := pair[1].([]interface{})
		if !ok || len(fields)%2 != 0 {
			return fmt.Errorf("invalid Redis stream fields")
		}
		entry := plugin.KeyValueEntry{ID: id, Fields: []plugin.KeyValueField{}}
		for i := 0; i < len(fields); i += 2 {
			name, nameOK := fields[i].(string)
			value, valueOK := fields[i+1].(string)
			if !nameOK || !valueOK {
				return fmt.Errorf("invalid Redis stream field value")
			}
			entry.Fields = append(entry.Fields, plugin.KeyValueField{Name: plugin.NewByteValue(name), Value: plugin.NewByteValue(value)})
		}
		result.Entries = append(result.Entries, entry)
	}
	return nil
}
