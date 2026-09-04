package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/model/internal/migration"
	"github.com/addp/model/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresPrepareReclaimsOnlyTerminalParentBatchWithoutActiveChildren(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_MODEL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_MODEL_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("run model migrations: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })

	ctx := context.Background()
	tenantID := time.Now().UnixNano()
	now := time.Now().UTC()
	principalID, membershipID, authorizationVersion := int64(11), int64(13), int64(17)
	layer := models.DWLayer{TenantID: tenantID, LayerCode: "dwd", LayerName: "Prepare Reclaim DWD", Version: 1}
	if err := tx.Create(&layer).Error; err != nil {
		t.Fatalf("create DW layer: %v", err)
	}
	table := models.LogicalTable{
		TenantID: tenantID, Name: "Prepare Reclaim", Code: "prepare_reclaim", TableType: "fact", Layer: "dwd",
		Status: "approved", GrainDescription: "one row", Version: 1, Materialization: models.JSONB{}, CreatedBy: 1,
	}
	if err := tx.Create(&table).Error; err != nil {
		t.Fatalf("create logical table: %v", err)
	}

	newExecution := func(module, taskType, status string, parentID *string) commonExecution.TaskExecution {
		return commonExecution.TaskExecution{
			TenantID: int(tenantID), ExecutionID: uuid.NewString(), Module: module, TaskType: taskType,
			Source: commonExecution.ModuleOrchestrator, ParentExecutionID: parentID, Status: status,
			ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, TriggerType: commonExecution.TriggerTypeManual,
			ActorPrincipalID: &principalID, ActorTenantMembershipID: &membershipID,
			IssuedAuthorizationVersion: &authorizationVersion, ExecutionConfig: commonModels.JSONMap{},
			CreatedAt: now, UpdatedAt: now,
		}
	}
	createExistingBatch := func(targetName, parentStatus, batchStatus string, activeChild bool) models.MaterializationBatch {
		parent := newExecution(commonExecution.ModuleOrchestrator, commonExecution.TaskTypeOrchestration, parentStatus, nil)
		if err := tx.Create(&parent).Error; err != nil {
			t.Fatalf("create old parent %s: %v", targetName, err)
		}
		prepare := newExecution(commonExecution.ModuleModel, commonExecution.TaskTypeMaterializationPrepare, commonExecution.ExecutionStatusSuccess, &parent.ExecutionID)
		if err := tx.Create(&prepare).Error; err != nil {
			t.Fatalf("create old prepare %s: %v", targetName, err)
		}
		if activeChild {
			child := newExecution(commonExecution.ModuleTransfer, commonExecution.TaskTypeSync, commonExecution.ExecutionStatusRunning, &parent.ExecutionID)
			if err := tx.Create(&child).Error; err != nil {
				t.Fatalf("create active child %s: %v", targetName, err)
			}
		}
		batch := models.MaterializationBatch{
			ID: uuid.NewString(), TenantID: tenantID, LogicalTableID: table.ID, LogicalTableVersion: 1,
			EngineID: 9, TargetParentLocator: "addp://engine/9/path/public?type=schema", TargetName: targetName,
			StagingName: targetName + "__staging", SchemaFingerprint: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Status: batchStatus, PrepareExecutionID: prepare.ExecutionID, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&batch).Error; err != nil {
			t.Fatalf("create old batch %s: %v", targetName, err)
		}
		return batch
	}
	newPrepare := func(targetName string) (*models.MaterializationBatch, *commonExecution.TaskExecution) {
		parent := newExecution(commonExecution.ModuleOrchestrator, commonExecution.TaskTypeOrchestration, commonExecution.ExecutionStatusRunning, nil)
		if err := tx.Create(&parent).Error; err != nil {
			t.Fatalf("create new parent %s: %v", targetName, err)
		}
		batch := &models.MaterializationBatch{
			ID: uuid.NewString(), TenantID: tenantID, LogicalTableID: table.ID, LogicalTableVersion: 1,
			EngineID: 9, TargetParentLocator: "addp://engine/9/path/public?type=schema", TargetName: targetName,
			StagingName: targetName + "__new_staging", SchemaFingerprint: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Status: models.MaterializationBatchPreparing, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
		}
		execution := newExecution(commonExecution.ModuleModel, commonExecution.TaskTypeMaterializationPrepare, commonExecution.ExecutionStatusPending, &parent.ExecutionID)
		batch.PrepareExecutionID = execution.ExecutionID
		return batch, &execution
	}

	repo := NewMaterializationBatchRepository(tx)
	reclaimable := createExistingBatch("terminal_target", commonExecution.ExecutionStatusFailed, models.MaterializationBatchPrepared, false)
	batch, execution := newPrepare(reclaimable.TargetName)
	if err := repo.CreatePrepareExecution(ctx, batch, execution, table.Name); err != nil {
		t.Fatalf("create prepare after terminal parent: %v", err)
	}
	var reloaded models.MaterializationBatch
	if err := tx.First(&reloaded, "id = ?", reclaimable.ID).Error; err != nil {
		t.Fatalf("reload reclaimed batch: %v", err)
	}
	if reloaded.Status != models.MaterializationBatchAborted {
		t.Fatalf("reclaimed batch status = %q, want aborted", reloaded.Status)
	}
	reclaimableStaging, err := repo.ListReclaimableStagingBatches(ctx, *batch)
	if err != nil {
		t.Fatalf("list reclaimable staging: %v", err)
	}
	if len(reclaimableStaging) != 1 || reclaimableStaging[0].ID != reclaimable.ID {
		t.Fatalf("reclaimable staging = %#v, want batch %s", reclaimableStaging, reclaimable.ID)
	}

	for _, testCase := range []struct {
		name         string
		parentStatus string
		batchStatus  string
		activeChild  bool
	}{
		{name: "running_parent", parentStatus: commonExecution.ExecutionStatusRunning, batchStatus: models.MaterializationBatchPrepared},
		{name: "successful_parent", parentStatus: commonExecution.ExecutionStatusSuccess, batchStatus: models.MaterializationBatchSealed},
		{name: "active_child", parentStatus: commonExecution.ExecutionStatusFailed, batchStatus: models.MaterializationBatchPrepared, activeChild: true},
		{name: "publishing_batch", parentStatus: commonExecution.ExecutionStatusFailed, batchStatus: models.MaterializationBatchPublishing},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			existing := createExistingBatch(testCase.name, testCase.parentStatus, testCase.batchStatus, testCase.activeChild)
			candidate, candidateExecution := newPrepare(existing.TargetName)
			err := repo.CreatePrepareExecution(ctx, candidate, candidateExecution, table.Name)
			if !errors.Is(err, commonAPI.ErrConflict) {
				t.Fatalf("CreatePrepareExecution() error = %v, want conflict", err)
			}
		})
	}
}

func TestPostgresPrepareCompletionPersistsTargetPredecessorAtomically(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_MODEL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_MODEL_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("run model migrations: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })

	ctx := context.Background()
	tenantID := time.Now().UnixNano()
	now := time.Now().UTC()
	principalID, membershipID, authorizationVersion := int64(11), int64(13), int64(17)
	layer := models.DWLayer{TenantID: tenantID, LayerCode: "dwd", LayerName: "Prepare CAS DWD", Version: 1}
	if err := tx.Create(&layer).Error; err != nil {
		t.Fatalf("create DW layer: %v", err)
	}
	table := models.LogicalTable{
		TenantID: tenantID, Name: "Prepare CAS", Code: "prepare_cas", TableType: "fact", Layer: "dwd",
		Status: "approved", GrainDescription: "one row", Version: 1, Materialization: models.JSONB{}, CreatedBy: 1,
	}
	if err := tx.Create(&table).Error; err != nil {
		t.Fatalf("create logical table: %v", err)
	}
	parentID := uuid.NewString()
	parent := commonExecution.TaskExecution{
		TenantID: int(tenantID), ExecutionID: parentID, Module: commonExecution.ModuleOrchestrator,
		TaskType: commonExecution.TaskTypeOrchestration, Source: commonExecution.ModuleOrchestrator,
		Status: commonExecution.ExecutionStatusRunning, ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		TriggerType: commonExecution.TriggerTypeManual, ActorPrincipalID: &principalID,
		ActorTenantMembershipID: &membershipID, IssuedAuthorizationVersion: &authorizationVersion,
		ExecutionConfig: commonModels.JSONMap{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&parent).Error; err != nil {
		t.Fatalf("create parent execution: %v", err)
	}

	repo := NewMaterializationBatchRepository(tx)
	authorizationID := int64(990)
	prepare := func(targetName string) (*models.MaterializationBatch, commonExecution.Lease) {
		batchID := uuid.NewString()
		executionID := uuid.NewString()
		batch := &models.MaterializationBatch{
			ID: batchID, TenantID: tenantID, LogicalTableID: table.ID, LogicalTableVersion: table.Version,
			EngineID: 9, TargetParentLocator: "addp://engine/9/path/public?type=schema", TargetName: targetName,
			StagingName: targetName + "__staging", SchemaFingerprint: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Status: models.MaterializationBatchPreparing, PrepareExecutionID: executionID, CreatedAt: now, UpdatedAt: now,
		}
		execution := &commonExecution.TaskExecution{
			TenantID: int(tenantID), ExecutionID: executionID, Module: commonExecution.ModuleModel,
			TaskType: commonExecution.TaskTypeMaterializationPrepare, Source: commonExecution.ModuleOrchestrator,
			ParentExecutionID: &parentID, Status: commonExecution.ExecutionStatusPending,
			ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, TriggerType: commonExecution.TriggerTypeManual,
			ExecutionConfig: commonModels.JSONMap{"batch_id": batchID}, MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
		}
		if err := repo.CreatePrepareExecution(ctx, batch, execution, table.Name); err != nil {
			t.Fatalf("create prepare execution: %v", err)
		}
		expiresAt := now.Add(time.Hour)
		authorizationID++
		if err := repo.AttachAuthorization(ctx, tenantID, executionID, map[string]interface{}{
			"execution_authorization_id": authorizationID, "authorization_expires_at": expiresAt,
		}); err != nil {
			t.Fatalf("attach authorization: %v", err)
		}
		claimed, claimedBatch, err := repo.ClaimPendingExecution(ctx, commonExecution.TaskTypeMaterializationPrepare, "prepare-worker", now, time.Minute)
		if err != nil || claimed == nil || claimedBatch == nil || claimedBatch.ID != batchID {
			t.Fatalf("claim prepare execution: execution=%#v batch=%#v error=%v", claimed, claimedBatch, err)
		}
		lease, err := commonExecution.LeaseFromExecution(*claimed)
		if err != nil {
			t.Fatalf("read prepare lease: %v", err)
		}
		return batch, lease
	}

	marker := "addp:model-materialization:v1:7:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef:old-batch"
	completedBatch, completedLease := prepare("persisted_target")
	if err := repo.CompleteExecution(ctx, completedLease, completedBatch.ID,
		commonExecution.TaskTypeMaterializationPrepare, commonExecution.ExecutionStatusSuccess,
		models.MaterializationBatchPrepared, commonModels.JSONMap{"expected_target_marker": marker}, nil); err != nil {
		t.Fatalf("complete prepare execution: %v", err)
	}
	var reloaded models.MaterializationBatch
	if err := tx.First(&reloaded, "id = ?", completedBatch.ID).Error; err != nil {
		t.Fatalf("reload completed batch: %v", err)
	}
	if reloaded.Status != models.MaterializationBatchPrepared || reloaded.ExpectedTargetMarker == nil || *reloaded.ExpectedTargetMarker != marker {
		t.Fatalf("completed batch = %#v", reloaded)
	}

	rejectedBatch, rejectedLease := prepare("missing_predecessor_target")
	err = repo.CompleteExecution(ctx, rejectedLease, rejectedBatch.ID,
		commonExecution.TaskTypeMaterializationPrepare, commonExecution.ExecutionStatusSuccess,
		models.MaterializationBatchPrepared, commonModels.JSONMap{"schema_version": "model.materialization/v1"}, nil)
	if !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("missing predecessor completion error = %v, want conflict", err)
	}
	reloaded = models.MaterializationBatch{}
	if err := tx.First(&reloaded, "id = ?", rejectedBatch.ID).Error; err != nil {
		t.Fatalf("reload rejected batch: %v", err)
	}
	if reloaded.Status != models.MaterializationBatchPreparing || reloaded.ExpectedTargetMarker != nil {
		t.Fatalf("rejected batch changed = %#v", reloaded)
	}
	var rejectedExecution commonExecution.TaskExecution
	if err := tx.First(&rejectedExecution, "tenant_id = ? AND execution_id = ?", tenantID, rejectedLease.ExecutionID).Error; err != nil {
		t.Fatalf("reload rejected execution: %v", err)
	}
	if rejectedExecution.Status != commonExecution.ExecutionStatusRunning {
		t.Fatalf("rejected execution status = %q, want running", rejectedExecution.Status)
	}
}
