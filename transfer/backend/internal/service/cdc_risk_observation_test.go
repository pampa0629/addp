package service

import (
	"encoding/json"
	"github.com/addp/transfer/internal/models"
	"strings"
	"testing"
	"time"
)

func TestCDCRisksUseCurrentTaskSamplesWhilePaused(t *testing.T) {
	now := time.Now().UTC()
	executionID := "bb706a62-4256-453e-b471-0dbdbb305c53"
	task := &models.TransferTask{ID: 7, TenantID: 2, DesiredState: models.TaskDesiredStatePaused, LastExecutionID: &executionID}
	at := now.Add(-time.Second).Format(time.RFC3339Nano)
	expires := now.Add(time.Minute).Format(time.RFC3339Nano)
	for _, tc := range []struct {
		status, risk, buffer, sourceWant, bufferWant string
		expired                                      bool
	}{
		{"available", "lost", "lost", "active", "active", false},
		{"missing", "", "available", "active", "clear", false},
		{"available", "reserved", "available", "clear", "clear", false},
		{"available", "extended", "available", "clear", "clear", false},
		{"available", "unreserved", "available", "clear", "clear", false},
		{"unavailable", "", "lost", "unknown", "active", false},
		{"available", "lost", "lost", "unknown", "unknown", true},
	} {
		t.Run(tc.status+tc.risk+tc.buffer+tc.sourceWant, func(t *testing.T) {
			until := expires
			if tc.expired {
				until = now.Add(-time.Millisecond).Format(time.RFC3339Nano)
			}
			resource := &models.CaptureResource{Generation: 3, Status: models.CaptureStatusRunning, SourceType: models.CaptureSourcePostgreSQL,
				ConnectorName: "private-connector", PostgreSQLObservation: models.JSONMap{"status": tc.status, "risk_state": tc.risk, "sampled_at": at, "expires_at": until, "slot_active": false, "slot_name": "private-slot"},
				BufferObservation: models.JSONMap{"status": "available", "recovery_state": tc.buffer, "sampled_at": at, "expires_at": until, "topic": "private-topic"}}
			row := cdcTaskRisks(task, resource, now)
			if row.State != "observing" || row.Source.State != tc.sourceWant || row.Buffer.State != tc.bufferWant || row.TenantID != 2 || row.Generation != 3 || row.ExecutionID != executionID {
				t.Fatalf("risk projection = %#v", row)
			}
			raw, _ := json.Marshal(row)
			if strings.Contains(string(raw), "private-") {
				t.Fatal("private capture fields escaped")
			}
			resource.Status = models.CaptureStatusCleaning
			if row := cdcTaskRisks(task, resource, now); row.State != "unknown" {
				t.Fatal("incomplete cleanup treated as recovery")
			}
			resource.Status = models.CaptureStatusStopped
			if row := cdcTaskRisks(task, resource, now); row.State != "closed" {
				t.Fatal("completed cleanup not closed")
			}
		})
	}
}

func TestCDCCaptureHealthProjectionRequiresFreshCurrentGeneration(t *testing.T) {
	now := time.Now().UTC()
	executionID := "bb706a62-4256-453e-b471-0dbdbb305c53"
	task := &models.TransferTask{ID: 7, TenantID: 2, DesiredState: models.TaskDesiredStatePaused, LastExecutionID: &executionID}
	since := now.Add(-time.Minute).Format(time.RFC3339Nano)
	for _, tc := range []struct {
		name, state, want string
		sample, expiry    time.Time
	}{
		{"failed", "failed", "failed", now, now.Add(time.Minute)},
		{"healthy", "healthy", "healthy", now, now.Add(time.Minute)},
		{"unknown", "unknown", "unknown", now, now.Add(time.Minute)},
		{"expired", "failed", "unknown", now.Add(-time.Minute), now},
		{"future", "failed", "unknown", now.Add(time.Second), now.Add(time.Minute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := models.JSONMap{"state": tc.state, "sampled_at": tc.sample.Format(time.RFC3339Nano), "expires_at": tc.expiry.Format(time.RFC3339Nano), "connector": "private-connector"}
			if tc.state == "failed" {
				value["reason"], value["failed_since"] = "task_not_running", since
			}
			resource := &models.CaptureResource{Generation: 3, Status: models.CaptureStatusFailed, HealthObservation: value}
			row := cdcTaskRisks(task, resource, now)
			if row.State != "observing" || row.Generation != 3 || row.Capture.State != tc.want {
				t.Fatalf("capture projection = %#v", row)
			}
			if tc.want == "failed" && (row.Capture.FailedSince == nil || row.Capture.FailedSince.Format(time.RFC3339Nano) != since) {
				t.Fatal("capture failure start lost")
			}
			raw, _ := json.Marshal(row)
			if strings.Contains(string(raw), "private-") {
				t.Fatal("private health fields escaped")
			}
		})
	}
}
