package resourcequery

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/addp/monitor/internal/frontendprobe"
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
	if c == nil {
		return nil, ErrUnavailable
	}
	if budget.MaxMetrics < len(frontendMetrics) || budget.MaxSeries < 1 || budget.MaxTotalPoints < 1 {
		return nil, ErrBudget
	}
	expression := frontendExpression(nodeID, address, revision)
	endpoint, args := planExpressionQuery(Plan{End: at}, expression, budget)
	limit := min(budget.MaxSeries, budget.MaxTotalPoints)
	args.Set("limit", strconv.Itoa(limit+1))
	wire, err := c.readForm(ctx, endpoint, args)
	if len(wire.Data.Result) > limit {
		return nil, ErrBudget
	}
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool)
	for _, module := range frontendprobe.Modules {
		for _, mode := range []string{"vite", "compose"} {
			for _, metric := range frontendMetrics {
				allowed[module+":"+mode+":"+metric] = true
			}
		}
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
	rows := make([]FrontendSample, 0, len(frontendprobe.Modules))
	for _, module := range frontendprobe.Modules {
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
		if row.Mode != "" {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func frontendExpression(nodeID, address, revision string) string {
	selector := deploymentNodeSelector(nodeID, address, revision)
	up := "up{" + selector + "}"
	join := " on(instance,addp_node_id) "
	// timestamp() removes the metric name. Keep each fixed metric in its own
	// operand so freshness comparison cannot merge across the catalog.
	parts := make([]string, 0, len(frontendMetrics))
	for _, key := range frontendMetrics {
		labels := `{frontend_module=~` + strconv.Quote(strings.Join(frontendprobe.Modules[:], "|")) + `,frontend_mode=~"vite|compose",` + selector + "}"
		field := "addp_frontend_" + key + labels
		if key == "cpu_cores" {
			counter := "addp_frontend_cpu_seconds_total" + strings.TrimSuffix(labels, "}") + `,frontend_generation!=""}`
			prior := counter + " offset 1m"
			rate := "rate(" + counter + "[1m])"
			// Match the process CPU window. A sample from the same generation
			// and revision must already exist at the beginning of the minute.
			value := rate + " >= 0 and (count_over_time(" + counter + "[1m]) >= 4) and (resets(" + counter + "[1m]) == 0)" +
				" and (" + counter + " >= " + prior + ") and (timestamp(" + prior + ") >= time()-76) and (timestamp(" + counter + ") >= time()-16)" +
				" and " + join + "(min_over_time(" + up + "[1m]) == 1) and " + join + "(" + up + " == 1)" +
				" and (timestamp(" + counter + ") == " + join + "group_left timestamp(" + up + "))"
			parts = append(parts, `label_replace((`+value+`),"__name__","addp_frontend_cpu_cores","","")`)
			continue
		}
		parts = append(parts, "("+field+" and "+join+"("+up+"==1) and "+join+"(timestamp("+up+")>=time()-30) and on(instance,addp_node_id,frontend_module,frontend_mode) "+
			"(timestamp("+field+") == "+join+"group_left timestamp("+up+")))")
	}
	return strings.Join(parts, " or on(__name__,instance,addp_node_id,frontend_module,frontend_mode) ")
}

func deploymentNodeSelector(nodeID, address, revision string) string {
	return `job="addp_nodes",addp_monitor_kind="frontend_resources",addp_source="frontend_exporter",addp_node_id=` +
		strconv.Quote(nodeID) + ",instance=" + strconv.Quote(address) + ",addp_frontend_revision=" + strconv.Quote(revision)
}

func (c *Client) BusinessNFSExport(ctx context.Context, nodeID, address, revision string, at time.Time, budget Budget) (*bool, error) {
	if c == nil {
		return nil, ErrUnavailable
	}
	selector := "{" + deploymentNodeSelector(nodeID, address, revision) + "}"
	field, up := "addp_business_nfs_export_available"+selector, "up"+selector
	guard := " and on(instance,addp_node_id) "
	expr := field + guard + "(" + up + "==1)" + guard + "(timestamp(" + up + ")>=time()-30)" + guard + "(timestamp(" + field + ")==on(instance,addp_node_id) timestamp(" + up + "))"
	endpoint, args := planExpressionQuery(Plan{End: at}, expr, budget)
	args.Set("limit", "2")
	wire, err := c.readForm(ctx, endpoint, args)
	if err != nil {
		return nil, err
	}
	for i, row := range wire.Data.Result {
		m := row.Metric
		if m["instance"] != address || m["addp_node_id"] != nodeID || m["addp_frontend_revision"] != revision || m["__name__"] != "addp_business_nfs_export_available" {
			return nil, ErrUnavailable
		}
		wire.Data.Result[i].Metric = map[string]string{"signal": "export"}
	}
	values, err := normalizeSignals(wire, at, map[string]bool{"export": true})
	if err != nil {
		return nil, err
	}
	v, ok := values["export"]
	if !ok {
		return nil, nil
	}
	if v != 0 && v != 1 {
		return nil, ErrUnavailable
	}
	available := v == 1
	return &available, nil
}
