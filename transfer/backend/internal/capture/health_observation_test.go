package capture

import (
	"github.com/addp/transfer/internal/models"
	"testing"
	"time"
)

func TestCaptureHealthEvidenceContinuityAndUnknown(t *testing.T) {
	now := time.Now().UTC()
	r := &models.CaptureResource{Status: models.CaptureStatusRunning, SourceType: models.CaptureSourcePostgreSQL}
	s := &Supervisor{config: SupervisorConfig{MonitorInterval: 15 * time.Second}}
	failed := &ConnectorStatus{ConnectorState: "RUNNING", TaskStates: []string{"FAILED"}}
	publish := func(status *ConnectorStatus, at time.Time) *models.CaptureHealthObservation {
		r.HealthObservation = s.healthObservation(r, status, at)
		return models.NewCaptureSummary(r).HealthObservation
	}
	first := publish(failed, now)
	if first.State != "failed" || first.Reason != "task_not_running" || !first.FailedSince.Equal(now) {
		t.Fatal(first)
	}
	for i := 1; i <= 4; i++ {
		v := publish(failed, now.Add(time.Duration(i)*15*time.Second))
		if !v.FailedSince.Equal(now) {
			t.Fatal("fresh contiguous failure restarted")
		}
	}
	if v := publish(nil, now.Add(65*time.Second)); v.State != "unknown" || v.FailedSince != nil {
		t.Fatal("unavailable status treated as failure")
	}
	restarted := publish(failed, now.Add(70*time.Second))
	if !restarted.FailedSince.Equal(now.Add(70 * time.Second)) {
		t.Fatal("unknown bridged failure duration")
	}
	expired := publish(failed, now.Add(101*time.Second))
	if !expired.FailedSince.Equal(now.Add(101 * time.Second)) {
		t.Fatal("expired gap bridged failure duration")
	}
	running := &ConnectorStatus{ConnectorState: "RUNNING", TaskStates: []string{"RUNNING"}}
	if v := publish(running, now.Add(102*time.Second)); v.State != "unknown" {
		t.Fatal("missing JMX implied healthy")
	}
	connected := true
	r.DelayObservation = sourceObservationMap(&models.CaptureDelayObservation{Status: "available", Connected: &connected, SampledAt: now.Add(102 * time.Second), ExpiresAt: now.Add(132 * time.Second)})
	if v := publish(running, now.Add(103*time.Second)); v.State != "healthy" || v.FailedSince != nil {
		t.Fatal(v)
	}
	connected = false
	r.DelayObservation = sourceObservationMap(&models.CaptureDelayObservation{Status: "available", Connected: &connected, SampledAt: now.Add(104 * time.Second), ExpiresAt: now.Add(134 * time.Second)})
	if v := publish(running, now.Add(104*time.Second)); v.State != "failed" || v.Reason != "source_disconnected" {
		t.Fatal(v)
	}
	if v := publish(running, now.Add(135*time.Second)); v.State != "unknown" {
		t.Fatal("expired JMX implied healthy")
	}
}
