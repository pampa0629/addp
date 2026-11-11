package resourcequery

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/models"
)

// CDC scopes are infrastructure components, never engine deployments.
type CDCScope struct {
	Component models.ObservabilityComponentIdentity
	Instance  string
}

var cdcDefinitions = map[string][]Definition{
	"redpanda": {
		{"cdc.redpanda.disk_total_bytes", "bytes", 0}, {"cdc.redpanda.disk_free_bytes", "bytes", 0},
		{"cdc.redpanda.disk_used_percent", "percent", 0}, {"cdc.redpanda.unavailable_partitions", "partitions", 0},
		{"cdc.redpanda.consumer_group_partitions", "partitions", 0}, {"cdc.redpanda.consumer_group_pending_records", "records", 0},
	},
	"kafka-connect": {
		{"cdc.connect.connectors", "connectors", 0}, {"cdc.connect.tasks", "tasks", 0},
		{"cdc.connect.failed_connectors", "connectors", 0}, {"cdc.connect.failed_tasks", "tasks", 0},
		{"cdc.connect.running_tasks", "tasks", 0}, {"cdc.connect.paused_tasks", "tasks", 0},
		{"cdc.connect.offset_commit_tasks", "tasks", 0}, {"cdc.connect.offset_commit_failure_percent", "percent", 0},
		{"cdc.connect.streaming_connectors", "connectors", 0}, {"cdc.connect.streaming_connected", "connectors", 0},
		{"cdc.connect.source_delay_connectors", "connectors", 0}, {"cdc.connect.source_delay_max_seconds", "seconds", 0},
	},
}

func CDCCatalog(kind string) []Definition { return append([]Definition(nil), cdcDefinitions[kind]...) }
func NewCDCPlan(kind string, keys []string, start, end, now time.Time, trend bool, b Budget) (Plan, error) {
	if len(CDCCatalog(kind)) == 0 {
		return Plan{}, ErrInvalid
	}
	return newPlan(keys, start, end, now, trend, nil, b, CDCCatalog(kind))
}
func (s CDCScope) selector(field string) string {
	prefix := "redpanda"
	if s.Component.Kind == "kafka-connect" {
		prefix = "connect"
	}
	metric := "addp_cdc_" + prefix + "_" + field
	if field == "up" {
		metric = "up"
	}
	return metric + `{job="addp_nodes",addp_monitor_kind="cdc_resources",addp_source=` + strconv.Quote(s.Component.Kind) + `,addp_component_id=` + strconv.Quote(s.Component.ID()) + `,addp_component_container_id=` + strconv.Quote(s.Component.ContainerID) + `,addp_component_container_started_at=` + strconv.Quote(s.Component.StartedAt.UTC().Format(time.RFC3339Nano)) + `,instance=` + strconv.Quote(s.Instance) + `}`
}
func (s CDCScope) fieldGuard(raw string) string {
	return ` and (count(` + raw + `)==1) and (max(timestamp(` + raw + `))==max(timestamp(` + s.selector("up") + `)))`
}
func (s CDCScope) snapshotGuard() string {
	up, observed := s.selector("up"), s.selector("observed_at_seconds")
	stamp := `max(timestamp(` + up + `))`
	return ` and (count(` + up + `)==1) and (max(` + up + `)==1) and (` + stamp + `>=time()-16)` + s.fieldGuard(observed) +
		` and (max(` + observed + `)>=` + stamp + `-5) and (max(` + observed + `)<=` + stamp + `+5)`
}
func (s CDCScope) expression(p Plan) (string, error) {
	if s.Component.Validate() != nil || s.Component.Metrics.State != "available" || s.Instance == "" || strings.ContainsAny(s.Instance, "\r\n") || len(s.Instance) > 512 || len(p.Metrics) == 0 || len(p.Metrics) > 12 {
		return "", ErrInvalid
	}
	parts, seen := []string{}, map[string]bool{}
	for _, d := range p.Metrics {
		canonical := false
		for _, def := range CDCCatalog(s.Component.Kind) {
			if d == def {
				canonical = true
			}
		}
		if !canonical || seen[d.Key] {
			return "", ErrInvalid
		}
		seen[d.Key] = true
		field := strings.SplitN(d.Key, ".", 3)[2]
		raw := s.selector(field)
		multiplier := ""
		switch field {
		case "failed_connectors":
			raw = strings.TrimSuffix(s.selector("connector_states"), "}") + `,state="FAILED"}`
		case "failed_tasks", "running_tasks", "paused_tasks":
			state := map[string]string{"failed_tasks": "FAILED", "running_tasks": "RUNNING", "paused_tasks": "PAUSED"}[field]
			raw = strings.TrimSuffix(s.selector("task_states"), "}") + `,state=` + strconv.Quote(state) + `}`
		case "offset_commit_failure_percent":
			raw = s.selector("offset_commit_failure_ratio_max")
			multiplier = "100*"
		}
		guard := s.snapshotGuard() + s.fieldGuard(raw)
		value := multiplier + `max(` + raw + `)`
		if field == "disk_used_percent" {
			total, free := s.selector("disk_total_bytes"), s.selector("disk_free_bytes")
			value = `100*(max(` + total + `)-max(` + free + `))/max(` + total + `)`
			guard = s.snapshotGuard() + s.fieldGuard(total) + s.fieldGuard(free) + ` and (min(` + free + `)>=0) and (max(` + total + `)>0) and (max(` + free + `)<=max(` + total + `))`
		} else if field != "source_delay_max_seconds" {
			guard += ` and (min(` + raw + `)>=0)`
		}
		if field == "offset_commit_failure_percent" {
			guard += ` and (max(` + raw + `)<=1)`
		}
		for _, item := range []struct{ k, v string }{{"value", value}, {"sampled_at", `max(timestamp(` + s.selector("up") + `))`}} {
			parts = append(parts, fmt.Sprintf(`label_replace(label_replace((%s%s),"addp_metric",%s,"",""),"addp_component",%s,"","")`, item.v, guard, strconv.Quote(d.Key), strconv.Quote(item.k)))
		}
	}
	return strings.Join(parts, " or "), nil
}
func (s CDCScope) collectionExpression() string {
	up := s.selector("up")
	parts := []string{}
	for _, item := range []struct{ k, v string }{{"up_count", `count(` + up + `) or vector(0)`}, {"up", `max(` + up + `)`}, {"sampled_at", `max(timestamp(` + up + `))`}, {"snapshot_valid", `(vector(1)` + s.snapshotGuard() + `) or vector(0)`}} {
		parts = append(parts, `label_replace((`+item.v+`),"signal",`+strconv.Quote(item.k)+`,"","")`)
	}
	return strings.Join(parts, " or ")
}
func (c *Client) CDCQuery(ctx context.Context, p Plan, s CDCScope, b Budget) (Summary, error) {
	if b.Validate() != nil {
		return Summary{}, ErrInvalid
	}
	if c == nil {
		return Summary{}, ErrUnavailable
	}
	expression, err := s.expression(p)
	if err != nil {
		return Summary{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(b.TimeoutSeconds)*time.Second)
	defer cancel()
	endpoint, q := planExpressionQuery(p, expression, b)
	data, err := c.readForm(ctx, endpoint, q)
	if err != nil {
		return Summary{}, err
	}
	series, err := normalize(data, p, b)
	if err != nil {
		return Summary{}, err
	}
	if p.Trend {
		return Summary{Series: series}, nil
	}
	_, q = planExpressionQuery(p, s.collectionExpression(), b)
	evidence, err := c.readForm(ctx, "/api/v1/query", q)
	if err != nil {
		return Summary{}, err
	}
	collection, err := normalizeEngineCollection(evidence, p.End)
	if err != nil {
		return Summary{}, err
	}
	if collection.State != "collecting" {
		series = Empty(p, "no_data")
	}
	return Summary{Series: series, Collection: collection}, nil
}
