package resourcequery

import (
	"fmt"
	"strconv"
	"strings"
)

var redisDefinitions = []Definition{
	{"redis.uptime_seconds", "seconds", 0},
	{"redis.memory.allocated_bytes", "bytes", 0}, {"redis.memory.rss_bytes", "bytes", 0}, {"redis.memory.limit_bytes", "bytes", 0},
	{"redis.memory.excluded_bytes", "bytes", 0}, {"redis.memory.counted_bytes", "bytes", 0}, {"redis.memory.used_percent", "percent", 0},
	{"redis.connections.total", "connections", 0}, {"redis.connections.maximum", "connections", 0}, {"redis.connections.blocked", "connections", 0},
	{"redis.commands.per_second", "commands_per_second", 60}, {"redis.keyspace.hits_per_second", "events_per_second", 60}, {"redis.keyspace.misses_per_second", "events_per_second", 60},
	{"redis.keyspace.hit_percent", "percent", 60}, {"redis.evictions.per_second", "events_per_second", 60}, {"redis.connections.rejected_per_second", "events_per_second", 60}, {"redis.oom.per_second", "events_per_second", 60},
	{"redis.loading", "status", 0}, {"redis.persistence.aof_enabled", "status", 0}, {"redis.persistence.rdb_save_ok", "status", 0}, {"redis.persistence.aof_write_ok", "status", 0}, {"redis.persistence.aof_rewrite_ok", "status", 0},
}
var redisPhysical = map[string]string{
	"redis.uptime_seconds": "uptime_seconds", "redis.memory.allocated_bytes": "memory_allocated_bytes", "redis.memory.rss_bytes": "memory_rss_bytes", "redis.memory.limit_bytes": "maxmemory_bytes",
	"redis.memory.excluded_bytes": "memory_excluded_from_limit_bytes", "redis.memory.counted_bytes": "memory_counted_for_limit_bytes",
	"redis.connections.total": "connections", "redis.connections.maximum": "max_connections", "redis.connections.blocked": "blocked_connections",
	"redis.commands.per_second": "commands_total", "redis.keyspace.hits_per_second": "keyspace_hits_total", "redis.keyspace.misses_per_second": "keyspace_misses_total",
	"redis.evictions.per_second": "evicted_keys_total", "redis.connections.rejected_per_second": "rejected_connections_total", "redis.oom.per_second": "oom_errors_total",
	"redis.loading": "loading", "redis.persistence.aof_enabled": "aof_enabled", "redis.persistence.rdb_save_ok": "rdb_last_bgsave_ok", "redis.persistence.aof_write_ok": "aof_last_write_ok", "redis.persistence.aof_rewrite_ok": "aof_last_rewrite_ok",
}
var redisCounters = []string{"commands_total", "keyspace_hits_total", "keyspace_misses_total", "evicted_keys_total", "rejected_connections_total", "oom_errors_total"}
var redisFields = []string{"observed_at_seconds", "uptime_seconds", "memory_allocated_bytes", "memory_rss_bytes", "maxmemory_bytes", "memory_excluded_from_limit_bytes", "memory_counted_for_limit_bytes", "connections", "max_connections", "blocked_connections", "commands_total", "keyspace_hits_total", "keyspace_misses_total", "evicted_keys_total", "rejected_connections_total", "oom_errors_total", "loading", "aof_enabled", "rdb_last_bgsave_ok", "aof_last_write_ok", "aof_last_rewrite_ok"}

func RedisCatalog() []Definition { return append([]Definition(nil), redisDefinitions...) }
func (s EngineScope) redisSnapshotGuard(offset string) string {
	up := s.selector("up") + offset
	stamp := `max(timestamp(` + up + `))`
	age := "16"
	if offset != "" {
		age = "76"
	}
	guard := ` and (count(` + up + `)==1) and (max(` + up + `)==1) and (` + stamp + `>=time()-` + age + `)`
	for _, field := range redisFields {
		raw := s.selector(field) + offset
		guard += ` and (count(` + raw + `)==1) and (min(` + raw + `)>=0) and (max(timestamp(` + raw + `))==` + stamp + `)`
	}
	observed := `max(` + s.selector("observed_at_seconds") + offset + `)`
	guard += ` and (` + observed + `>=` + stamp + `-5) and (` + observed + `<=` + stamp + `+5) and (` + observed + `-max(` + s.selector("uptime_seconds") + offset + `)>=` + strconv.FormatInt(s.Source.StartedAt.Unix()-1, 10) + `)`
	for _, field := range []string{"loading", "aof_enabled", "rdb_last_bgsave_ok", "aof_last_write_ok", "aof_last_rewrite_ok"} {
		guard += ` and ((max(` + s.selector(field) + offset + `)==0) or (max(` + s.selector(field) + offset + `)==1))`
	}
	for _, field := range redisCounters {
		guard += ` and (count(` + strings.TrimSuffix(s.selector(field), "}") + `,counter_epoch=~"[0-9a-f]{64}"}` + offset + `)==1)`
	}
	for _, field := range redisCounters[1:] {
		guard += ` and (count(` + s.selector(field) + offset + ` and ` + s.selector(redisCounters[0]) + offset + `)==1)`
	}
	guard += ` and (max(` + s.selector("memory_excluded_from_limit_bytes") + offset + `)<=max(` + s.selector("memory_allocated_bytes") + offset + `)) and (max(` + s.selector("memory_counted_for_limit_bytes") + offset + `)==max(` + s.selector("memory_allocated_bytes") + offset + `)-max(` + s.selector("memory_excluded_from_limit_bytes") + offset + `))`
	return guard
}
func (s EngineScope) redisExpression(p Plan) (string, error) {
	if len(p.Metrics) == 0 || len(p.Metrics) > len(redisDefinitions) {
		return "", ErrInvalid
	}
	guard := s.redisSnapshotGuard("")
	parts := []string{}
	seen := map[string]bool{}
	for _, d := range p.Metrics {
		if seen[d.Key] {
			return "", ErrInvalid
		}
		seen[d.Key] = true
		field, ok := redisPhysical[d.Key]
		eligible := guard
		value := `max(` + s.selector(field) + `)`
		stamp := `max(timestamp(` + s.selector("up") + `))`
		if d.Key == "redis.memory.used_percent" {
			value = `100*max(` + s.selector("memory_counted_for_limit_bytes") + `)/max(` + s.selector("maxmemory_bytes") + `)`
			eligible += ` and (max(` + s.selector("maxmemory_bytes") + `)>0)`
		} else if d.Key == "redis.keyspace.hit_percent" {
			hits, misses := counterWindow(s.selector("keyspace_hits_total")), counterWindow(s.selector("keyspace_misses_total"))
			value = `100*max(` + hits + `)/(max(` + hits + `)+max(` + misses + `))`
			eligible += s.redisSnapshotGuard(" offset 1m") + ` and (count(` + hits + `)==1) and (count(` + misses + `)==1) and (max(` + hits + `)+max(` + misses + `)>0)`
		} else if !ok {
			return "", ErrInvalid
		} else if d.WindowSeconds == 60 {
			raw := s.selector(field)
			valid := counterWindow(raw)
			value = `max(` + valid + `)`
			eligible += s.redisSnapshotGuard(" offset 1m") + ` and (count(` + valid + `)==1)`
		}
		for _, item := range []struct{ key, value string }{{"value", value}, {"sampled_at", stamp}} {
			parts = append(parts, fmt.Sprintf(`label_replace(label_replace((%s%s),"addp_metric",%s,"",""),"addp_component",%s,"","")`, item.value, eligible, strconv.Quote(d.Key), strconv.Quote(item.key)))
		}
	}
	return strings.Join(parts, " or "), nil
}
