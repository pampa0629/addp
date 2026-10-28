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
	BlockCapacity string     `json:"block_capacity,omitempty" enums:"available,empty,unsupported,failed,not_collected,stale,unknown"`
	State         string     `json:"state"`
	SampledAt     *time.Time `json:"sampled_at"`
	Filesystem    string     `json:"filesystem"`
	Network       string     `json:"network" enums:"available,failed,not_collected,unknown"`
}

func (c *Client) Collection(ctx context.Context, scope Scope, at time.Time, budget Budget, block bool) (Collection, error) {
	result := Collection{State: "no_sample", Filesystem: "unknown", Network: "unknown"}
	if scope.Validate() != nil || budget.Validate() != nil || at.IsZero() || at.Nanosecond() != 0 {
		return result, ErrInvalid
	}
	if c == nil {
		return result, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(budget.TimeoutSeconds)*time.Second)
	defer cancel()
	data, err := c.readForm(ctx, "/api/v1/query", url.Values{"query": {collectionExpression(scope, block)}, "time": {at.Format(time.RFC3339)}, "timeout": {strconv.Itoa(budget.TimeoutSeconds) + "s"}, "lookback_delta": {strconv.FormatInt(LookbackSeconds, 10) + "s"}})
	if err != nil {
		return result, err
	}
	return normalizeCollection(data, at)
}

func collectionExpression(scope Scope, block bool) string {
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
	if block {
		_, sampled, guard := blockCapacityFrame(scope)
		bc := scope.selector("addp_block_capacity_success")
		expressions = append(expressions, []struct{ key, expression string }{
			{"block_count", "count(" + bc + ") or vector(0)"},
			{"block_ready", "(" + sampled + guard + ")"},
			{"block_devices", "(max(" + scope.selector("addp_block_capacity_device_count") + ")" + guard + ")"},
			{"block_supported", "max(" + scope.selector("addp_block_capacity_supported") + ")"},
			{"block_time", "max(" + scope.selector("addp_block_capacity_sample_time_seconds") + ")"},
			{"block_collector_count", "count(" + strings.TrimSuffix(scope.selector("node_scrape_collector_success"), "}") + `,collector="textfile"}) or vector(0)`},
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
	allowed := map[string]bool{"up_count": true, "up": true, "sampled_at": true, "filesystem_count": true, "filesystem": true, "filesystem_sampled_at": true, "network_count": true, "network": true, "network_sampled_at": true, "block_count": true, "block_ready": true, "block_devices": true, "block_supported": true, "block_time": true, "block_collector_count": true}
	values, err := normalizeSignals(data, at, allowed)
	if err != nil {
		return result, err
	}
	result, err = normalizeUpEvidence(values, at)
	if err != nil || result.State != "collecting" {
		return result, err
	}
	stamp := values["sampled_at"]
	result.Filesystem, err = collectorEvidence(values, "filesystem", stamp)
	if err != nil {
		return result, err
	}
	result.Network, err = collectorEvidence(values, "network", stamp)
	if count, present := values["block_count"]; present {
		result.BlockCapacity = "failed"
		if count == 0 {
			if collectors, known := values["block_collector_count"]; known && collectors == 0 {
				result.BlockCapacity = "not_collected"
			}
		} else if count == 1 {
			stamp, hasTime := values["block_time"]
			if hasTime && stamp == math.Trunc(stamp) && stamp <= float64(at.Unix()) {
				if float64(at.Unix())-stamp > float64(FreshnessSeconds) {
					result.BlockCapacity = "stale"
				} else if supported, known := values["block_supported"]; known {
					if supported == 0 {
						result.BlockCapacity = "unsupported"
					} else if supported == 1 {
						ready, valid := values["block_ready"]
						devices, complete := values["block_devices"]
						if valid && ready == stamp && complete && devices == math.Trunc(devices) && devices >= 0 && devices <= 100 {
							result.BlockCapacity = "available"
							if devices == 0 {
								result.BlockCapacity = "empty"
							}
						}
					}
				}
			}
		} else if count != math.Trunc(count) {
			return result, ErrUnavailable
		}
	}
	return result, err
}

func normalizeSignals(data envelope, at time.Time, allowed map[string]bool) (map[string]float64, error) {
	if data.Data.ResultType != "vector" || data.Data.Result == nil || len(data.Data.Result) > len(allowed) {
		return nil, ErrUnavailable
	}
	values := map[string]float64{}
	for _, row := range data.Data.Result {
		key := row.Metric["signal"]
		_, duplicate := values[key]
		if len(row.Metric) != 1 || !allowed[key] || duplicate || row.Value == nil || row.Values != nil || len(row.Histogram) > 0 || len(row.Histograms) > 0 {
			return nil, ErrUnavailable
		}
		var evaluated float64
		var raw string
		if json.Unmarshal(row.Value[0], &evaluated) != nil || evaluated != float64(at.Unix()) || json.Unmarshal(row.Value[1], &raw) != nil {
			return nil, ErrUnavailable
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return nil, ErrUnavailable
		}
		values[key] = v
	}
	return values, nil
}

func normalizeProcessCollection(data envelope, at time.Time) (Collection, error) {
	values, err := normalizeSignals(data, at, map[string]bool{"up_count": true, "up": true, "sampled_at": true, "identity_valid": true})
	if err != nil {
		return Collection{}, err
	}
	result, err := normalizeUpEvidence(values, at)
	if err != nil || result.State != "collecting" {
		return result, err
	}
	identity, ok := values["identity_valid"]
	if !ok || (identity != 0 && identity != 1) {
		return Collection{}, ErrUnavailable
	}
	if identity == 0 {
		result.State = "identity_mismatch"
	}
	return result, nil
}

func normalizeUpEvidence(values map[string]float64, at time.Time) (Collection, error) {
	result := Collection{State: "no_sample", Filesystem: "unknown", Network: "unknown"}
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
	return result, nil
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
