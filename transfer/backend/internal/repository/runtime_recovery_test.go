package repository

import (
	"encoding/json"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"reflect"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
)

func TestMergeContinuousDiagnosticsMetadataPreservesRuntimeAndProjectsSafeCaptureFacts(t *testing.T) {
	sampledAt := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	metadata := commonModels.JSONMap{
		"root_fact": "preserved",
		"continuous": map[string]interface{}{
			"owner_instance_id": "worker-a",
			"fencing_token":     uint64(7),
			"schema_change":     map[string]interface{}{"status": "pending"},
			"capture":           map[string]interface{}{"stale": true},
		},
	}
	diagnostics := ContinuousDiagnostics{SampledAt: sampledAt, Health: "healthy"}
	capture := &ContinuousCaptureFacts{
		Generation: 4,
		SourceRecovery: &models.CaptureSourceRecovery{
			SchemaVersion: "capture.source_recovery/v1", Provider: "oracle", Health: "healthy", SampledAt: sampledAt,
		},
		SourceTransactions: &models.CaptureSourceTransactions{
			SchemaVersion: "capture.source_transactions/v1", Provider: "oracle", Status: "available", ActiveCount: 0, SampledAt: sampledAt,
		},
	}

	merged := mergeContinuousDiagnosticsMetadata(metadata, diagnostics, capture)
	continuousMeta, _ := merged["continuous"].(map[string]interface{})
	if merged["root_fact"] != "preserved" || continuousMeta["owner_instance_id"] != "worker-a" || continuousMeta["fencing_token"] != uint64(7) || continuousMeta["schema_change"] == nil {
		t.Fatalf("metadata fields were overwritten: %#v", merged)
	}
	if !reflect.DeepEqual(continuousMeta["diagnostics"], diagnostics) || continuousMeta["capture"] != capture {
		t.Fatalf("observation fields = %#v", continuousMeta)
	}

	withoutCapture := mergeContinuousDiagnosticsMetadata(merged, diagnostics, nil)
	continuousMeta, _ = withoutCapture["continuous"].(map[string]interface{})
	if _, exists := continuousMeta["capture"]; exists {
		t.Fatalf("non-CDC metadata retained capture: %#v", continuousMeta)
	}
	data, err := json.Marshal(withoutCapture)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"stale":true`) {
		t.Fatalf("stale capture leaked after removal: %s", data)
	}
}

func TestBuildContinuousRecoveryPlan(t *testing.T) {
	policy := ContinuousRecoveryPolicy{
		InitialBackoff:  time.Second,
		MaxBackoff:      4 * time.Second,
		MaxFailures:     3,
		CircuitOpenTime: 10 * time.Second,
		StabilityWindow: 30 * time.Second,
	}
	now := time.Date(2026, 7, 15, 15, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		previous       commonExecution.TaskExecution
		reason         string
		sessionStarted time.Time
		wantAttempt    int
		wantDelay      time.Duration
		wantCircuit    string
	}{
		{
			name: "worker shutdown does not increase failures",
			previous: commonExecution.TaskExecution{Metadata: commonModels.JSONMap{
				"recovery_consecutive_failures": 2, "recovery_circuit_state": recoveryCircuitHalfOpen,
			}},
			reason: continuousRecoveryReasonWorkerShutdown, sessionStarted: now.Add(-time.Second),
			wantAttempt: 2, wantDelay: 0, wantCircuit: recoveryCircuitHalfOpen,
		},
		{
			name:     "first failure uses initial backoff",
			previous: commonExecution.TaskExecution{Metadata: commonModels.JSONMap{}},
			reason:   continuousRecoveryReasonExecutionFailed, sessionStarted: now.Add(-time.Second),
			wantAttempt: 1, wantDelay: time.Second, wantCircuit: recoveryCircuitClosed,
		},
		{
			name:     "second failure doubles backoff",
			previous: commonExecution.TaskExecution{Metadata: commonModels.JSONMap{"recovery_consecutive_failures": 1}},
			reason:   continuousRecoveryReasonExecutionFailed, sessionStarted: now.Add(-time.Second),
			wantAttempt: 2, wantDelay: 2 * time.Second, wantCircuit: recoveryCircuitClosed,
		},
		{
			name:     "maximum failures opens circuit",
			previous: commonExecution.TaskExecution{Metadata: commonModels.JSONMap{"recovery_consecutive_failures": 2}},
			reason:   continuousRecoveryReasonLeaseExpired, sessionStarted: now.Add(-time.Second),
			wantAttempt: 3, wantDelay: 10 * time.Second, wantCircuit: recoveryCircuitOpen,
		},
		{
			name: "failed half open probe reopens circuit",
			previous: commonExecution.TaskExecution{Metadata: commonModels.JSONMap{
				"recovery_consecutive_failures": 3, "recovery_circuit_state": recoveryCircuitHalfOpen,
			}},
			reason: continuousRecoveryReasonExecutionFailed, sessionStarted: now.Add(-time.Second),
			wantAttempt: 3, wantDelay: 10 * time.Second, wantCircuit: recoveryCircuitOpen,
		},
		{
			name: "stable session resets consecutive failures",
			previous: commonExecution.TaskExecution{Metadata: commonModels.JSONMap{
				"recovery_consecutive_failures": 3, "recovery_circuit_state": recoveryCircuitHalfOpen,
			}},
			reason: continuousRecoveryReasonExecutionFailed, sessionStarted: now.Add(-31 * time.Second),
			wantAttempt: 1, wantDelay: time.Second, wantCircuit: recoveryCircuitClosed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := buildContinuousRecoveryPlan(tt.previous, tt.reason, tt.sessionStarted, now, policy)
			if plan.Attempt != tt.wantAttempt || plan.NotBefore.Sub(now) != tt.wantDelay || plan.CircuitState != tt.wantCircuit {
				t.Fatalf("plan=%#v, want attempt=%d delay=%s circuit=%s", plan, tt.wantAttempt, tt.wantDelay, tt.wantCircuit)
			}
		})
	}
}

func TestContinuousRecoveryPreservesOnlyVerifiedTaskOrigin(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*commonExecution.TaskExecution)
	}{
		{"valid", nil},
		{"missing_origin", func(e *commonExecution.TaskExecution) { e.ActorPrincipalID = nil }},
		{"conflicting_origin", func(e *commonExecution.TaskExecution) { by := 10; e.TriggeredBy = &by }},
		{"other_tenant", func(e *commonExecution.TaskExecution) { e.TenantID++ }},
		{"other_task", func(e *commonExecution.TaskExecution) {
			e.SourceTaskID = commonExecution.NewSourceTaskIDFromUint(99999)
		}},
		{"other_module", func(e *commonExecution.TaskExecution) { e.Module = commonExecution.ModuleDevelop }},
		{"bounded", func(e *commonExecution.TaskExecution) { e.ExecutionBoundary = commonExecution.ExecutionBoundaryBounded }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTaskRepositoryTestDB(t)
			task := createTaskRepositoryTestTask(t, db, 7, "recovery-origin")
			task.Config["runtime"] = map[string]interface{}{"boundary": "continuous"}
			previous := claimTestExecution(task, uuid.NewString())
			grant, lease, expiry := int64(999), "old-secret", time.Now().Add(time.Hour)
			previous.ExecutionAuthorizationID, previous.LeaseToken, previous.AuthorizationExpiresAt = &grant, &lease, &expiry
			if tc.mutate != nil {
				tc.mutate(&previous)
			}
			var recovered *commonExecution.TaskExecution
			err := db.Transaction(func(tx *gorm.DB) error {
				var err error
				recovered, err = createRecoveryExecution(tx, task, previous, "worker_shutdown", time.Now(), time.Now(), ContinuousRecoveryPolicy{})
				return err
			})
			if tc.mutate != nil {
				if err == nil {
					t.Fatal("invalid recovery origin accepted")
				}
				var count int64
				if err := db.Model(&commonExecution.TaskExecution{}).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("recovery inserted rows: %d, %v", count, err)
				}
				var unchanged models.TransferTask
				if err := db.First(&unchanged, task.ID).Error; err != nil {
					t.Fatal(err)
				}
				if unchanged.LastExecutionID != task.LastExecutionID {
					t.Fatal("failed recovery changed task")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var stored commonExecution.TaskExecution
			if err := db.Where("execution_id = ?", recovered.ExecutionID).First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if stored.TriggeredBy == nil || *stored.TriggeredBy != 9 || stored.ActorPrincipalID == nil || *stored.ActorPrincipalID != 9 || stored.ActorTenantMembershipID == nil || *stored.ActorTenantMembershipID != 19 || stored.IssuedAuthorizationVersion == nil || *stored.IssuedAuthorizationVersion != 1 {
				t.Fatalf("origin was not persisted: %#v", stored)
			}
			if stored.RetryOfExecutionID == nil || *stored.RetryOfExecutionID != previous.ExecutionID || stored.Metadata["recovery_reason"] != "worker_shutdown" {
				t.Fatal("recovery cause lost")
			}
			if stored.ExecutionAuthorizationID != nil || stored.LeaseToken != nil || stored.AuthorizationExpiresAt != nil {
				t.Fatal("recovery copied authorization or lease")
			}
		})
	}
}
