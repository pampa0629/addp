package service

import (
	"testing"
	"time"
)

func TestCaptureMetricWindowBoundsAndPointBudget(t *testing.T) {
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	for _, duration := range []time.Duration{time.Hour, 6 * time.Hour, 24 * time.Hour, 7 * 24 * time.Hour} {
		w, err := NewCaptureMetricWindow(now.Add(-duration), now, now)
		if err != nil || (duration+w.Step-1)/w.Step > 1000 || w.Step < time.Minute {
			t.Fatalf("window %+v %v", w, err)
		}
	}
	for _, tc := range [][2]time.Time{{now, now}, {now.Add(-time.Hour), now.Add(time.Hour)}, {now.Add(-8 * 24 * time.Hour), now}, {now.Add(time.Hour), now}} {
		if _, err := NewCaptureMetricWindow(tc[0], tc[1], now); err == nil {
			t.Fatal("invalid window accepted", tc)
		}
	}
}
