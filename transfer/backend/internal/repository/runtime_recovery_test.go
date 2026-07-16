package repository

import (
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
)

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
