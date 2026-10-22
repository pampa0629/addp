package service

import (
	"math"
	"time"

	"github.com/addp/monitor/internal/resourcequery"
)

// One continuous-window evaluator; callers validate their fixed metric and policy.
func evaluateThresholdWindow(series resourcequery.Series, trigger, recovery float64, triggerSeconds, recoverySeconds int, since, at time.Time, active bool, freshness time.Duration) resourcePressureResult {
	result := resourcePressureResult{Verdict: PlatformAlertUnknown}
	seconds := triggerSeconds
	if active {
		seconds = recoverySeconds
	}
	duration := time.Duration(seconds) * time.Second
	if since.IsZero() || at.IsZero() || since.After(at) {
		return result
	}
	expected := at
	var previousSample time.Time
	matched := true
	result.Minimum, result.Maximum = math.Inf(1), math.Inf(-1)
	for i := len(series.Points) - 1; i >= 0; i-- {
		point := series.Points[i]
		if !point.EvaluatedAt.Equal(expected) || !validThresholdPoint(point) || point.EvaluatedAt.Sub(*point.SampledAt) > freshness || point.SampledAt.Add(-time.Duration(series.WindowSeconds)*time.Second).Before(since) || (!previousSample.IsZero() && (!previousSample.After(*point.SampledAt) || previousSample.Sub(*point.SampledAt) > 16*time.Second)) {
			return resourcePressureResult{Verdict: PlatformAlertUnknown}
		}
		v := *point.Value
		if series.Unit == "percent" && v > 100 {
			return resourcePressureResult{Verdict: PlatformAlertUnknown}
		}
		if result.Samples == 0 {
			result.SampledTo = *point.SampledAt
			result.Latest = v
		}
		result.SampledFrom = *point.SampledAt
		result.Samples++
		result.Minimum = math.Min(result.Minimum, v)
		result.Maximum = math.Max(result.Maximum, v)
		if active {
			matched = matched && v < recovery
		} else {
			matched = matched && v > trigger
		}
		if result.SampledTo.Sub(result.SampledFrom) >= duration {
			result.Verdict = PlatformAlertRisk
			if (active && matched) || (!active && !matched) {
				result.Verdict = PlatformAlertNormal
			}
			return result
		}
		previousSample = *point.SampledAt
		expected = expected.Add(-15 * time.Second)
	}
	return resourcePressureResult{Verdict: PlatformAlertUnknown}
}

func validThresholdPoint(point resourcequery.Point) bool {
	return point.DataState == "valid" && point.Value != nil && !math.IsNaN(*point.Value) && !math.IsInf(*point.Value, 0) && *point.Value >= 0 && point.SampledAt != nil && !point.SampledAt.After(point.EvaluatedAt) && point.EvaluatedAt.Sub(*point.SampledAt) <= 60*time.Second
}
