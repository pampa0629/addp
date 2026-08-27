package repository

import (
	"context"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/quality/internal/models"
)

func TestMaterializationGateExecutionClaimsBeforeDynamicAuthorization(t *testing.T) {
	db := newCheckTaskRepositoryTestDB(t)
	repo := NewMaterializationGateRepository(db)
	now := time.Now().UTC()
	task := models.MaterializationGateTask{
		TenantID: 7, Code: "outdoor_gate", Name: "Outdoor gate", Version: 1,
		MaterializationGroupID: 9, MaterializationGroupVersion: 2,
		TableBindings: []byte(`[{"alias":"orders","logical_table_id":3}]`),
		Assertions:    []byte(`{"schema_version":"addp.quality.materialization-gate/v1","assertions":[{"assertion_key":"f3889a4a-1675-4623-b6e3-773f9125a04d","type":"not_null","severity":"error","params":{"table":"orders","column":"id"}}]}`),
		CreatedBy:     1, UpdatedBy: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Create(context.Background(), &task); err != nil {
		t.Fatal(err)
	}
	parent := "dbad5a67-d6cd-4d63-b99b-7646d1c89ec5"
	execution := &commonExecution.TaskExecution{
		ExecutionID: "gate-execution-1", TenantID: 7, Module: commonExecution.ModuleQuality,
		TaskType: commonExecution.TaskTypeMaterializationGate, Source: commonExecution.ModuleOrchestrator,
		ParentExecutionID: &parent, ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
		ExecutionConfig: commonModels.JSONMap{"schema_version": "addp.quality.materialization-gate-execution-config/v1"},
	}
	if _, err := repo.CreateExecution(context.Background(), task.ID, task.TenantID, execution); err != nil {
		t.Fatal(err)
	}
	claimed, claimedTask, err := repo.ClaimPendingExecution(context.Background(), "quality-worker", now.Add(time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimedTask == nil || claimed.ExecutionAuthorizationID != nil || claimed.Status != commonExecution.ExecutionStatusRunning {
		t.Fatalf("claimed execution/task = %#v / %#v", claimed, claimedTask)
	}
	lease, err := commonExecution.LeaseFromExecution(*claimed)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachExecutionAuthorization(context.Background(), lease, map[string]interface{}{"execution_authorization_id": int64(41)}); err != nil {
		t.Fatal(err)
	}
	completedAt := now.Add(2 * time.Second)
	if err := repo.CompleteExecutionWithLease(context.Background(), task.ID, task.TenantID, lease, commonExecution.ExecutionStatusSuccess, map[string]interface{}{"progress": 100}, completedAt); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Get(context.Background(), task.TenantID, task.ID)
	if err != nil || stored.LastExecutionStatus != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("stored task = %#v, err=%v", stored, err)
	}
}
