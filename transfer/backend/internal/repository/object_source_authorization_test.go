package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/google/uuid"
)

func TestObjectExecutionWaitsForExactAuthorizationAndRejectsFailedPreparation(t *testing.T) {
	db := newTaskRepositoryTestDB(t)
	repo := NewTaskRepository(db)
	ctx := t.Context()
	task := createTaskRepositoryTestTask(t, db, 7, "exact object import")
	binding, err := authorizationfacts.NativeCandidateBinding(plugin.ObjectItemPath(4, "demo", "orders.parquet"))
	if err != nil {
		t.Fatal(err)
	}
	actor, member, version, who := int64(11), int64(12), int64(3), 11
	record := &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Module: "transfer", Source: "transfer", TaskType: "sync", SourceTaskID: execution.NewSourceTaskIDFromUint(task.ID),
		Status: execution.ExecutionStatusPending, TriggerType: "manual", ExecutionBoundary: "bounded", MaxAttempts: 1, TriggeredBy: &who, ActorPrincipalID: &actor, ActorTenantMembershipID: &member, IssuedAuthorizationVersion: &version,
		ExecutionConfig: commonmodels.JSONMap{"runtime": map[string]any{"boundary": "bounded"}, "load": map[string]any{"mode": "snapshot"},
			"source":        map[string]any{"locator": "addp://engine/4/path/demo/orders.parquet?type=object&item_id=21", "data_type": "table", "representation": "encoded", "format": "parquet"},
			"target":        map[string]any{"parent_locator": "addp://engine/5/path/public?type=schema", "name": "orders", "data_type": "table", "representation": "native"},
			"object_source": execution.TransferObjectSource{EngineVersion: "1", Binding: binding}}}
	if _, _, err := repo.ClaimExecution(ctx, task.ID, task.TenantID, record, ""); err != nil {
		t.Fatal(err)
	}
	if row, lease, err := repo.ClaimNextBoundedExecution(ctx, "object-worker", time.Minute); err != nil || row != nil || lease != nil {
		t.Fatal("unbound object scope was claimable", row, err)
	}
	raw, _ := json.Marshal(record.ExecutionConfig)
	scope, err := execution.NewTransferObjectReadScope(raw)
	if err != nil {
		t.Fatal(err)
	}
	issued := &client.IssuedTransferObjectAuthorization{ID: "9", ExecutionID: record.ExecutionID, TenantID: "7", Audience: "transfer", SourceReadScope: *scope, ExpiresAt: time.Now().Add(time.Minute)}
	changed := *issued
	changed.SourceReadScope = *scope.Clone()
	changed.SourceReadScope.TargetEngineID = 6
	if err := repo.BindTransferObjectAuthorization(ctx, record, &changed); err == nil {
		t.Fatal("accepted different target scope")
	}
	if err := repo.BindTransferObjectAuthorization(ctx, record, issued); err != nil {
		t.Fatal(err)
	}
	row, lease, err := repo.ClaimNextBoundedExecution(ctx, "object-worker", time.Minute)
	if err != nil || row == nil || lease == nil || row.ExecutionID != record.ExecutionID || lease.Attempt != 1 {
		t.Fatal("exact authorization did not release pending execution", err)
	}
	rejected := *record
	rejected.ID = 0
	rejected.ExecutionID = uuid.NewString()
	rejected.ExecutionAuthorizationID = nil
	rejected.AuthorizationExpiresAt = nil
	rejected.Status = execution.ExecutionStatusPending
	if err := execution.CreatePending(ctx, db, &rejected); err != nil {
		t.Fatal(err)
	}
	if err := repo.RejectObjectAuthorization(ctx, &rejected); err != nil {
		t.Fatal(err)
	}
	var stored execution.TaskExecution
	if err := db.First(&stored, rejected.ID).Error; err != nil || stored.Status != execution.ExecutionStatusFailed {
		t.Fatal("failed authorization remained pending", err)
	}
}
