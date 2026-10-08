package resourcequery

import (
	"context"
	"encoding/json"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Collection is current bounded collection evidence, not historical node health.
// 当前有界采集证据；不代表主机或历史时点的健康状态。
type Collection struct {
	State      string     `json:"state"`
	SampledAt  *time.Time `json:"sampled_at"`
	Filesystem string     `json:"filesystem"`
}

func (c *Client) Collection(ctx context.Context, scope Scope, at time.Time, budget Budget) (Collection, error) {
	result := Collection{State: "no_sample", Filesystem: "unknown"}
	if scope.Validate() != nil || budget.Validate() != nil || at.IsZero() || at.Nanosecond() != 0 {
		return result, ErrInvalid
	}
	if c == nil {
		return result, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(budget.TimeoutSeconds)*time.Second)
	defer cancel()
	up := scope.selector("up")
	fs := strings.TrimSuffix(scope.selector("node_scrape_collector_success"), "}") + `,collector="filesystem"}`
	expressions := []struct{ key, expression string }{
		{"up_count", "count(" + up + ") or vector(0)"},
		{"up", "max(" + up + ")"},
		{"sampled_at", "max(timestamp(" + up + "))"},
		{"filesystem_count", "count(" + fs + ") or vector(0)"},
		{"filesystem", "max(" + fs + ")"},
		{"filesystem_sampled_at", "max(timestamp(" + fs + "))"},
	}
	parts := make([]string, 0, len(expressions))
	for _, v := range expressions {
		parts = append(parts, `label_replace((`+v.expression+`),"signal",`+strconv.Quote(v.key)+`,"","")`)
	}
	data, err := c.read(ctx, "/api/v1/query", url.Values{"query": {strings.Join(parts, " or ")}, "time": {at.Format(time.RFC3339)}, "timeout": {strconv.Itoa(budget.TimeoutSeconds) + "s"}, "lookback_delta": {strconv.FormatInt(LookbackSeconds, 10) + "s"}})
	if err != nil {
		return result, err
	}
	return normalizeCollection(data, at)
}

func normalizeCollection(data envelope, at time.Time) (Collection, error) {
	result := Collection{State: "no_sample", Filesystem: "unknown"}
	if data.Data.ResultType != "vector" || data.Data.Result == nil || len(data.Data.Result) > 6 {
		return result, ErrUnavailable
	}
	values := map[string]float64{}
	allowed := map[string]bool{"up_count": true, "up": true, "sampled_at": true, "filesystem_count": true, "filesystem": true, "filesystem_sampled_at": true}
	for _, row := range data.Data.Result {
		key := row.Metric["signal"]
		_, duplicate := values[key]
		if len(row.Metric) != 1 || !allowed[key] || duplicate || row.Value == nil || row.Values != nil || len(row.Histogram) > 0 || len(row.Histograms) > 0 {
			return result, ErrUnavailable
		}
		var evaluated float64
		var raw string
		if json.Unmarshal(row.Value[0], &evaluated) != nil || evaluated != float64(at.Unix()) || json.Unmarshal(row.Value[1], &raw) != nil {
			return result, ErrUnavailable
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return result, ErrUnavailable
		}
		values[key] = v
	}
	count, hasCount := values["up_count"]
	if !hasCount || math.Trunc(count) != count {
		return result, ErrUnavailable
	}
	if count != 1 {
		return result, nil
	}
	up, hasUp := values["up"]
	stamp, hasStamp := values["sampled_at"]
	if !hasUp || !hasStamp || (up != 0 && up != 1) || stamp > float64(at.Unix()) {
		return result, ErrUnavailable
	}
	sec, fraction := math.Modf(stamp)
	sampled := time.Unix(int64(sec), int64(fraction*1e9)).UTC()
	result.SampledAt = &sampled
	if at.Sub(sampled) > time.Duration(FreshnessSeconds)*time.Second {
		result.State = "stale"
		return result, nil
	}
	if up == 0 {
		result.State = "failed"
		return result, nil
	}
	result.State = "collecting"
	fsCount, known := values["filesystem_count"]
	if !known || math.Trunc(fsCount) != fsCount {
		return result, ErrUnavailable
	}
	if fsCount == 0 {
		result.Filesystem = "not_collected"
		return result, nil
	}
	if fsCount != 1 {
		return result, nil
	}
	success, known := values["filesystem"]
	fsStamp, sameScrape := values["filesystem_sampled_at"]
	if !known || (success != 0 && success != 1) || !sameScrape || fsStamp != stamp {
		return result, nil
	}
	result.Filesystem = "failed"
	if success == 1 {
		result.Filesystem = "available"
	}
	return result, nil
}
