package service

import (
	"math"
	"testing"
	"time"

	"github.com/addp/monitor/internal/resourcequery"
)

func pressureWindow(key string, value float64, at time.Time) (resourcequery.Series, resourcequery.Series) {
	metric := resourcequery.Series{MetricKey: key, Unit: "percent"}
	if key == "node.cpu.busy_percent" {
		metric.WindowSeconds = 60
	}
	uptime := resourcequery.Series{MetricKey: "node.uptime_seconds", Unit: "seconds"}
	for i := 0; i < 22; i++ {
		evaluated := at.Add(time.Duration(i-21) * 15 * time.Second)
		sampled, v, age := evaluated.Add(-3*time.Second), value, float64(3600+i*15)
		metric.Points = append(metric.Points, resourcequery.Point{EvaluatedAt: evaluated, SampledAt: &sampled, Value: &v, DataState: "valid"})
		uptime.Points = append(uptime.Points, resourcequery.Point{EvaluatedAt: evaluated, SampledAt: &sampled, Value: &age, DataState: "valid"})
	}
	return metric, uptime
}

func TestResourcePressureThresholdsAndHysteresis(t *testing.T) {
	at := time.Date(2026, 10, 16, 0, 10, 0, 0, time.UTC)
	for _, c := range []struct {
		name, key string
		value     float64
		active    bool
		want      PlatformAlertVerdict
	}{
		{"CPU opens", "node.cpu.busy_percent", 91, false, PlatformAlertRisk},
		{"CPU trigger equality", "node.cpu.busy_percent", 90, false, PlatformAlertNormal},
		{"CPU recovers", "node.cpu.busy_percent", 79, true, PlatformAlertNormal},
		{"CPU recovery equality", "node.cpu.busy_percent", 80, true, PlatformAlertRisk},
		{"memory opens", "node.memory.used_percent", 81, false, PlatformAlertRisk},
		{"memory trigger equality", "node.memory.used_percent", 80, false, PlatformAlertNormal},
		{"memory recovers", "node.memory.used_percent", 74, true, PlatformAlertNormal},
		{"memory recovery equality", "node.memory.used_percent", 75, true, PlatformAlertRisk},
		{"memory dead band active", "node.memory.used_percent", 78, true, PlatformAlertRisk},
		{"memory dead band inactive", "node.memory.used_percent", 78, false, PlatformAlertNormal},
		{"zero recovers", "node.memory.used_percent", 0, true, PlatformAlertNormal},
	} {
		t.Run(c.name, func(t *testing.T) {
			metric, uptime := pressureWindow(c.key, c.value, at)
			if got := pressureVerdict(metric, uptime, at.Add(-time.Hour), at, true, c.active); got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestResourcePressureRejectsIncompleteEvidence(t *testing.T) {
	at := time.Date(2026, 10, 16, 0, 10, 0, 0, time.UTC)
	for _, active := range []bool{false, true} {
		for _, c := range []struct {
			name string
			edit func(*resourcequery.Series, *resourcequery.Series)
		}{
			{"missing", func(m, _ *resourcequery.Series) { m.Points[18].Value = nil }},
			{"missing timestamp", func(m, _ *resourcequery.Series) { m.Points[18].SampledAt = nil }},
			{"stale", func(m, _ *resourcequery.Series) { m.Points[18].DataState = "stale" }},
			{"NaN", func(m, _ *resourcequery.Series) { *m.Points[18].Value = math.NaN() }},
			{"infinity", func(m, _ *resourcequery.Series) { *m.Points[18].Value = math.Inf(1) }},
			{"out of range", func(m, _ *resourcequery.Series) { *m.Points[18].Value = 101 }},
			{"duplicate lookback", func(m, u *resourcequery.Series) {
				m.Points[18].SampledAt = m.Points[17].SampledAt
				u.Points[18].SampledAt = m.Points[17].SampledAt
			}},
			{"sampling gap", func(m, u *resourcequery.Series) {
				stamp := m.Points[18].SampledAt.Add(-2 * time.Second)
				m.Points[18].SampledAt, u.Points[18].SampledAt = &stamp, &stamp
			}},
			{"future", func(m, u *resourcequery.Series) {
				stamp := m.Points[18].EvaluatedAt.Add(time.Second)
				m.Points[18].SampledAt, u.Points[18].SampledAt = &stamp, &stamp
			}},
			{"old latest sample", func(m, u *resourcequery.Series) {
				stamp := at.Add(-17 * time.Second)
				m.Points[21].SampledAt, u.Points[21].SampledAt = &stamp, &stamp
			}},
			{"wrong scrape boot witness", func(_, u *resourcequery.Series) {
				stamp := u.Points[18].SampledAt.Add(-time.Second)
				u.Points[18].SampledAt = &stamp
			}},
			{"reboot", func(_, u *resourcequery.Series) { *u.Points[18].Value -= 120 }},
			{"boot witness absent", func(_, u *resourcequery.Series) { u.Points[18].DataState = "no_data" }},
			{"invalid boot value", func(_, u *resourcequery.Series) { *u.Points[18].Value = math.Inf(1) }},
			{"grid gap", func(m, _ *resourcequery.Series) { m.Points[18].EvaluatedAt = m.Points[17].EvaluatedAt }},
			{"short window", func(m, u *resourcequery.Series) { m.Points, u.Points = m.Points[15:], u.Points[15:] }},
			{"unbounded input", func(m, u *resourcequery.Series) {
				m.Points = append(m.Points, m.Points[21])
				u.Points = append(u.Points, u.Points[21])
			}},
			{"wrong unit", func(m, _ *resourcequery.Series) { m.Unit = "bytes" }},
			{"grouped source", func(m, _ *resourcequery.Series) { m.Dimensions = resourcequery.Dimensions{"device": "sda"} }},
			{"wrong catalog window", func(m, _ *resourcequery.Series) { m.WindowSeconds = 0 }},
		} {
			t.Run(c.name+"/active="+map[bool]string{false: "false", true: "true"}[active], func(t *testing.T) {
				value := 99.0
				if active {
					value = 0
				}
				metric, uptime := pressureWindow("node.cpu.busy_percent", value, at)
				c.edit(&metric, &uptime)
				if got := pressureVerdict(metric, uptime, at.Add(-time.Hour), at, true, active); got != PlatformAlertUnknown {
					t.Fatalf("unsafe verdict: %s", got)
				}
			})
		}
	}
}

func TestResourcePressureUsesRawTimeAndBoundedWindow(t *testing.T) {
	at := time.Date(2026, 10, 16, 0, 10, 0, 0, time.UTC)
	for _, active := range []bool{false, true} {
		value, points := 99.0, 21
		if active {
			value, points = 0, 9
		}
		metric, uptime := pressureWindow("node.memory.used_percent", value, at)
		metric.Points, uptime.Points = metric.Points[22-points:], uptime.Points[22-points:]
		want := PlatformAlertRisk
		if active {
			want = PlatformAlertNormal
		}
		if got := pressureVerdict(metric, uptime, at.Add(-time.Hour), at, true, active); got != want {
			t.Fatalf("exact complete window active=%t: %s", active, got)
		}
		// The evaluation grid spans exactly the duration, but real samples
		// span one second less. This must not count as continuous evidence.
		stamp := metric.Points[0].SampledAt.Add(time.Second)
		metric.Points[0].SampledAt, uptime.Points[0].SampledAt = &stamp, &stamp
		if got := pressureVerdict(metric, uptime, at.Add(-time.Hour), at, true, active); got != PlatformAlertUnknown {
			t.Fatalf("grid substituted for raw elapsed time active=%t: %s", active, got)
		}
	}
	metric, uptime := pressureWindow("node.memory.used_percent", 99, at)
	// A consistent 16s lag fits the established grid tolerance. A 17s lag
	// was separately rejected above; timestamps are never advanced here.
	for i := range metric.Points {
		stamp := metric.Points[i].EvaluatedAt.Add(-16 * time.Second)
		metric.Points[i].SampledAt, uptime.Points[i].SampledAt = &stamp, &stamp
	}
	if got := pressureVerdict(metric, uptime, at.Add(-time.Hour), at, true, false); got != PlatformAlertRisk {
		t.Fatalf("source within cadence tolerance rejected: %s", got)
	}
}

func TestResourcePressureLifecycleBoundaries(t *testing.T) {
	at := time.Date(2026, 10, 16, 0, 10, 0, 0, time.UTC)
	metric, uptime := pressureWindow("node.cpu.busy_percent", 99, at)
	// A version/collection change invalidates CPU's pre-boundary rate window,
	// even when every displayed point itself is after the boundary.
	since := at.Add(-6 * time.Minute)
	if got := pressureVerdict(metric, uptime, since, at, true, false); got != PlatformAlertUnknown {
		t.Fatalf("CPU window crossed lifecycle boundary: %s", got)
	}
	if got := pressureVerdict(metric, uptime, time.Time{}, at, true, false); got != PlatformAlertUnknown {
		t.Fatalf("absent lifecycle evidence accepted: %s", got)
	}
	if got := pressureVerdict(resourcequery.Series{}, resourcequery.Series{}, time.Time{}, at, false, true); got != PlatformAlertPaused {
		t.Fatalf("disabled source: %s", got)
	}
	// The recovery window uses only its own complete trailing evidence.
	metric, uptime = pressureWindow("node.memory.used_percent", 0, at)
	metric.Points[0].Value = nil
	if got := pressureVerdict(metric, uptime, at.Add(-3*time.Minute), at, true, true); got != PlatformAlertNormal {
		t.Fatalf("old unrelated gap blocked valid recovery: %s", got)
	}
	// A brief threshold excursion cannot open or recover an incident.
	metric, uptime = pressureWindow("node.memory.used_percent", 81, at)
	*metric.Points[18].Value = 80
	if got := pressureVerdict(metric, uptime, at.Add(-time.Hour), at, true, false); got != PlatformAlertNormal {
		t.Fatalf("non-continuous trigger: %s", got)
	}
	metric, uptime = pressureWindow("node.memory.used_percent", 74, at)
	*metric.Points[18].Value = 75
	if got := pressureVerdict(metric, uptime, at.Add(-time.Hour), at, true, true); got != PlatformAlertRisk {
		t.Fatalf("non-continuous recovery: %s", got)
	}
}

func pressureVerdict(metric, uptime resourcequery.Series, since, at time.Time, enabled, active bool) PlatformAlertVerdict {
	return evaluateResourcePressure(metric, uptime, since, at, enabled, active, defaultResourcePressurePolicy(metric.MetricKey)).Verdict
}

func TestResourcePressureConfiguredPolicyAndImmutableWindowEvidence(t *testing.T) {
	at := time.Date(2026, 10, 16, 0, 10, 0, 0, time.UTC)
	metric, uptime := pressureWindow("node.memory.used_percent", 70, at)
	policy := defaultResourcePressurePolicy(metric.MetricKey)
	policy.TriggerPercent, policy.RecoveryPercent = 65, 60
	policy.TriggerSeconds = 30
	got := evaluateResourcePressure(metric, uptime, at.Add(-time.Hour), at, true, false, policy)
	if got.Verdict != PlatformAlertRisk || got.Samples != 3 || got.SampledTo.Sub(got.SampledFrom) != 30*time.Second || got.Minimum != 70 || got.Maximum != 70 || got.Latest != 70 {
		t.Fatalf("configured evidence: %#v", got)
	}
	policy.RecoveryPercent = policy.TriggerPercent
	if got := evaluateResourcePressure(metric, uptime, at.Add(-time.Hour), at, true, false, policy); got.Verdict != PlatformAlertUnknown {
		t.Fatal("invalid hysteresis accepted", got)
	}
}
