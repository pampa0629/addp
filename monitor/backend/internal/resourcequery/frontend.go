package resourcequery

import (
	"context"
	"math"
	"regexp"
	"slices"
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

var frontendModes = []string{"vite", "compose", "kubernetes"}

var frontendMetrics = []string{"entry_up", "asset_up", "cpu_cores", "memory_bytes", "restart_count"}

type FrontendProjection struct {
	modules []string
	metrics []string
}

var moduleKey = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,49}$`)

func FrontendTotalsProjection() FrontendProjection {
	return FrontendProjection{frontendprobe.Modules[:], []string{"entry_up", "cpu_cores", "memory_bytes"}}
}

func NewFrontendProjection(view, names string) (FrontendProjection, error) {
	if view == "inventory" && names == "" {
		return FrontendProjection{frontendprobe.Modules[:], []string{"entry_up"}}, nil
	}
	modules := strings.Split(names, ",")
	metrics := []string{"cpu_cores", "memory_bytes"}
	switch view {
	case "cards":
		if len(modules) > 20 {
			return FrontendProjection{}, ErrInvalid
		}
	case "detail":
		if len(modules) != 1 {
			return FrontendProjection{}, ErrInvalid
		}
		metrics = frontendMetrics
	default:
		return FrontendProjection{}, ErrInvalid
	}
	selected := []string{}
	for i, module := range modules {
		if !moduleKey.MatchString(module) || slices.Contains(modules[:i], module) {
			return FrontendProjection{}, ErrInvalid
		}
		if slices.Contains(frontendprobe.Modules[:], module) {
			selected = append(selected, module)
		}
	}
	return FrontendProjection{selected, metrics}, nil
}

func (c *Client) FrontendSamples(ctx context.Context, nodeID, address, revision string, at time.Time, budget Budget, projection FrontendProjection) ([]FrontendSample, error) {
	if c == nil {
		return nil, ErrUnavailable
	}
	if len(projection.modules) == 0 {
		return []FrontendSample{}, nil
	}
	if budget.MaxMetrics < len(projection.metrics) || budget.MaxSeries < 1 || budget.MaxTotalPoints < 1 {
		return nil, ErrBudget
	}
	expression := frontendExpression(nodeID, address, revision, projection)
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
	for _, module := range projection.modules {
		for _, mode := range frontendModes {
			for _, metric := range projection.metrics {
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
	rows := make([]FrontendSample, 0, len(projection.modules))
	for _, module := range projection.modules {
		row := FrontendSample{Module: module}
		for _, mode := range frontendModes {
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
			if row.Mode != "" || (entry == nil && slices.Contains(projection.metrics, "entry_up")) || (entry != nil && *entry != 0 && *entry != 1) || (asset != nil && ((*asset != 0 && *asset != 1) || entry == nil || *entry == 0)) ||
				(restarts != nil && (mode == "vite" || *restarts != math.Trunc(*restarts))) {
				return nil, ErrUnavailable
			}
			row.Mode = mode
			if entry != nil {
				up := *entry == 1
				row.EntryUp = &up
			}
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

func frontendExpression(nodeID, address, revision string, projection FrontendProjection) string {
	selector := deploymentNodeSelector(nodeID, address, revision)
	up := "up{" + selector + "}"
	join := " on(instance,addp_node_id) "
	// timestamp() removes the metric name. Keep each fixed metric in its own
	// operand so freshness comparison cannot merge across the catalog.
	parts := make([]string, 0, len(projection.metrics))
	for _, key := range projection.metrics {
		labels := `{frontend_module=~` + strconv.Quote(strings.Join(projection.modules, "|")) + ",frontend_mode=~" + strconv.Quote(strings.Join(frontendModes, "|")) + "," + selector + "}"
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
