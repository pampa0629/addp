package service

import (
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"testing"
	"time"
)

var testFilesystemMount = resourcequery.Dimensions{"device": "/dev/data", "mountpoint": "/data", "fstype": "ext4"}

func filesystemPressureWindow(value float64, at time.Time, total float64) (resourcequery.Series, resourcequery.Series, []resourcequery.Series) {
	metric, uptime := pressureWindow(models.FilesystemCapacityMetric, value, at)
	metric.Dimensions = testFilesystemMount.Copy()
	companions := []resourcequery.Series{}
	for i, key := range filesystemPressureKeys {
		series := resourcequery.Series{MetricKey: key, Unit: "bytes", Dimensions: metric.Dimensions.Copy()}
		for _, point := range metric.Points {
			v := []float64{total, total * value / 100, total * (100 - value) / 100}[i]
			point.Value = &v
			series.Points = append(series.Points, point)
		}
		companions = append(companions, series)
	}
	return metric, uptime, companions
}
func TestFilesystemPressureThresholdsAndMountEvidence(t *testing.T) {
	at := time.Date(2026, 10, 27, 0, 10, 0, 0, time.UTC)
	policy := defaultResourcePressurePolicy(models.FilesystemCapacityMetric)
	for _, test := range []struct {
		value  float64
		active bool
		want   PlatformAlertVerdict
	}{
		{86, false, PlatformAlertRisk}, {85, false, PlatformAlertNormal}, {79, true, PlatformAlertNormal}, {80, true, PlatformAlertRisk}, {83, false, PlatformAlertNormal}, {83, true, PlatformAlertRisk}, {0, true, PlatformAlertNormal}, {100, false, PlatformAlertRisk},
	} {
		metric, uptime, companions := filesystemPressureWindow(test.value, at, 1024*1024*1024)
		got := evaluateResourcePressure(metric, uptime, at.Add(-time.Hour), at, true, test.active, policy, companions...)
		if got.Verdict != test.want {
			t.Fatalf("value=%v active=%v: %+v", test.value, test.active, got)
		}
	}
	for _, mutate := range []func(*resourcequery.Series, *resourcequery.Series, []resourcequery.Series){
		func(m, u *resourcequery.Series, c []resourcequery.Series) { m.Dimensions["fstype"] = "xfs" },
		func(m, u *resourcequery.Series, c []resourcequery.Series) { c[0].Points[18].Value = nil },
		func(m, u *resourcequery.Series, c []resourcequery.Series) { c[1].Points[18].DataState = "stale" },
		func(m, u *resourcequery.Series, c []resourcequery.Series) {
			v := 2 * 1024 * 1024 * 1024.0
			c[0].Points[18].Value = &v
		},
		func(m, u *resourcequery.Series, c []resourcequery.Series) {
			c[2].Points[18].SampledAt = m.Points[17].SampledAt
		},
		func(m, u *resourcequery.Series, c []resourcequery.Series) {
			m.Points[18].SampledAt = m.Points[17].SampledAt
			u.Points[18].SampledAt = m.Points[17].SampledAt
		},
		func(m, u *resourcequery.Series, c []resourcequery.Series) { v := 1.0; c[2].Points[18].Value = &v },
	} {
		for _, active := range []bool{false, true} {
			metric, uptime, companions := filesystemPressureWindow(90, at, 1024*1024*1024)
			mutate(&metric, &uptime, companions)
			if got := evaluateResourcePressure(metric, uptime, at.Add(-time.Hour), at, true, active, policy, companions...); got.Verdict != PlatformAlertUnknown {
				t.Fatalf("invalid evidence changed lifecycle: %+v", got)
			}
		}
	}
}
func TestFilesystemSignalsSeparateFullMountIdentity(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range []resourcequery.Dimensions{testFilesystemMount, {"device": "/dev/other", "mountpoint": "/data", "fstype": "ext4"}, {"device": "/dev/data", "mountpoint": "/bind", "fstype": "ext4"}, {"device": "/dev/data", "mountpoint": "/data", "fstype": "xfs"}} {
		signal := models.ResourceAlertSignal(models.FilesystemCapacityMetric, d)
		if seen[signal] || len(signal) > 160 {
			t.Fatal("mount identity collision", signal)
		}
		seen[signal] = true
	}
	if models.ResourceAlertSignal("node.memory.used_percent", nil) != "node.memory.used_percent" {
		t.Fatal("scalar signal changed")
	}
}
