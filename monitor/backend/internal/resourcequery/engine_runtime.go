package resourcequery

import (
	"context"
	"strconv"
	"strings"
	"time"
)

var runtimeMetrics = map[string][]Definition{
	"document": workflowMetrics, "spark": workflowMetrics, "model3d": workflowMetrics, "pointcloud": workflowMetrics,
	"duckdb":    {{"uptime_seconds", "seconds", 0}, {"resident_memory_bytes", "bytes", 0}},
	"inference": {{"uptime_seconds", "seconds", 0}, {"resident_memory_bytes", "bytes", 0}},
	"supermap":  {{"uptime_seconds", "seconds", 0}},
	"jupyter":   {{"uptime_seconds", "seconds", 0}},
	"meilisearch": {
		{"tasks_pending", "tasks", 0},
		{"tasks_oldest_wait_seconds", "seconds", 0},
		{"tasks_retained_failed", "tasks", 0},
	},
	"geopython": {
		{"effective_cpu_cores", "cores", 0},
		{"memory_limit_bytes", "bytes", 0},
		{"raster_running", "tasks", 0},
		{"raster_waiting", "tasks", 0},
		{"raster_admission_enabled", "status", 0},
	},
}

var workflowMetrics = []Definition{{"uptime_seconds", "seconds", 0}, {"executions_running", "tasks", 0}, {"executions_pending", "tasks", 0}}

// RuntimeMetricSource contains only server-resolved identity labels.
type RuntimeMetricSource struct {
	Kind, ID, Instance string
	StartedAt          time.Time
	ContainerID        string
	EngineID           uint
}

func NewRuntimeMetricPlan(kind, metric string, start, end, now time.Time, b Budget) (Plan, error) {
	catalog := runtimeMetrics[kind]
	keys := make([]string, 0, len(catalog))
	trend := metric != ""
	if trend {
		keys = append(keys, metric)
	} else {
		start = now.UTC().Truncate(time.Second)
		end = start
		for _, metric := range catalog {
			keys = append(keys, metric.Key)
		}
	}
	return newPlan(keys, start, end, now, trend, nil, b, catalog)
}

func (s RuntimeMetricSource) selector(metric string) string {
	selector := `{job="addp_nodes",addp_source=` + strconv.Quote(s.Kind) + `,instance=` + strconv.Quote(s.Instance)
	if s.Kind == "meilisearch" {
		selector += `,addp_monitor_kind="search_resources",addp_component_id=` + strconv.Quote(s.ID) +
			`,addp_component_container_id=` + strconv.Quote(s.ContainerID) +
			`,addp_component_container_started_at=` + strconv.Quote(s.StartedAt.UTC().Format(time.RFC3339Nano))
	} else {
		selector += `,addp_monitor_kind="compute_resources",addp_instance_id=` + strconv.Quote(s.ID) +
			`,addp_runtime_engine_id=` + strconv.Quote(strconv.FormatUint(uint64(s.EngineID), 10))
	}
	if metric != "up" {
		metric = "addp_" + s.Kind + "_" + metric
	}
	return metric + selector + "}"
}

func (c *Client) EngineRuntimeQuery(ctx context.Context, p Plan, s RuntimeMetricSource, b Budget) (Summary, error) {
	if c == nil {
		return Summary{}, ErrUnavailable
	}
	expression, collection := s.expressions(p)
	return c.querySnapshot(ctx, p, b, expression, collection)
}

func (s RuntimeMetricSource) expressions(p Plan) (string, string) {
	up := s.selector("up")
	guard := snapshotGuard(up, s.selector("observed_at_seconds"))
	parts := make([]string, 0, len(p.Metrics)*2)
	for _, metric := range p.Metrics {
		raw := s.selector(metric.Key)
		filters := guard + snapshotFieldGuard(raw, up) + ` and (min(` + raw + `)>=0)`
		switch metric.Unit {
		case "status":
			filters += ` and ((min(` + raw + `)==0) or (min(` + raw + `)==1))`
		case "cores", "bytes":
			filters += ` and (min(` + raw + `)>0)`
		}
		for _, item := range []struct{ label, value string }{
			{"value", `max(` + raw + `)`},
			{"sampled_at", `max(timestamp(` + up + `))`},
		} {
			parts = append(parts, `label_replace(label_replace((`+item.value+filters+`),"addp_metric",`+strconv.Quote(metric.Key)+`,"",""),"addp_component",`+strconv.Quote(item.label)+`,"","")`)
		}
	}
	return strings.Join(parts, " or "), snapshotCollectionExpression(up, guard)
}
