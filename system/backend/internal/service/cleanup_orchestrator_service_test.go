package service

import (
	"errors"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"testing"
)

func TestCleanupExecutorEnabledReadsModuleRegistryCapability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata map[string]interface{}
		want     bool
	}{
		{
			name: "enabled resource reclaim executor",
			metadata: map[string]interface{}{
				"module": "meta",
				"capabilities": map[string]interface{}{
					"cleanup_executor": map[string]interface{}{
						"enabled": true,
					},
				},
			},
			want: true,
		},
		{
			name: "disabled resource reclaim executor",
			metadata: map[string]interface{}{
				"capabilities": map[string]interface{}{
					"cleanup_executor": map[string]interface{}{
						"enabled": false,
					},
				},
			},
			want: false,
		},
		{
			name: "missing capability",
			metadata: map[string]interface{}{
				"module": "transfer",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := cleanupExecutorEnabled(tt.metadata); got != tt.want {
				t.Fatalf("cleanupExecutorEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCleanupExecutorSupportsCause(t *testing.T) {
	t.Parallel()
	metadata := map[string]interface{}{
		"capabilities": map[string]interface{}{
			"cleanup_executor": map[string]interface{}{
				"enabled": true,
				"causes":  []interface{}{events.CleanupCauseEngineDeleting, events.CleanupCauseTenantDeleted},
			},
		},
	}
	if !cleanupExecutorSupportsCause(metadata, events.CleanupCauseEngineDeleting) {
		t.Fatal("expected engine.deleting cause to be supported")
	}
	if cleanupExecutorSupportsCause(metadata, "item.deleted") {
		t.Fatal("unexpected item.deleted support")
	}
}

func TestValidateCleanupExecuteConfirmation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		cleanupMode  string
		summary      events.CleanupResultSummary
		confirmation CleanupExecuteConfirmation
		wantErr      error
	}{
		{
			name:        "unconfirmed request is rejected",
			cleanupMode: events.CleanupModeLogical,
			confirmation: CleanupExecuteConfirmation{
				Confirmed: false,
			},
			wantErr: ErrCleanupExecuteConfirmRequired,
		},
		{
			name:        "physical cleanup requires external artifact policy",
			cleanupMode: events.CleanupModePhysical,
			confirmation: CleanupExecuteConfirmation{
				Confirmed: true,
			},
			wantErr: ErrCleanupExternalArtifactPolicyRequired,
		},
		{
			name:        "physical cleanup requires token",
			cleanupMode: events.CleanupModePhysical,
			confirmation: CleanupExecuteConfirmation{
				Confirmed:              true,
				ExternalArtifactPolicy: commonModels.ExternalArtifactPolicyDelete,
			},
			wantErr: ErrCleanupExecuteConfirmTokenRequired,
		},
		{
			name:        "physical cleanup rejects invalid external artifact policy",
			cleanupMode: events.CleanupModePhysical,
			confirmation: CleanupExecuteConfirmation{
				Confirmed:              true,
				ConfirmationToken:      "CONFIRM",
				ExternalArtifactPolicy: "unknown",
			},
			wantErr: ErrCleanupExternalArtifactPolicyInvalid,
		},
		{
			name:        "logical cleanup rejects external artifact policy",
			cleanupMode: events.CleanupModeLogical,
			confirmation: CleanupExecuteConfirmation{
				Confirmed:              true,
				ExternalArtifactPolicy: commonModels.ExternalArtifactPolicyAbandon,
			},
			wantErr: ErrCleanupExternalArtifactPolicyNotAllowed,
		},
		{
			name:        "high risk logical cleanup requires token",
			cleanupMode: events.CleanupModeLogical,
			summary: events.CleanupResultSummary{
				RiskLevel: "high",
			},
			confirmation: CleanupExecuteConfirmation{
				Confirmed: true,
			},
			wantErr: ErrCleanupExecuteConfirmTokenRequired,
		},
		{
			name:        "low risk logical cleanup accepts confirmation",
			cleanupMode: events.CleanupModeLogical,
			summary: events.CleanupResultSummary{
				RiskLevel: "low",
			},
			confirmation: CleanupExecuteConfirmation{
				Confirmed: true,
			},
		},
		{
			name:        "physical cleanup accepts token",
			cleanupMode: events.CleanupModePhysical,
			confirmation: CleanupExecuteConfirmation{
				Confirmed:              true,
				ConfirmationToken:      "CONFIRM",
				ExternalArtifactPolicy: commonModels.ExternalArtifactPolicyAbandon,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateCleanupExecuteConfirmation(tt.cleanupMode, tt.summary, tt.confirmation)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("validateCleanupExecuteConfirmation() error = %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateCleanupExecuteConfirmation() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestCleanupReceiptSummaryCountsOnlyTypedFields(t *testing.T) {
	impact, err := events.BuildCleanupImpactData([]events.CleanupImpactItem{{StableRef: "task:3", Disposition: events.CleanupImpactRebindable}}, "")
	if err != nil {
		t.Fatal(err)
	}
	rows := []execution.CleanupReceipt{{Module: "develop", Summary: events.CleanupResultSummary{AffectedRecords: 3, DeletedTaskDefinitions: 3, ErrorCount: 2, RiskLevel: "high"}, Impact: impact.Summary, ImpactDigest: impact.Digest}}
	summary, actual, digest, err := cleanupReceiptSummary(rows)
	if err != nil || summary.DeletedTaskDefinitions != 3 || summary.ErrorCount != 2 || summary.RiskLevel != "high" || actual.Rebindable != 1 || digest == "" {
		t.Fatalf("aggregate=%+v impact=%+v digest=%q err=%v", summary, actual, digest, err)
	}
}

func TestCleanupExecuteScopeRequiresExplicitManagerPair(t *testing.T) {
	for _, tc := range []struct {
		scope  []string
		denied bool
	}{
		{[]string{"meta"}, true}, {[]string{"graph", "meta"}, true},
		{[]string{"manager", "meta"}, false}, {[]string{"manager"}, false}, {[]string{"graph"}, false},
	} {
		original := append([]string(nil), tc.scope...)
		err := validateCleanupExecuteScope(tc.scope)
		if errors.Is(err, ErrCleanupManagerScopeRequired) != tc.denied {
			t.Fatalf("%v: %v", tc.scope, err)
		}
		for i := range original {
			if original[i] != tc.scope[i] {
				t.Fatal("scope expanded")
			}
		}
	}
}
