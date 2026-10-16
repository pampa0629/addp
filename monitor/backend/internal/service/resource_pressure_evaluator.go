package service

import (
	"math"
	"time"

	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
)

type resourcePressureResult struct {
	Verdict                  PlatformAlertVerdict
	SampledFrom, SampledTo   time.Time
	Samples                  int
	Minimum, Maximum, Latest float64
}

func defaultResourcePressurePolicy(key string) models.ResourcePressurePolicy {
	if key == "node.cpu.busy_percent" {
		return models.ResourcePressurePolicy{TriggerPercent: 90, RecoveryPercent: 80, TriggerSeconds: 300, RecoverySeconds: 120}
	}
	return models.ResourcePressurePolicy{TriggerPercent: 80, RecoveryPercent: 75, TriggerSeconds: 300, RecoverySeconds: 120}
}

// evaluateResourcePressure checks evidence, not scheduler elapsed time. The
// caller must resolve one current source and supply the common validity boundary
// of its rule, node/target versions and uninterrupted collection lifecycle.
// Unknown and paused deliberately leave an existing incident unresolved.
func evaluateResourcePressure(metric, uptime resourcequery.Series, validSince, at time.Time, enabled, active bool, policy models.ResourcePressurePolicy) resourcePressureResult {
	result := resourcePressureResult{Verdict: PlatformAlertUnknown}
	if !policy.Valid() {
		return result
	}

	if !enabled {
		result.Verdict = PlatformAlertPaused
		return result
	}
	trigger, recovery, window := policy.TriggerPercent, policy.RecoveryPercent, 0
	switch metric.MetricKey {
	case "node.cpu.busy_percent":
		window = 60
	case "node.memory.used_percent":
		// Instantaneous memory ratio has no calculation window.
	default:
		return result
	}
	const maxPoints = 22 // Five minutes plus one 15s boundary slot.
	if at.IsZero() || at.Nanosecond() != 0 || validSince.IsZero() || validSince.After(at) ||
		metric.Unit != "percent" || metric.WindowSeconds != window || len(metric.Dimensions) != 0 ||
		uptime.MetricKey != "node.uptime_seconds" || uptime.Unit != "seconds" || uptime.WindowSeconds != 0 || len(uptime.Dimensions) != 0 ||
		len(metric.Points) == 0 || len(metric.Points) > maxPoints || len(metric.Points) != len(uptime.Points) {
		return result
	}
	duration := time.Duration(policy.TriggerSeconds) * time.Second
	if active {
		duration = time.Duration(policy.RecoverySeconds) * time.Second
	}
	step := time.Duration(resourcequery.MinimumStepSeconds) * time.Second
	// Same tolerance as the catalog's CPU full-window source witness.
	maxGap := step + time.Second
	var newest, previous time.Time
	var boot float64
	matched := true
	for i := len(metric.Points) - 1; i >= 0; i-- {
		point, witness := metric.Points[i], uptime.Points[i]
		expected := at.Add(-time.Duration(len(metric.Points)-1-i) * step)
		if !point.EvaluatedAt.Equal(expected) || !witness.EvaluatedAt.Equal(expected) ||
			!validPressurePoint(point) || !validPressurePoint(witness) ||
			!point.SampledAt.Equal(*witness.SampledAt) ||
			expected.Sub(*point.SampledAt) < 0 || expected.Sub(*point.SampledAt) > maxGap ||
			point.SampledAt.Add(-time.Duration(window)*time.Second).Before(validSince) ||
			*point.Value < 0 || *point.Value > 100 || *witness.Value < 0 {
			return result
		}
		// The uptime catalog formula uses evaluation time, not scrape time.
		// Both components come from the same response and source scrape.
		currentBoot := float64(expected.Unix()) - *witness.Value
		if currentBoot <= 0 || currentBoot > float64(point.SampledAt.Unix())-float64(window) {
			return result
		}
		if newest.IsZero() {
			newest, boot = *point.SampledAt, currentBoot
			result.SampledTo, result.Latest = newest, *point.Value
			result.Minimum, result.Maximum = *point.Value, *point.Value
		} else if gap := previous.Sub(*point.SampledAt); gap <= 0 || gap > maxGap || currentBoot != boot {
			return result
		}
		previous = *point.SampledAt
		result.SampledFrom = previous
		result.Samples++
		result.Minimum = math.Min(result.Minimum, *point.Value)
		result.Maximum = math.Max(result.Maximum, *point.Value)
		if active {
			matched = matched && *point.Value < recovery
		} else {
			matched = matched && *point.Value > trigger
		}
		if newest.Sub(previous) >= duration {
			if (active && matched) || (!active && !matched) {
				result.Verdict = PlatformAlertNormal
				return result
			}
			result.Verdict = PlatformAlertRisk
			return result
		}
	}
	return result
}

func validPressurePoint(point resourcequery.Point) bool {
	return point.DataState == "valid" && point.SampledAt != nil && !point.SampledAt.IsZero() &&
		point.Value != nil && !math.IsNaN(*point.Value) && !math.IsInf(*point.Value, 0)
}
