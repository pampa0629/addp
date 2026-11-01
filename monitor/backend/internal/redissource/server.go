package redissource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
)

// Handler exposes only the fixed projection; no target or query API is present.
func Handler(read func(context.Context) (Snapshot, error)) http.Handler {
	busy := make(chan struct{}, 1)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/metrics" || r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			http.Error(w, "Redis metrics request rejected", http.StatusNotFound)
			return
		}
		select {
		case busy <- struct{}{}:
			defer func() { <-busy }()
		default:
			http.Error(w, "Redis metrics busy", http.StatusServiceUnavailable)
			return
		}
		snapshot, err := read(r.Context())
		if err != nil {
			http.Error(w, "Redis metrics unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		names := make([]string, 0, len(snapshot.Values))
		for name := range snapshot.Values {
			names = append(names, name)
		}
		sort.Strings(names)
		epoch := sha256.Sum256([]byte(snapshot.RunID))
		generation := hex.EncodeToString(epoch[:])
		fmt.Fprintf(w, "addp_redis_observed_at_seconds %d\n", snapshot.SampledAt.Unix())
		for _, name := range names {
			definition, ok := exportedMetrics[name]
			if !ok {
				continue
			}
			if definition.counter {
				fmt.Fprintf(w, "addp_redis_%s{counter_epoch=\"%s\"} %d\n", definition.name, generation, snapshot.Values[name])
			} else {
				fmt.Fprintf(w, "addp_redis_%s %d\n", definition.name, snapshot.Values[name])
			}
		}
	})
}

var exportedMetrics = map[string]struct {
	name    string
	counter bool
}{
	"uptime_in_seconds": {"uptime_seconds", false},
	"connected_clients": {"connections", false}, "maxclients": {"max_connections", false}, "blocked_clients": {"blocked_connections", false},
	"used_memory": {"memory_allocated_bytes", false}, "used_memory_rss": {"memory_rss_bytes", false},
	"maxmemory": {"maxmemory_bytes", false}, "mem_not_counted_for_evict": {"memory_excluded_from_limit_bytes", false},
	"memory_counted_for_limit_bytes": {"memory_counted_for_limit_bytes", false},
	"total_commands_processed":       {"commands_total", true}, "keyspace_hits": {"keyspace_hits_total", true}, "keyspace_misses": {"keyspace_misses_total", true},
	"evicted_keys": {"evicted_keys_total", true}, "rejected_connections": {"rejected_connections_total", true}, "oom_errors_total": {"oom_errors_total", true},
	"loading": {"loading", false}, "aof_enabled": {"aof_enabled", false},
	"rdb_last_bgsave_status": {"rdb_last_bgsave_ok", false}, "aof_last_write_status": {"aof_last_write_ok", false}, "aof_last_bgrewrite_status": {"aof_last_rewrite_ok", false},
}
