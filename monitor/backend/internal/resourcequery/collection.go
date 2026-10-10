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
	Network    string     `json:"network" enums:"available,failed,not_collected,unknown"`
}

func (c *Client) Collection(ctx context.Context, scope Scope, at time.Time, budget Budget) (Collection, error) {
	result := Collection{State: "no_sample", Filesystem: "unknown", Network: "unknown"}
	if scope.Validate() != nil || budget.Validate() != nil || at.IsZero() || at.Nanosecond() != 0 {
		return result, ErrInvalid
	}
	if c == nil {
		return result, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(budget.TimeoutSeconds)*time.Second)
	defer cancel()
	data, err := c.read(ctx, "/api/v1/query", url.Values{"query": {collectionExpression(scope)}, "time": {at.Format(time.RFC3339)}, "timeout": {strconv.Itoa(budget.TimeoutSeconds) + "s"}, "lookback_delta": {strconv.FormatInt(LookbackSeconds, 10) + "s"}})
	if err != nil {
		return result, err
	}
	return normalizeCollection(data, at)
}

func collectionExpression(scope Scope) string {
	up := scope.selector("up")
	expressions := []struct{ key, expression string }{
		{"up_count", "count(" + up + ") or vector(0)"},
		{"up", "max(" + up + ")"},
		{"sampled_at", "max(timestamp(" + up + "))"},
	}
	for _, collector := range []struct{ key, name string }{{"filesystem", "filesystem"}, {"network", "netdev"}} {
		selector := strings.TrimSuffix(scope.selector("node_scrape_collector_success"), "}") + `,collector=` + strconv.Quote(collector.name) + `}`
		expressions = append(expressions, []struct{ key, expression string }{
			{collector.key + "_count", "count(" + selector + ") or vector(0)"},
			{collector.key, "max(" + selector + ")"},
			{collector.key + "_sampled_at", "max(timestamp(" + selector + "))"},
		}...)
	}
	parts := make([]string, 0, len(expressions))
	for _, v := range expressions {
		parts = append(parts, `label_replace((`+v.expression+`),"signal",`+strconv.Quote(v.key)+`,"","")`)
	}
	return strings.Join(parts, " or ")
}

func normalizeCollection(data envelope, at time.Time) (Collection, error) {
	result := Collection{State: "no_sample", Filesystem: "unknown", Network: "unknown"}
	if data.Data.ResultType != "vector" || data.Data.Result == nil || len(data.Data.Result) > 9 {
		return result, ErrUnavailable
	}
	values := map[string]float64{}
	allowed := map[string]bool{"up_count": true, "up": true, "sampled_at": true, "filesystem_count": true, "filesystem": true, "filesystem_sampled_at": true, "network_count": true, "network": true, "network_sampled_at": true}
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
	var err error
	result.Filesystem, err = collectorEvidence(values, "filesystem", stamp)
	if err != nil {
		return result, err
	}
	result.Network, err = collectorEvidence(values, "network", stamp)
	return result, err
}

func collectorEvidence(values map[string]float64, key string, stamp float64) (string, error) {
	count, known := values[key+"_count"]
	if !known || math.Trunc(count) != count {
		return "unknown", ErrUnavailable
	}
	if count == 0 {
		return "not_collected", nil
	}
	if count != 1 {
		return "unknown", nil
	}
	success, known := values[key]
	collectorStamp, sameScrape := values[key+"_sampled_at"]
	if !known || (success != 0 && success != 1) || !sameScrape || collectorStamp != stamp {
		return "unknown", nil
	}
	if success == 1 {
		return "available", nil
	}
	return "failed", nil
}
