// Package resourcequery owns the fixed resource metric catalog and bounded
// Prometheus query protocol. It does not resolve user authorization or targets.
package resourcequery

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalid     = errors.New("invalid resource query")
	ErrBudget      = errors.New("resource query budget exceeded")
	ErrUnavailable = errors.New("resource query backend unavailable")
	ErrBusy        = errors.New("resource query concurrency exceeded")
	ErrConflict    = errors.New("resource query policy version conflict")
)

const (
	LookbackSeconds    int64 = 300
	FreshnessSeconds   int64 = 60
	MinimumStepSeconds int64 = 15
)

type Budget struct {
	MaxMetrics         int   `json:"max_metrics"`
	MaxSeries          int   `json:"max_series"`
	MaxPointsPerSeries int   `json:"max_points_per_series"`
	MaxTotalPoints     int   `json:"max_total_points"`
	MaxRangeSeconds    int64 `json:"max_range_seconds"`
	TimeoutSeconds     int   `json:"timeout_seconds"`
	SubjectConcurrency int   `json:"subject_concurrency"`
	ProcessConcurrency int   `json:"process_concurrency"`
}

func DefaultBudget() Budget { return Budget{12, 100, 1000, 20000, 7 * 86400, 5, 2, 8} }
func (b Budget) Validate() error {
	d := DefaultBudget()
	if b.MaxMetrics < 1 || b.MaxMetrics > d.MaxMetrics || b.MaxSeries < 1 || b.MaxSeries > d.MaxSeries || b.MaxPointsPerSeries < 2 || b.MaxPointsPerSeries > d.MaxPointsPerSeries || b.MaxTotalPoints < 2 || b.MaxTotalPoints > d.MaxTotalPoints || b.MaxRangeSeconds < 15 || b.MaxRangeSeconds > d.MaxRangeSeconds || b.TimeoutSeconds < 1 || b.TimeoutSeconds > d.TimeoutSeconds || b.SubjectConcurrency < 1 || b.SubjectConcurrency > d.SubjectConcurrency || b.ProcessConcurrency < 1 || b.ProcessConcurrency > d.ProcessConcurrency {
		return ErrInvalid
	}
	return nil
}

type Definition struct {
	Key           string `json:"key"`
	Unit          string `json:"unit"`
	WindowSeconds int    `json:"window_seconds"`
}

// Scalars have one series; grouped families retain their closed observation dimensions.
var definitions = []Definition{
	{"node.cpu.logical_cores", "cores", 0},
	{"node.cpu.busy_percent", "percent", 60},
	{"node.memory.total_bytes", "bytes", 0},
	{"node.memory.available_bytes", "bytes", 0},
	{"node.memory.used_percent", "percent", 0},
	{"node.load.average_1m", "load", 0},
	{"node.load.average_5m", "load", 0},
	{"node.load.average_15m", "load", 0},
	{"node.uptime_seconds", "seconds", 0},
	{"node.disk.read_bytes_per_second", "bytes_per_second", 60},
	{"node.disk.write_bytes_per_second", "bytes_per_second", 60},
	{"node.filesystem.total_bytes", "bytes", 0},
	{"node.filesystem.free_bytes", "bytes", 0},
	{"node.filesystem.available_bytes", "bytes", 0},
	{"node.filesystem.used_bytes", "bytes", 0},
	{"node.filesystem.used_percent", "percent", 0},
	{"node.filesystem.inodes_total", "inodes", 0},
	{"node.filesystem.inodes_free", "inodes", 0},
	{"node.filesystem.inodes_used", "inodes", 0},
	{"node.filesystem.inodes_used_percent", "percent", 0},
}

func Catalog() []Definition { return append([]Definition(nil), definitions...) }

type Plan struct {
	Metrics          []Definition
	Start, End       time.Time
	StepSeconds      int64
	Points           int
	Trend            bool
	Dimensions       Dimensions
	SeriesUpperBound int
	FilesystemGroups int
	DiskGroups       int
}

func NewPlan(keys []string, start, end, now time.Time, trend bool, dimensions Dimensions, b Budget) (Plan, error) {
	start, end, now = start.UTC(), end.UTC(), now.UTC()
	p := Plan{Start: start, End: end, Trend: trend, StepSeconds: MinimumStepSeconds, Points: 1}
	if b.Validate() != nil || len(keys) == 0 || dimensions.Validate() != nil {
		return p, ErrInvalid
	}
	if len(keys) > b.MaxMetrics || len(keys) > b.MaxSeries {
		return p, ErrBudget
	}
	seen := map[string]bool{}
	for _, key := range keys {
		if seen[key] {
			return p, ErrInvalid
		}
		seen[key] = true
		found := false
		for _, d := range definitions {
			if d.Key == key {
				p.Metrics = append(p.Metrics, d)
				found = true
				break
			}
		}
		if !found {
			return p, ErrInvalid
		}
	}
	scalar, filesystem, disk := 0, 0, 0
	for _, d := range p.Metrics {
		if d.Filesystem() {
			filesystem++
		} else if d.Disk() {
			disk++
		} else {
			scalar++
		}
	}
	if len(dimensions) > 0 && ((len(dimensions) == 1 && (disk == 0 || filesystem > 0)) || (len(dimensions) == 3 && (filesystem == 0 || disk > 0))) {
		return p, ErrInvalid
	}
	p.Dimensions = dimensions.Copy()
	p.SeriesUpperBound = scalar
	if filesystem+disk > 0 {
		groups := (b.MaxSeries - scalar) / (filesystem + disk)
		if len(dimensions) > 0 {
			groups = 1
		}
		if groups < 1 {
			return p, ErrBudget
		}
		if filesystem > 0 {
			p.FilesystemGroups = groups
		}
		if disk > 0 {
			p.DiskGroups = groups
		}
		p.SeriesUpperBound += groups * (filesystem + disk)
	}
	if end.IsZero() || end.Nanosecond() != 0 || end.After(now) || end.Unix() < 0 {
		return p, ErrInvalid
	}
	if !trend {
		p.Start = end
		if p.SeriesUpperBound > b.MaxTotalPoints {
			return p, ErrBudget
		}
		return p, nil
	}
	if start.IsZero() || start.Nanosecond() != 0 || start.Unix() < 0 || !start.Before(end) {
		return p, ErrInvalid
	}
	span := end.Unix() - start.Unix()
	if span > b.MaxRangeSeconds {
		return p, ErrBudget
	}
	limit := b.MaxPointsPerSeries
	if total := b.MaxTotalPoints / p.SeriesUpperBound; total < limit {
		limit = total
	}
	if limit < 2 {
		return p, ErrBudget
	}
	step := (span + int64(limit) - 2) / int64(limit-1)
	if step < MinimumStepSeconds {
		step = MinimumStepSeconds
	}
	step = (step + MinimumStepSeconds - 1) / MinimumStepSeconds * MinimumStepSeconds
	p.StepSeconds = step
	p.Points = int(span/step) + 1
	if p.Points > b.MaxPointsPerSeries || p.Points*p.SeriesUpperBound > b.MaxTotalPoints {
		return p, ErrBudget
	}
	return p, nil
}

type Scope struct{ NodeID, Instance string }

func (s Scope) Validate() error {
	id, e := uuid.Parse(s.NodeID)
	if e != nil || id == uuid.Nil || id.String() != s.NodeID || s.Instance == "" || len(s.Instance) > 512 || strings.ContainsAny(s.Instance, "\r\n") {
		return ErrInvalid
	}
	return nil
}
func (s Scope) selector(metric string) string {
	return metric + `{job="addp_nodes",addp_node_id=` + strconv.Quote(s.NodeID) + `,addp_monitor_kind="host_resources",addp_source="node_exporter",instance=` + strconv.Quote(s.Instance) + `}`
}

// Expression builds only catalog formulas. Aggregations discard all upstream
// labels; count and timestamp guards reject duplicate or mismatched evidence.
func (p Plan) Expression(s Scope) (string, error) {
	if s.Validate() != nil || len(p.Metrics) == 0 || len(p.Metrics) > 12 {
		return "", ErrInvalid
	}
	parts := make([]string, 0, len(p.Metrics)*2)
	for _, d := range p.Metrics {
		var value, stamp string
		components := []struct{ k, v string }{}
		if d.Filesystem() {
			value, stamp, presence := filesystemExpression(s, d.Key, p.Dimensions)
			components = append(components, struct{ k, v string }{"value", value}, struct{ k, v string }{"sampled_at", stamp}, struct{ k, v string }{"observed_at", presence})
		} else if d.Disk() {
			value, stamp, presence := diskExpression(s, d.Key, p.Dimensions)
			components = append(components, struct{ k, v string }{"value", value}, struct{ k, v string }{"sampled_at", stamp}, struct{ k, v string }{"observed_at", presence})
		} else {
			gauge := func(name string) (string, string) {
				raw := s.selector(name)
				guard := ` and (count(` + raw + `)==1)`
				return `(max(` + raw + `)` + guard + `)`, `(max(timestamp(` + raw + `))` + guard + `)`
			}
			switch d.Key {
			case "node.cpu.logical_cores":
				raw := s.selector("node_cpu_seconds_total")
				raw = strings.TrimSuffix(raw, "}") + `,mode="idle"}`
				guard := ` and (min(timestamp(` + raw + `)) == max(timestamp(` + raw + `)))`
				value = `(count(` + raw + `)` + guard + `)`
				stamp = `(min(timestamp(` + raw + `))` + guard + `)`
			case "node.cpu.busy_percent":
				raw := strings.TrimSuffix(s.selector("node_cpu_seconds_total"), "}") + `,mode="idle"}`
				previous := raw + ` offset 1m`
				// Integer scaling avoids reciprocal rounding of decimal step sizes.
				rate := `(round(rate(` + raw + `[1m])*1000000000000)/1000000000000)`
				// Range selectors are left-open: four samples at a 15s cadence,
				// plus a source witness at the start, prove the full minute.
				// One second tolerates the API's whole-second evaluation grid.
				eligible := `(` + rate +
					` and (count_over_time(` + raw + `[1m])>=4)` +
					` and (resets(` + raw + `[1m])==0)` +
					` and (` + raw + `>=` + previous + `)` +
					` and (timestamp(` + previous + `)>=time()-76)` +
					` and (timestamp(` + raw + `)>=time()-16)` +
					` and (` + rate + `>=0) and (` + rate + `<=1))`
				boot := s.selector("node_boot_time_seconds")
				guard := ` and (count(` + eligible + `)==count(` + raw + `))` +
					` and (count(` + raw + `)==count(` + previous + `))` +
					` and (count(count_over_time(` + raw + `[1m]))==count(` + raw + `))` +
					` and (min(timestamp(` + raw + `))==max(timestamp(` + raw + `)))` +
					` and (min(timestamp(` + previous + `))==max(timestamp(` + previous + `)))` +
					` and (count(` + boot + `)==1) and (count(` + boot + ` offset 1m)==1)` +
					` and (max(` + boot + `)==max(` + boot + ` offset 1m))` +
					` and (max(timestamp(` + boot + `))==max(timestamp(` + raw + `)))` +
					` and (max(timestamp(` + boot + ` offset 1m))==max(timestamp(` + previous + `)))`
				value = `((round(100*(1-avg(` + eligible + `))*1000000000)/1000000000)` + guard + `)`
				stamp = `(min(timestamp(` + raw + `))` + guard + `)`
			case "node.memory.total_bytes":
				value, stamp = gauge("node_memory_MemTotal_bytes")
			case "node.memory.available_bytes":
				value, stamp = gauge("node_memory_MemAvailable_bytes")
			case "node.memory.used_percent":
				total, ts := gauge("node_memory_MemTotal_bytes")
				avail, as := gauge("node_memory_MemAvailable_bytes")
				guard := ` and (` + total + ` > 0) and (` + avail + ` >= 0) and (` + avail + ` <= ` + total + `) and (` + ts + ` == ` + as + `)`
				value = `((100*(1-` + avail + `/` + total + `))` + guard + `)`
				stamp = `(` + ts + guard + `)`
			case "node.load.average_1m":
				value, stamp = gauge("node_load1")
			case "node.load.average_5m":
				value, stamp = gauge("node_load5")
			case "node.load.average_15m":
				value, stamp = gauge("node_load15")
			case "node.uptime_seconds":
				boot, ts := gauge("node_boot_time_seconds")
				value = `(time()-` + boot + `)`
				stamp = ts
			default:
				return "", ErrInvalid
			}
			components = append(components, struct{ k, v string }{"value", value}, struct{ k, v string }{"sampled_at", stamp})
		}
		for _, component := range components {
			parts = append(parts, fmt.Sprintf(`label_replace(label_replace(%s,"addp_metric",%s,"",""),"addp_component",%s,"","")`, component.v, strconv.Quote(d.Key), strconv.Quote(component.k)))
		}
	}
	return strings.Join(parts, " or "), nil
}
