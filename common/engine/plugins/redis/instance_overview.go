package redis

import (
	"context"
	"strconv"
	"strings"

	"github.com/addp/common/engine/plugin"
)

func (p *RedisPlugin) ReadInstanceOverview(ctx context.Context, info plugin.ConnectionInfo) (*plugin.InstanceOverview, error) {
	client, connection, err := p.open(ctx, info)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	defer connection.Close()
	database, _ := connectionInteger(info, "database", 0, 0, 2147483647)
	metrics := []plugin.InstanceOverviewMetric{
		{Key: "database_keys", Scope: "database", ScopeName: strconv.Itoa(database), Unit: "count", Semantics: "gauge", Status: "unavailable"},
		{Key: "server_memory_bytes", Scope: "server", Unit: "bytes", Semantics: "gauge", Status: "unavailable"},
		{Key: "server_connections", Scope: "server", Unit: "count", Semantics: "gauge", Status: "unavailable"},
		{Key: "server_operations_per_second", Scope: "server", Unit: "operations_per_second", Semantics: "rate", Status: "unavailable"},
	}
	count, err := connection.DBSize(ctx).Result()
	if err != nil {
		if !strings.HasPrefix(err.Error(), "NOPERM") {
			return nil, err
		}
		metrics[0].Status = "permission_denied"
	} else if count >= 0 {
		metrics[0].SetValue(uint64(count))
	}
	text, err := connection.Info(ctx, "memory", "clients", "stats").Result()
	if err != nil {
		if !strings.HasPrefix(err.Error(), "NOPERM") {
			return nil, err
		}
		for i := 1; i < len(metrics); i++ {
			metrics[i].Status = "permission_denied"
		}
	} else {
		fields := map[string]int{"used_memory": 1, "connected_clients": 2, "instantaneous_ops_per_sec": 3}
		for _, line := range strings.Split(text, "\n") {
			key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r"), ":")
			if index, wanted := fields[key]; ok && wanted {
				if n, err := strconv.ParseUint(value, 10, 64); err == nil {
					metrics[index].SetValue(n)
				}
			}
		}
	}
	return &plugin.InstanceOverview{Metrics: metrics}, nil
}
