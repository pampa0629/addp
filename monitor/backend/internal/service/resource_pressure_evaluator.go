package service

import (
	"math"
	"time"

	"github.com/addp/monitor/internal/resourcequery"
)

// evaluateResourcePressure checks evidence, not scheduler elapsed time. The
// caller must resolve one current source and supply the common validity boundary
// of its rule, node/target versions and uninterrupted collection lifecycle.
// Unknown and paused deliberately leave an existing incident unresolved.
func evaluateResourcePressure(metric, uptime resourcequery.Series, validSince, at time.Time, enabled, active bool) PlatformAlertVerdict {
	if !enabled {
		return PlatformAlertPaused
	}
	trigger, recovery, window := 0.0, 0.0, 0
	switch metric.MetricKey {
	case "node.cpu.busy_percent":
		trigger, recovery, window = 90, 80, 60
	case "node.memory.used_percent":
		trigger, recovery = 80, 75
	default:
		return PlatformAlertUnknown
	}
	const maxPoints = 22 // Five minutes plus one 15s boundary slot.
	if at.IsZero() || at.Nanosecond() != 0 || validSince.IsZero() || validSince.After(at) ||
		metric.Unit != "percent" || metric.WindowSeconds != window || len(metric.Dimensions) != 0 ||
		uptime.MetricKey != "node.uptime_seconds" || uptime.Unit != "seconds" || uptime.WindowSeconds != 0 || len(uptime.Dimensions) != 0 ||
		len(metric.Points) == 0 || len(metric.Points) > maxPoints || len(metric.Points) != len(uptime.Points) {
		return PlatformAlertUnknown
	}
	duration := 5 * time.Minute
	if active {
		duration = 2 * time.Minute
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
			return PlatformAlertUnknown
		}
		// The uptime catalog formula uses evaluation time, not scrape time.
		// Both components come from the same response and source scrape.
		currentBoot := float64(expected.Unix()) - *witness.Value
		if currentBoot <= 0 || currentBoot > float64(point.SampledAt.Unix())-float64(window) {
			return PlatformAlertUnknown
		}
		if newest.IsZero() {
			newest, boot = *point.SampledAt, currentBoot
		} else if gap := previous.Sub(*point.SampledAt); gap <= 0 || gap > maxGap || currentBoot != boot {
			return PlatformAlertUnknown
		}
		previous = *point.SampledAt
		if active {
			matched = matched && *point.Value < recovery
		} else {
			matched = matched && *point.Value > trigger
		}
		if newest.Sub(previous) >= duration {
			if (active && matched) || (!active && !matched) {
				return PlatformAlertNormal
			}
			return PlatformAlertRisk
		}
	}
	return PlatformAlertUnknown
}

func validPressurePoint(point resourcequery.Point) bool {
	return point.DataState == "valid" && point.SampledAt != nil && !point.SampledAt.IsZero() &&
		point.Value != nil && !math.IsNaN(*point.Value) && !math.IsInf(*point.Value, 0)
}
