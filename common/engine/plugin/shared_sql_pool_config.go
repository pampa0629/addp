package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	poolMaxOpen     = "pool_max_open_conns"
	poolMaxIdle     = "pool_max_idle_conns"
	poolIdleSeconds = "pool_max_idle_seconds"
	poolLifeSeconds = "pool_max_lifetime_seconds"
)

var ErrStalePoolConfiguration = errors.New("SQL pool engine configuration is stale")

type poolField struct {
	key, label, hint   string
	min, max, fallback int
}

var sharedSQLPoolFields = [...]poolField{
	{poolMaxOpen, "poolMaxOpen", "poolMaxOpenHint", 1, 1000, 10},
	{poolMaxIdle, "poolMaxIdle", "poolMaxIdleHint", 0, 1000, 5},
	{poolIdleSeconds, "poolIdleSeconds", "poolTimeoutHint", 0, 86400, 0},
	{poolLifeSeconds, "poolLifetimeSeconds", "poolTimeoutHint", 0, 86400, 3600},
}

// WithSharedSQLPoolFields registers the operator-configurable shared pool policy.
func WithSharedSQLPoolFields(spec ConnectionSpec) ConnectionSpec {
	for _, field := range sharedSQLPoolFields {
		spec.Fields = append(spec.Fields, ConnectionFieldSpec{
			Key: field.key, LabelKey: "storageEngine." + field.label,
			GroupKey: "storageEngine.poolSettings", Input: ConnectionFieldNumber,
			HintKey: "storageEngine." + field.hint,
			Default: field.fallback, Min: Int(field.min), Max: Int(field.max),
		})
	}
	return spec
}

func usesSharedSQLPoolPolicy(engine EnginePlugin) bool {
	provider, ok := engine.(ConnectionSpecProvider)
	if !ok {
		return false
	}
	_, ok = provider.ConnectionSpec().Field(poolMaxOpen)
	return ok
}

// ValidateSharedSQLPoolSettings is used by System before saving connection_info.
func ValidateSharedSQLPoolSettings(engineType string, info ConnectionInfo) error {
	for _, field := range sharedSQLPoolFields {
		if _, present := info[field.key]; present {
			engine, err := Get(engineType)
			if err != nil || !usesSharedSQLPoolPolicy(engine) {
				return errors.New("engine does not support configurable shared SQL pools")
			}
			_, err = sharedSQLPoolConfig(info)
			return err
		}
	}
	return nil
}

func sharedSQLPoolConfig(info ConnectionInfo) (PoolConfig, error) {
	values := make(map[string]int, len(sharedSQLPoolFields))
	for _, field := range sharedSQLPoolFields {
		value, exists := info[field.key]
		if !exists {
			values[field.key] = field.fallback
			continue
		}
		var n int64
		switch number := value.(type) {
		case int:
			n = int64(number)
		case int64:
			n = number
		case float64:
			if number < float64(field.min) || number > float64(field.max) || number != float64(int64(number)) {
				return PoolConfig{}, fmt.Errorf("%s must be an integer from %d to %d", field.key, field.min, field.max)
			}
			n = int64(number)
		case json.Number:
			parsed, err := number.Int64()
			if err != nil {
				return PoolConfig{}, fmt.Errorf("%s must be an integer", field.key)
			}
			n = parsed
		default:
			return PoolConfig{}, fmt.Errorf("%s must be an integer", field.key)
		}
		if n < int64(field.min) || n > int64(field.max) {
			return PoolConfig{}, fmt.Errorf("%s must be an integer from %d to %d", field.key, field.min, field.max)
		}
		values[field.key] = int(n)
	}
	if values[poolMaxIdle] > values[poolMaxOpen] {
		return PoolConfig{}, fmt.Errorf("%s must not exceed %s", poolMaxIdle, poolMaxOpen)
	}
	return PoolConfig{MaxOpenConns: values[poolMaxOpen], MaxIdleConns: values[poolMaxIdle],
		ConnMaxIdleTime: time.Duration(values[poolIdleSeconds]) * time.Second,
		ConnMaxLifetime: time.Duration(values[poolLifeSeconds]) * time.Second}, nil
}

type poolEngineVersion struct {
	id      uint
	version int64
}

type poolEngineVersionKey struct{}

// WithPoolEngineVersion binds the version from an authorized Engine Instance
// to the provider call. It does not accept request or task configuration.
func WithPoolEngineVersion(ctx context.Context, engineID uint, version int64) context.Context {
	return context.WithValue(ctx, poolEngineVersionKey{}, poolEngineVersion{engineID, version})
}

func PoolEngineVersion(ctx context.Context, engineID uint) int64 {
	if ctx == nil {
		return 0
	}
	if snapshot, ok := ctx.Value(poolEngineVersionKey{}).(poolEngineVersion); ok && snapshot.id == engineID {
		return snapshot.version
	}
	return 0
}
