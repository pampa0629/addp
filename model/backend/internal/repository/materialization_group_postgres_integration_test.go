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

func TestPostgresMaterializationGroupPublishesAllBatchesUnderOneExecution(t *testing.T) {
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
	layer := models.DWLayer{TenantID: tenantID, LayerCode: "dws", LayerName: "Group DWS", Version: 1}
	if err := tx.Create(&layer).Error; err != nil {
		t.Fatalf("create DW layer: %v", err)
	}
	tables := []models.LogicalTable{
		{
			TenantID: tenantID, Name: "Person Metric", Code: "person_metric", TableType: "fact", Layer: "dws",
			Status: "approved", GrainDescription: "one person", Version: 1, CreatedBy: 1,
			Materialization: models.JSONB{"target_parent_locator": "addp://engine/9/path/public?type=schema", "target_name": "person_metric"},
		},
		{
			TenantID: tenantID, Name: "Person Pair Metric", Code: "person_pair_metric", TableType: "fact", Layer: "dws",
			Status: "approved", GrainDescription: "one person pair", Version: 1, CreatedBy: 1,
			Materialization: models.JSONB{"target_parent_locator": "addp://engine/9/path/public?type=schema", "target_name": "person_pair_metric"},
		},
	}
	for index := range tables {
		if err := tx.Create(&tables[index]).Error; err != nil {
			t.Fatalf("create logical table %d: %v", index, err)
		}
	}

	groupRepo := NewMaterializationGroupRepository(tx)
	group := &models.MaterializationGroup{
		TenantID: tenantID, Code: "outdoor_metrics", Name: "Outdoor Metrics", Version: 1,
		CreatedBy: 1, UpdatedBy: 1, CreatedAt: now, UpdatedAt: now,
		Members: []models.MaterializationGroupMember{
			{TenantID: tenantID, LogicalTableID: int64(tables[0].ID), Position: 0},
			{TenantID: tenantID, LogicalTableID: int64(tables[1].ID), Position: 1},
		},
	}
	versions := map[int64]int64{int64(tables[0].ID): 1, int64(tables[1].ID): 1}
	if err := groupRepo.Create(ctx, group, versions); err != nil {
		t.Fatalf("create materialization group: %v", err)
	}
	group.Name = "Outdoor Metric Publication"
	group.UpdatedBy = 2
	if err := groupRepo.Update(ctx, group, 1, versions); err != nil {
		t.Fatalf("update materialization group: %v", err)
	}
	if group.Version != 2 {
		t.Fatalf("group version = %d, want 2", group.Version)
	}
	stale := *group
	stale.Name = "Stale Update"
	if err := groupRepo.Update(ctx, &stale, 1, versions); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("stale group update error = %v, want conflict", err)
	}

	principalID, membershipID, authorizationVersion := int64(11), int64(13), int64(17)
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

	batches := make([]models.MaterializationBatch, 0, len(tables))
	for index := range tables {
		prepareID := "prepare-execution-" + uuid.NewString()
		prepare := commonExecution.TaskExecution{
			TenantID: int(tenantID), ExecutionID: prepareID, Module: commonExecution.ModuleModel,
			TaskType: commonExecution.TaskTypeMaterializationPrepare, Source: commonExecution.ModuleOrchestrator,
			ParentExecutionID: &parentID, Status: commonExecution.ExecutionStatusSuccess,
			ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, TriggerType: commonExecution.TriggerTypeManual,
			ActorPrincipalID: &principalID, ActorTenantMembershipID: &membershipID,
			IssuedAuthorizationVersion: &authorizationVersion, ExecutionConfig: commonModels.JSONMap{},
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&prepare).Error; err != nil {
			t.Fatalf("create prepare execution %d: %v", index, err)
		}
		batchID := uuid.NewString()
		writerExecutionID := uuid.NewString()
		sealExecutionID := uuid.NewString()
		stagingName := "staging_" + uuid.NewString()[:8]
		batch := models.MaterializationBatch{
			ID: batchID, TenantID: tenantID, LogicalTableID: int64(tables[index].ID), LogicalTableVersion: 1,
			EngineID: 9, TargetParentLocator: "addp://engine/9/path/public?type=schema",
			TargetName: tables[index].Materialization["target_name"].(string), StagingName: stagingName,
			SchemaFingerprint: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Status:            models.MaterializationBatchSealed, PrepareExecutionID: prepareID,
			WriterExecutionID: &writerExecutionID, SealExecutionID: &sealExecutionID, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&batch).Error; err != nil {
			t.Fatalf("create materialization batch %d: %v", index, err)
		}
		batches = append(batches, batch)
	}

	executionID := uuid.NewString()
	publishExecution := &commonExecution.TaskExecution{
		TenantID: int(tenantID), ExecutionID: executionID, Module: commonExecution.ModuleModel,
		TaskType: commonExecution.TaskTypeMaterializationGroupPublish, Source: commonExecution.ModuleOrchestrator,
		ParentExecutionID: &parentID, Status: commonExecution.ExecutionStatusPending,
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, TriggerType: commonExecution.TriggerTypeManual,
		ExecutionConfig: commonModels.JSONMap{"schema_version": "model.materialization-group/v1"},
		MaxAttempts:     3, CreatedAt: now, UpdatedAt: now,
	}
	batchRepo := NewMaterializationBatchRepository(tx)
	if _, _, err := batchRepo.CreateGroupPublishExecution(ctx, tenantID, group.ID, group.Version+1, parentID, publishExecution); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("mismatched expected group version error = %v, want conflict", err)
	}
	loadedGroup, queuedBatches, err := batchRepo.CreateGroupPublishExecution(ctx, tenantID, group.ID, group.Version, parentID, publishExecution)
	if err != nil {
		t.Fatalf("create group publish execution: %v", err)
	}
	if loadedGroup.Version != 2 || len(queuedBatches) != 2 || queuedBatches[0].LogicalTableID != int64(tables[0].ID) {
		t.Fatalf("queued group=%#v batches=%#v", loadedGroup, queuedBatches)
	}
	expiresAt := now.Add(time.Hour)
	if err := batchRepo.AttachAuthorization(ctx, tenantID, executionID, map[string]interface{}{
		"execution_authorization_id": int64(991), "authorization_expires_at": expiresAt,
	}); err != nil {
		t.Fatalf("attach authorization: %v", err)
	}
	claimed, claimedBatches, err := batchRepo.ClaimPendingGroupExecution(ctx, "group-worker", now, time.Minute)
	if err != nil || claimed == nil || len(claimedBatches) != 2 {
		t.Fatalf("claim group execution: execution=%#v batches=%#v error=%v", claimed, claimedBatches, err)
	}
	lease, err := commonExecution.LeaseFromExecution(*claimed)
	if err != nil {
		t.Fatalf("read group lease: %v", err)
	}
	batchIDs := []string{claimedBatches[0].ID, claimedBatches[1].ID}
	metadata := commonModels.JSONMap{"schema_version": "model.materialization-group/v1"}
	if err := batchRepo.CompleteGroupExecution(ctx, lease, batchIDs, commonExecution.ExecutionStatusSuccess, metadata, nil); err != nil {
		t.Fatalf("complete group execution: %v", err)
	}
	var publishedCount int64
	if err := tx.Model(&models.MaterializationBatch{}).
		Where("tenant_id = ? AND id IN ? AND status = ? AND publish_execution_id = ? AND published_at IS NOT NULL",
			tenantID, batchIDs, models.MaterializationBatchPublished, executionID).
		Count(&publishedCount).Error; err != nil {
		t.Fatalf("count published batches: %v", err)
	}
	if publishedCount != 2 {
		t.Fatalf("published batch count = %d, want 2", publishedCount)
	}
}
