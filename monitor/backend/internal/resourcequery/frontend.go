package resourcequery

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"
)

type FrontendSample struct {
	Module       string   `json:"module"`
	Mode         string   `json:"mode"`
	EntryUp      *bool    `json:"entry_up"`
	AssetUp      *bool    `json:"asset_up"`
	CPUCores     *float64 `json:"cpu_cores"`
	MemoryBytes  *float64 `json:"memory_bytes"`
	RestartCount *float64 `json:"restart_count"`
}

var frontendMetrics = []string{"entry_up", "asset_up", "cpu_cores", "memory_bytes", "restart_count"}

func (c *Client) FrontendSamples(ctx context.Context, nodeID, address, revision string, at time.Time, budget Budget) ([]FrontendSample, error) {
	const maximumSeries = 2 * 5
	if c == nil {
		return nil, ErrUnavailable
	}
	if budget.MaxMetrics < len(frontendMetrics) || budget.MaxSeries < maximumSeries || budget.MaxTotalPoints < maximumSeries {
		return nil, ErrBudget
	}
	expression := frontendExpression(nodeID, address, revision)
	endpoint, args := planExpressionQuery(Plan{End: at}, expression, budget)
	wire, err := c.readForm(ctx, endpoint, args)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool)
	for _, module := range []string{"console", "monitor"} {
		for _, mode := range []string{"vite", "compose"} {
			for _, metric := range frontendMetrics {
				allowed[module+":"+mode+":"+metric] = true
			}
		}
	}
	if len(wire.Data.Result) > maximumSeries {
		return nil, ErrUnavailable
	}
	for i := range wire.Data.Result {
		m := wire.Data.Result[i].Metric
		if m["instance"] != address || m["addp_node_id"] != nodeID || m["addp_frontend_revision"] != revision {
			return nil, ErrUnavailable
		}
		wire.Data.Result[i].Metric = map[string]string{"signal": m["frontend_module"] + ":" + m["frontend_mode"] + ":" + strings.TrimPrefix(m["__name__"], "addp_frontend_")}
	}
	values, err := normalizeSignals(wire, at, allowed)
	if err != nil {
		return nil, err
	}
	rows := make([]FrontendSample, 0, 2)
	for _, module := range []string{"console", "monitor"} {
		row := FrontendSample{Module: module}
		for _, mode := range []string{"vite", "compose"} {
			lookup := func(key string) *float64 {
				value, ok := values[module+":"+mode+":"+key]
				if !ok {
					return nil
				}
				return &value
			}
			entry := lookup("entry_up")
			asset := lookup("asset_up")
			cpu, memory, restarts := lookup("cpu_cores"), lookup("memory_bytes"), lookup("restart_count")
			if entry == nil && asset == nil && cpu == nil && memory == nil && restarts == nil {
				continue
			}
			if row.Mode != "" || entry == nil || (*entry != 0 && *entry != 1) || (asset != nil && ((*asset != 0 && *asset != 1) || *entry == 0)) ||
				(restarts != nil && (mode != "compose" || *restarts != math.Trunc(*restarts))) {
				return nil, ErrUnavailable
			}
			up := *entry == 1
			row.Mode, row.EntryUp = mode, &up
			if asset != nil {
				up := *asset == 1
				row.AssetUp = &up
			}
			row.CPUCores, row.MemoryBytes, row.RestartCount = cpu, memory, restarts
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func frontendExpression(nodeID, address, revision string) string {
	selector := `job="addp_nodes",addp_monitor_kind="frontend_resources",addp_source="frontend_exporter",addp_node_id=` +
		strconv.Quote(nodeID) + ",instance=" + strconv.Quote(address) + ",addp_frontend_revision=" + strconv.Quote(revision)
	up := "up{" + selector + "}"
	join := " on(instance,addp_node_id) "
	// timestamp() removes the metric name. Keep each fixed metric in its own
	// operand so freshness comparison cannot merge across the catalog.
	parts := make([]string, 0, len(frontendMetrics))
	for _, key := range frontendMetrics {
		field := "addp_frontend_" + key + `{frontend_module=~"console|monitor",frontend_mode=~"vite|compose",` + selector + "}"
		parts = append(parts, "("+field+" and "+join+"("+up+"==1) and "+join+"(timestamp("+up+")>=time()-30) and on(instance,addp_node_id,frontend_module,frontend_mode) "+
			"(timestamp("+field+") == "+join+"group_left timestamp("+up+")))")
	}
	return strings.Join(parts, " or on(__name__,instance,addp_node_id,frontend_module,frontend_mode) ")
}
