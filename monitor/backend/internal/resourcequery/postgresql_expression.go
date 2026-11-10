package resourcequery

import (
	"fmt"
	"strconv"
	"strings"
)

var postgresqlPhysical = map[string]string{
	"postgresql.connections.total":                   "connections",
	"postgresql.connections.active":                  "active_connections",
	"postgresql.connections.idle":                    "idle_connections",
	"postgresql.connections.idle_in_transaction":     "idle_in_transaction_connections",
	"postgresql.connections.waiting":                 "active_waiting_connections",
	"postgresql.connections.lock_waiting":            "active_lock_waiting_connections",
	"postgresql.connections.io_waiting":              "active_io_waiting_connections",
	"postgresql.connections.maximum":                 "max_connections",
	"postgresql.transactions.committed_per_second":   "transactions_committed_total",
	"postgresql.transactions.rolled_back_per_second": "transactions_rolled_back_total",
	"postgresql.deadlocks.per_second":                "deadlocks_total",
	"postgresql.database.total_bytes":                "database_bytes",
	"postgresql.replication.logical_slots":           "logical_slots",
	"postgresql.replication.inactive_logical_slots":  "inactive_logical_slots",
	"postgresql.replication.retained_wal_bytes":      "logical_slot_retained_wal_bytes",
}

// postgresqlSnapshotGuard requires all non-nullable snapshot fields from the same scrape.
// A successful exporter HTTP response alone never proves a SQL snapshot.
func (s EngineScope) postgresqlSnapshotGuard(offset string) string {
	up, observed := s.selector("up")+offset, s.selector("sample_time_seconds")+offset
	stamp := `max(timestamp(` + up + `))`
	guard := ` and (count(` + up + `)==1) and (max(` + up + `)==1)` +
		` and (` + stamp + `>=time()-` + func() string {
		if offset == "" {
			return "16"
		}
		return "76"
	}() + `)`
	for _, field := range []string{"sample_time_seconds", "server_start_time_seconds", "track_counts_enabled", "connections", "active_connections", "idle_connections", "idle_in_transaction_connections", "active_waiting_connections", "active_lock_waiting_connections", "active_io_waiting_connections", "max_connections", "database_bytes", "statistics_info"} {
		raw := s.selector(field) + offset
		guard += ` and (count(` + raw + `)==1) and (max(timestamp(` + raw + `))==` + stamp + `) and (min(` + raw + `)>=0)`
	}
	guard += ` and (max(` + observed + `)>=` + stamp + `-5) and (max(` + observed + `)<=` + stamp + `+5)`
	guard += ` and (max(` + s.selector("statistics_info") + offset + `)==1)` +
		` and ((max(` + s.selector("track_counts_enabled") + offset + `)==0) or (max(` + s.selector("track_counts_enabled") + offset + `)==1))` +
		` and (max(` + s.selector("server_start_time_seconds") + offset + `)>=` + strconv.FormatInt(s.Source.StartedAt.Unix()-1, 10) + `)`
	return guard
}

func (s EngineScope) postgresqlExpression(p Plan) (string, error) {
	if s.Validate() != nil || len(p.Metrics) == 0 || len(p.Metrics) > len(postgresqlDefinitions) {
		return "", ErrInvalid
	}
	guard := s.postgresqlSnapshotGuard("")
	parts, seen := []string{}, map[string]bool{}
	for _, d := range p.Metrics {
		field, ok := postgresqlPhysical[d.Key]
		if !ok || seen[d.Key] {
			return "", ErrInvalid
		}
		seen[d.Key] = true
		raw := s.selector(field)
		value, stamp := `max(`+raw+`)`, `max(timestamp(`+raw+`))`
		eligible := guard
		if d.WindowSeconds == 60 {
			previous := raw + ` offset 1m`
			valid := counterWindow(raw)
			// counter_epoch is part of the vector match; statistics_generation
			// and server start must also match across both window boundaries.
			eligible += s.postgresqlSnapshotGuard(" offset 1m") + ` and (max(` + s.selector("track_counts_enabled") + `)==1) and (max(` + s.selector("track_counts_enabled") + ` offset 1m)==1)` +
				` and (max(` + s.selector("server_start_time_seconds") + `)==max(` + s.selector("server_start_time_seconds") + ` offset 1m))` +
				` and (count(` + s.selector("statistics_info") + ` and ` + s.selector("statistics_info") + ` offset 1m)==1)` +
				` and (count(` + previous + `)==1) and (max(timestamp(` + previous + `))==max(timestamp(` + s.selector("up") + ` offset 1m))) and (count(` + valid + `)==1)`
			value = `max(` + valid + `)`
		}
		eligible += ` and (count(` + raw + `)==1) and (min(` + raw + `)>=0) and (max(timestamp(` + raw + `))==max(timestamp(` + s.selector("up") + `)))`
		for _, item := range []struct{ key, expression string }{{"value", value}, {"sampled_at", stamp}} {
			parts = append(parts, fmt.Sprintf(`label_replace(label_replace((%s%s),"addp_metric",%s,"",""),"addp_component",%s,"","")`, item.expression, eligible, strconv.Quote(d.Key), strconv.Quote(item.key)))
		}
	}
	return strings.Join(parts, " or "), nil
}
