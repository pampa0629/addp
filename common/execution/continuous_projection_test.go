package execution_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	execution "github.com/addp/common/execution"
	"github.com/addp/common/models"
)

func TestContinuousApprovalPublicationPreservesTerminalExecution(t *testing.T) {
	db := newLeaseTestDB(t)
	task := "17"
	start := time.Now().UTC().Add(-time.Minute)
	end := start.Add(time.Second)
	duration := int64(1000)
	actor := int64(8)
	item := execution.TaskExecution{ExecutionID: "continuous-approval", TenantID: 7, Module: execution.ModuleTransfer, TaskType: execution.TaskTypeSync, Source: execution.ModuleTransfer, SourceTaskID: &task, ExecutionBoundary: execution.ExecutionBoundaryContinuous, Status: execution.ExecutionStatusFailed, TriggerType: execution.TriggerTypeManual, Attempt: 1, StartedAt: &start, CompletedAt: &end, ExecutionTimeMs: &duration, ActorPrincipalID: &actor, Metadata: models.JSONMap{"continuous": models.JSONMap{"kept": "yes"}, execution.ObservabilityKey: models.JSONMap{"prior": "kept"}, "cleanup_receipt": models.JSONMap{"protected": "kept"}}}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	var before execution.TaskExecution
	if err := db.First(&before, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	projection := execution.ContinuousSchemaChangeProjection{RequestID: 4, Status: "applied", Generation: 1, FromRevision: 1, ToRevision: 2, DetectedAt: start, MetadataScan: &execution.ContinuousScanProjection{Status: "pending"}}
	origin := execution.AppliedSchemaChangeScanOrigin{RequestID: 4, TaskID: 17, Generation: 1, FromRevision: 1, ToRevision: 2, TargetLocator: "target", AppliedAt: end, TriggeredBy: 12, ActorPrincipalID: 12, ActorTenantMembershipID: 22, IssuedAuthorizationVersion: 1}
	revoked := false
	guard := func(item execution.TaskExecution, now time.Time) error {
		if revoked || item.TenantID != 7 || now.IsZero() {
			return commonapi.ErrConflict
		}
		*item.StartedAt = now
		item.Metadata["cleanup_receipt"].(map[string]interface{})["protected"] = "changed"
		return nil
	}
	publish := func() error {
		return execution.PublishContinuousSchemaChange(t.Context(), db, item.ExecutionID, 7, 17, projection, &origin, guard)
	}
	if err := execution.PublishContinuousSchemaChange(t.Context(), db, item.ExecutionID, 7, 17, projection, &origin, nil); err == nil {
		t.Fatal("missing owner proof accepted")
	}
	if err := execution.PublishContinuousSchemaChange(t.Context(), db, item.ExecutionID, 8, 17, projection, &origin, guard); err == nil {
		t.Fatal("wrong tenant accepted")
	}
	if err := publish(); err != nil {
		t.Fatal(err)
	}
	origin.ActorPrincipalID = 13
	if err := publish(); err == nil {
		t.Fatal("approval actor replacement accepted")
	}
	origin.ActorPrincipalID = 12
	until := time.Now().UTC().Add(time.Minute)
	origin.ClaimToken = "claim"
	origin.Attempt = 1
	origin.LeaseUntil = &until
	projection.MetadataScan = &execution.ContinuousScanProjection{Status: "running", Attempt: 1, LeaseUntil: &until}
	if err := publish(); err != nil {
		t.Fatal(err)
	}
	revoked = true
	origin.Attempt = 2
	projection.MetadataScan.Attempt = 2
	if err := publish(); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("revoked publication=%v", err)
	}
	var after execution.TaskExecution
	if err := db.First(&after, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status || after.Attempt != before.Attempt || !after.StartedAt.Equal(*before.StartedAt) || !after.CompletedAt.Equal(*before.CompletedAt) || *after.ExecutionTimeMs != *before.ExecutionTimeMs || *after.ActorPrincipalID != *before.ActorPrincipalID {
		t.Fatal("approval changed execution lifecycle or actor")
	}
	for _, key := range []string{execution.ObservabilityKey, "cleanup_receipt"} {
		if !reflect.DeepEqual(after.Metadata[key], before.Metadata[key]) {
			t.Fatalf("reserved namespace %s changed", key)
		}
	}
	if after.Metadata["continuous"].(map[string]interface{})["kept"] != "yes" {
		t.Fatal("unrelated continuous facts erased")
	}
	revoked = false
	if err := execution.PublishContinuousMetadataScan(t.Context(), db, item.ExecutionID, 7, 17, execution.ContinuousScanProjection{Status: "success", Attempt: 1, ExecutionID: "scan"}, "", guard); err != nil {
		t.Fatal(err)
	}
	if err := execution.CompleteContinuous(t.Context(), db, item.ExecutionID, execution.ExecutionStatusFailed, execution.ExecutionStatusSuccess, nil, guard); err == nil {
		t.Fatal("publication allowed terminal transition")
	}
}
