package resourcequery

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/addp/common/models"
)

var processDefinitions = []Definition{{"process.cpu.core_equivalents", "cores", 60}, {"process.memory.resident_bytes", "bytes", 0}, {"process.uptime_seconds", "seconds", 0}}

func ProcessCatalog() []Definition { return append([]Definition(nil), processDefinitions...) }

func NewProcessSummaryPlan(count int, at time.Time, b Budget) (Plan, error) {
	if count < 1 || count > 100 {
		return Plan{}, ErrInvalid
	}
	keys := make([]string, len(processDefinitions))
	for i, d := range processDefinitions {
		keys[i] = d.Key
	}
	p, err := newPlan(keys, at, at, at, false, nil, b, processDefinitions)
	if err == nil && (count*len(p.Metrics) > b.MaxSeries || count*len(p.Metrics) > b.MaxTotalPoints) {
		return Plan{}, ErrBudget
	}
	return p, err
}

// ProcessScope refers to a currently authorized System record and its immutable
// instance declaration. The physical source is private transport scope only.
type ProcessScope struct {
	ID                                     uint
	ModuleName, InstanceID, Role, Instance string
	StartedAt                              time.Time
}

func (s ProcessScope) Validate() error {
	validName := func(value string, max int) bool {
		return value != "" && strings.TrimSpace(value) == value && utf8.ValidString(value) && utf8.RuneCountInString(value) <= max && strings.IndexFunc(value, unicode.IsControl) < 0
	}
	if _, err := models.ParseRuntimeInstanceIDs(strconv.FormatUint(uint64(s.ID), 10)); err != nil {
		return ErrInvalid
	}
	if !validName(s.ModuleName, 50) || !validName(s.InstanceID, 100) || s.Instance == "" || len(s.Instance) > 512 || strings.ContainsAny(s.Instance, "\r\n") || s.StartedAt.IsZero() {
		return ErrInvalid
	}
	switch s.Role {
	case "backend", "worker", "scheduler", "ingress":
	default:
		return ErrInvalid
	}
	return nil
}

func (s ProcessScope) selector(metric string) string {
	return metric + `{job="addp_nodes",addp_monitor_kind="process_resources",addp_source="application",addp_module_name=` + strconv.Quote(s.ModuleName) + `,addp_instance_id=` + strconv.Quote(s.InstanceID) + `,addp_runtime_role=` + strconv.Quote(s.Role) + `,instance=` + strconv.Quote(s.Instance) + `}`
}

func (s ProcessScope) identitySelector() string {
	return strings.TrimSuffix(s.selector("addp_process_identity_info"), "}") + `,module_name=` + strconv.Quote(s.ModuleName) + `,runtime_instance_id=` + strconv.Quote(s.InstanceID) + `,runtime_role=` + strconv.Quote(s.Role) + `,schema_version=` + strconv.Quote(models.ProcessMetricsSchema) + `,operating_system=~"darwin|linux"}`
}

func (s ProcessScope) identityGuard(offset string) string {
	identity, anyIdentity, start := s.identitySelector()+offset, s.selector("addp_process_identity_info")+offset, s.selector("process_start_time_seconds")+offset
	expected := strconv.FormatFloat(float64(s.StartedAt.Unix())+float64(s.StartedAt.Nanosecond())/1e9, 'f', 9, 64)
	return ` and (count(` + anyIdentity + `)==1) and (count(` + identity + `)==1) and (max(` + identity + `)==1) and (count(` + start + `)==1)` +
		` and (abs(max(` + start + `)-` + expected + `)<=0.000001) and (max(timestamp(` + identity + `))==max(timestamp(` + start + `)))`
}

func (s ProcessScope) expression(p Plan) (string, error) {
	if s.Validate() != nil || p.Trend || len(p.Metrics) != len(processDefinitions) {
		return "", ErrInvalid
	}
	info, start, up := s.identitySelector(), s.selector("process_start_time_seconds"), s.selector("up")
	guard := s.identityGuard("") + ` and (count(` + up + `)==1) and (max(` + up + `)==1) and (max(timestamp(` + up + `))==max(timestamp(` + info + `)))`
	parts := []string{}
	for _, d := range p.Metrics {
		var raw, value, stamp string
		switch d.Key {
		case "process.cpu.core_equivalents":
			raw = s.selector("process_cpu_seconds_total")
			previous := raw + ` offset 1m`
			rate := `rate(` + raw + `[1m])`
			eligible := `(` + rate + ` and (count_over_time(` + raw + `[1m])>=4) and (resets(` + raw + `[1m])==0)` +
				` and (` + raw + `>=` + previous + `) and (timestamp(` + previous + `)>=time()-76) and (timestamp(` + raw + `)>=time()-16) and (` + rate + `>=0))`
			guarded := guard + s.identityGuard(" offset 1m") + ` and (count(` + previous + `)==1) and (count(` + eligible + `)==1)` +
				` and (max(timestamp(` + previous + `))==max(timestamp(` + start + ` offset 1m)))`
			value = `(max(` + eligible + `)` + guarded + `)`
			stamp = `(max(timestamp(` + raw + `))` + guarded + `)`
		case "process.memory.resident_bytes":
			raw = s.selector("process_resident_memory_bytes")
			value = `(max(` + raw + `)` + guard + `)`
			stamp = `(max(timestamp(` + raw + `))` + guard + `)`
		case "process.uptime_seconds":
			raw = start
			value = `(time()-max(` + start + `)` + guard + `)`
			stamp = `(max(timestamp(` + start + `))` + guard + `)`
		default:
			return "", ErrInvalid
		}
		metricGuard := ` and (count(` + raw + `)==1) and (max(timestamp(` + raw + `))==max(timestamp(` + info + `)))`
		for _, component := range []struct{ key, value string }{{"value", value}, {"sampled_at", stamp}} {
			parts = append(parts, fmt.Sprintf(`label_replace(label_replace((%s%s),"addp_metric",%s,"",""),"addp_component",%s,"","")`, component.value, metricGuard, strconv.Quote(d.Key), strconv.Quote(component.key)))
		}
	}
	return strings.Join(parts, " or "), nil
}

func (s ProcessScope) collectionExpression() string {
	up := s.selector("up")
	parts := []string{}
	for _, signal := range []struct{ key, value string }{
		{"up_count", `count(` + up + `) or vector(0)`}, {"up", `max(` + up + `)`}, {"sampled_at", `max(timestamp(` + up + `))`},
		{"identity_valid", `(vector(1)` + s.identityGuard("") + ` and (max(timestamp(` + s.identitySelector() + `))==max(timestamp(` + up + `)))) or vector(0)`},
	} {
		parts = append(parts, `label_replace((`+signal.value+`),"signal",`+strconv.Quote(signal.key)+`,"","")`)
	}
	return strings.Join(parts, " or ")
}

// ProcessSummaries reuses the node transport, batch splitter and value/time
// normalizer. Only the process identity and its fixed metric formulas differ.
func (c *Client) ProcessSummaries(ctx context.Context, scopes []ProcessScope, at time.Time, b Budget) (map[uint]Summary, error) {
	p, err := NewProcessSummaryPlan(len(scopes), at, b)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(b.TimeoutSeconds)*time.Second)
	defer cancel()
	ids := map[string]bool{}
	metricParts, collectionParts := []string{}, []string{}
	for _, scope := range scopes {
		id := strconv.FormatUint(uint64(scope.ID), 10)
		if ids[id] {
			return nil, ErrInvalid
		}
		ids[id] = true
		expression, err := scope.expression(p)
		if err != nil {
			return nil, err
		}
		label := func(part string) string {
			return `label_replace((` + part + `),"addp_summary_process",` + strconv.Quote(id) + `,"","")`
		}
		metricParts = append(metricParts, label(expression))
		collectionParts = append(collectionParts, label(scope.collectionExpression()))
	}
	data, err := c.readSummaryGroups(ctx, metricParts, at, b, ids, 2*len(p.Metrics), "addp_summary_process")
	if err != nil {
		return nil, err
	}
	evidence, err := c.readSummaryGroups(ctx, collectionParts, at, b, ids, 4, "addp_summary_process")
	if err != nil {
		return nil, err
	}
	result := map[uint]Summary{}
	for _, scope := range scopes {
		id := strconv.FormatUint(uint64(scope.ID), 10)
		series, err := normalize(data[id], p, b)
		if err != nil {
			return nil, err
		}
		collection, err := normalizeProcessCollection(evidence[id], at)
		if err != nil {
			return nil, err
		}
		if collection.State != "collecting" {
			series = Empty(p, "no_data")
		}
		result[scope.ID] = Summary{Series: series, Collection: collection}
	}
	return result, nil
}
