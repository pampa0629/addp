package service

import (
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"math"
	"testing"
	"time"
)

func processThresholdSeries(at time.Time, value float64, key string) resourcequery.Series {
	line := httpThresholdSeries(at, value)
	line.MetricKey = key
	line.Unit = "bytes"
	line.WindowSeconds = 0
	if key == "process.cpu.core_equivalents" {
		line.Unit = "cores"
		line.WindowSeconds = 60
	}
	return line
}
func TestProcessThresholdUnitsWindowsAndMissingSamples(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	since := at.Add(-time.Hour)
	policy := models.ProcessThresholdPolicy{TriggerValue: 2, RecoveryValue: 1, TriggerSeconds: 30, RecoverySeconds: 30}
	for _, key := range []string{"process.cpu.core_equivalents", "process.memory.resident_bytes"} {
		t.Run(key, func(t *testing.T) {
			high := processThresholdSeries(at, 3, key)
			if v := evaluateProcessThreshold(high, policy, since, at, false); v.Verdict != PlatformAlertRisk || v.Samples != 3 {
				t.Fatal(v)
			}
			if v := evaluateProcessThreshold(processThresholdSeries(at, 0, key), policy, since, at, true); v.Verdict != PlatformAlertNormal {
				t.Fatal("valid zero cannot recover", v)
			}
			if v := evaluateProcessThreshold(processThresholdSeries(at, 1, key), policy, since, at, true); v.Verdict != PlatformAlertRisk {
				t.Fatal("threshold equality recovered", v)
			}
			for _, mutate := range []func(*resourcequery.Series){
				func(s *resourcequery.Series) { s.Points[20].DataState = "no_data" },
				func(s *resourcequery.Series) { s.Points[20].SampledAt = s.Points[21].SampledAt },
				func(s *resourcequery.Series) { v := at.Add(-17 * time.Second); s.Points[21].SampledAt = &v },
				func(s *resourcequery.Series) { s.Points[21].Value = nil },
				func(s *resourcequery.Series) { v := math.NaN(); s.Points[21].Value = &v },
				func(s *resourcequery.Series) { s.Points[20].EvaluatedAt = s.Points[21].EvaluatedAt },
				func(s *resourcequery.Series) { s.Unit = "percent" },
				func(s *resourcequery.Series) { s.WindowSeconds = 15 },
			} {
				bad := processThresholdSeries(at, 0, key)
				mutate(&bad)
				if v := evaluateProcessThreshold(bad, policy, since, at, true); v.Verdict != PlatformAlertUnknown {
					t.Fatal("invalid evidence recovered", v)
				}
			}
			boundary := at.Add(-30 * time.Second)
			want := PlatformAlertRisk
			if key == "process.cpu.core_equivalents" {
				want = PlatformAlertUnknown
			}
			if v := evaluateProcessThreshold(high, policy, boundary, at, false); v.Verdict != want {
				t.Fatal("activation boundary", v, want)
			}
		})
	}
	memory := policy
	memory.TriggerValue = 4 * 1024 * 1024 * 1024
	memory.RecoveryValue = 2 * 1024 * 1024 * 1024
	if !memory.Valid("process.memory.resident_bytes") || memory.Valid("node.memory.used_percent") {
		t.Fatal("absolute memory contract")
	}
}
