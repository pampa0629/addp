package infraconfig

import (
	"context"
	"math"
	"strings"

	"github.com/redis/go-redis/v9"
)

type redisConfiguration struct{ client *redis.Client }
type redisBinding struct{ options redis.Options }

func (r redisBinding) read(ctx context.Context) ([]Parameter, error) {
	client := redis.NewClient(&r.options)
	defer client.Close()
	return (redisConfiguration{client}).read(ctx)
}

func (r redisBinding) apply(ctx context.Context, name, value string, record func(string) error) (Outcome, error) {
	client := redis.NewClient(&r.options)
	defer client.Close()
	return (redisConfiguration{client}).apply(ctx, name, value, record)
}

type redisSetting struct {
	name, unit string
	writable   bool
	min, max   int64
}

var redisSettings = []redisSetting{
	{"slowlog-log-slower-than", "us", true, -1, math.MaxInt64},
	{"slowlog-max-len", "entries", true, 0, math.MaxInt32},
	{"latency-monitor-threshold", "ms", true, 0, math.MaxInt32},
	{name: "maxmemory", unit: "bytes"},
	{name: "maxmemory-policy"},
	{name: "appendonly"},
	{name: "appendfsync"},
	{name: "save"},
	{name: "maxclients", unit: "connections"},
	{name: "timeout", unit: "s"},
}

func (r redisConfiguration) hasConfigurationFile(ctx context.Context) (bool, error) {
	info, err := r.client.Info(ctx, "server").Result()
	if err != nil {
		return false, ErrUnavailable
	}
	for _, line := range strings.Split(info, "\r\n") {
		if path, ok := strings.CutPrefix(line, "config_file:"); ok {
			return path != "", nil
		}
	}
	return false, ErrUnavailable
}

func (r redisConfiguration) parameter(ctx context.Context, setting redisSetting, hasFile bool) (Parameter, error) {
	values, err := r.client.ConfigGet(ctx, setting.name).Result()
	if err != nil {
		return Parameter{}, ErrUnavailable
	}
	value, exists := values[setting.name]
	if !exists {
		return Parameter{}, ErrUnavailable
	}
	activation := "native"
	if setting.writable {
		activation = "online"
	}
	return Parameter{
		Name: setting.name, Value: value, Unit: setting.unit, Source: "redis_runtime",
		Activation: activation, Editable: setting.writable && hasFile,
	}, nil
}

func (r redisConfiguration) read(ctx context.Context) ([]Parameter, error) {
	hasFile, err := r.hasConfigurationFile(ctx)
	if err != nil {
		return nil, err
	}
	parameters := make([]Parameter, 0, len(redisSettings))
	for _, setting := range redisSettings {
		value, err := r.parameter(ctx, setting, hasFile)
		if err != nil {
			return nil, err
		}
		parameters = append(parameters, value)
	}
	return parameters, nil
}

func (r redisConfiguration) apply(ctx context.Context, name, value string, record func(string) error) (Outcome, error) {
	var setting redisSetting
	for _, candidate := range redisSettings {
		if candidate.name == name {
			setting = candidate
			break
		}
	}
	if setting.name == "" {
		return Outcome{}, ErrNotFound
	}
	if !setting.writable {
		return Outcome{}, ErrReadOnly
	}
	value, err := intValue(value, setting.min, setting.max)
	if err != nil {
		return Outcome{}, err
	}
	hasFile, err := r.hasConfigurationFile(ctx)
	if err != nil {
		return Outcome{}, err
	}
	if !hasFile {
		return Outcome{}, ErrReadOnly
	}
	before, err := r.parameter(ctx, setting, hasFile)
	if err != nil {
		return Outcome{}, err
	}
	if err := record(before.Value); err != nil {
		return Outcome{}, err
	}
	out := Outcome{PreviousValue: before.Value, RequestedValue: value, Application: "unconfirmed", Persistence: "not_attempted"}
	if err := r.client.ConfigSet(ctx, name, value).Err(); err != nil {
		out.FailureStage = "write"
		return out, nil
	}
	out.Persistence = "unconfirmed"
	if err := r.client.ConfigRewrite(ctx).Err(); err != nil {
		out.FailureStage = "persist"
	}
	current, err := r.parameter(ctx, setting, hasFile)
	if err == nil {
		out.Current = &current
	}
	if err != nil || current.Value != value {
		if out.FailureStage == "" {
			out.FailureStage = "verify"
		}
		return out, nil
	}
	out.Application = "confirmed"
	if out.FailureStage == "" {
		out.Persistence = "confirmed"
	}
	return out, nil
}
