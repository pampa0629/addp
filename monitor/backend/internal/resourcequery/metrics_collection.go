package resourcequery

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"
)

type MetricsCollectionHealth struct {
	Discovered     int   `json:"discovered"`
	Online         *int  `json:"online"`
	Failed         *int  `json:"failed"`
	DiscoveryError *bool `json:"discovery_error"`
	TSDBBytes      int64 `json:"tsdb_bytes"`
	RetentionBytes int64 `json:"retention_bytes"`
}

func (c *Client) CollectionHealth(ctx context.Context, at time.Time, budget Budget) (MetricsCollectionHealth, error) {
	var health MetricsCollectionHealth
	if c == nil {
		return health, ErrUnavailable
	}
	const signals = 6
	if budget.MaxMetrics < signals || budget.MaxSeries < signals || budget.MaxTotalPoints < signals {
		return health, ErrBudget
	}
	self := `up{job="prometheus"}`
	stamp := `max(timestamp(` + self + `))`
	selfGuard := ` and (count(` + self + `)==1) and (max(` + self + `)==1) and (` + stamp + `>=time()-16)`
	selfMetric := func(metric string) string {
		guard := `(max(` + metric + `)` + snapshotFieldGuard(metric, self)
		return guard + ")"
	}
	external := `up{job="addp_nodes"}`
	current := ` and (timestamp(` + external + `)>=time()-16)`
	failures := `prometheus_sd_http_failures_total{job="prometheus"}`
	fields := []struct{ key, expression string }{
		{"discovered", selfMetric(`prometheus_sd_discovered_targets{job="prometheus",config="addp_nodes",name="scrape"}`)},
		{"online", `(count((` + external + `==1)` + current + `) or vector(0))`},
		{"failed", `(count((` + external + `==0)` + current + `) or vector(0))`},
		{"discovery_error", `((max(increase(` + failures + `[5m])) > bool 0)` + snapshotFieldGuard(failures, self) + `)`},
		{"tsdb_bytes", selfMetric(`prometheus_tsdb_storage_blocks_bytes{job="prometheus"}`) +
			`+` + selfMetric(`prometheus_tsdb_head_chunks_storage_size_bytes{job="prometheus"}`) +
			`+` + selfMetric(`prometheus_tsdb_wal_storage_size_bytes{job="prometheus"}`)},
		{"retention_bytes", selfMetric(`prometheus_tsdb_retention_limit_bytes{job="prometheus"}`)},
	}
	parts, allowed := make([]string, 0, len(fields)), make(map[string]bool, len(fields))
	for _, s := range fields {
		parts = append(parts, `label_replace(((`+s.expression+`)`+selfGuard+`),"signal",`+strconv.Quote(s.key)+`,"","")`)
		allowed[s.key] = true
	}
	endpoint, args := planExpressionQuery(Plan{End: at}, strings.Join(parts, " or "), budget)
	data, err := c.readForm(ctx, endpoint, args)
	if err != nil {
		return health, err
	}
	values, err := normalizeSignals(data, at, allowed)
	if err != nil {
		return health, err
	}
	for _, key := range []string{"discovered", "online", "failed", "tsdb_bytes", "retention_bytes"} {
		v, ok := values[key]
		if !ok || v >= 1<<53 || v != math.Trunc(v) {
			return health, ErrUnavailable
		}
	}
	health.Discovered = int(values["discovered"])
	online, failed := int(values["online"]), int(values["failed"])
	if online+failed <= health.Discovered {
		health.Online, health.Failed = &online, &failed
	}
	if failure, ok := values["discovery_error"]; ok {
		if failure != 0 && failure != 1 {
			return health, ErrUnavailable
		}
		flag := failure == 1
		health.DiscoveryError = &flag
	}
	health.TSDBBytes, health.RetentionBytes = int64(values["tsdb_bytes"]), int64(values["retention_bytes"])
	return health, nil
}
