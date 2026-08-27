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

func TestPostgresMaterializationSealBindsGenericSuccessfulWriter(t *testing.T) {
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

	tenantID := time.Now().UnixNano()
	now := time.Now().UTC()
	layer := models.DWLayer{TenantID: tenantID, LayerCode: "dwd", LayerName: "Seal DWD", Version: 1}
	if err := tx.Create(&layer).Error; err != nil {
		t.Fatalf("create DW layer: %v", err)
	}
	table := models.LogicalTable{
		TenantID: tenantID, Name: "Seal Fact", Code: "seal_fact", TableType: "fact", Layer: "dwd",
		Status: "approved", GrainDescription: "one row", Version: 1, Materialization: models.JSONB{}, CreatedBy: 1,
	}
	if err := tx.Create(&table).Error; err != nil {
		t.Fatalf("create logical table: %v", err)
	}
	principalID, membershipID, authorizationVersion := int64(11), int64(13), int64(17)
	parentID, prepareID, writerID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	lineage := func(execution *commonExecution.TaskExecution) {
		execution.ActorPrincipalID = &principalID
		execution.ActorTenantMembershipID = &membershipID
		execution.IssuedAuthorizationVersion = &authorizationVersion
		execution.ExecutionBoundary = commonExecution.ExecutionBoundaryBounded
		execution.TriggerType = commonExecution.TriggerTypeManual
		execution.CreatedAt = now
		execution.UpdatedAt = now
	}
	parent := commonExecution.TaskExecution{
		TenantID: int(tenantID), ExecutionID: parentID, Module: commonExecution.ModuleOrchestrator,
		TaskType: commonExecution.TaskTypeOrchestration, Source: commonExecution.ModuleOrchestrator,
		Status: commonExecution.ExecutionStatusRunning, ExecutionConfig: commonModels.JSONMap{},
	}
	prepare := commonExecution.TaskExecution{
		TenantID: int(tenantID), ExecutionID: prepareID, Module: commonExecution.ModuleModel,
		TaskType: commonExecution.TaskTypeMaterializationPrepare, Source: commonExecution.ModuleOrchestrator,
		ParentExecutionID: &parentID, Status: commonExecution.ExecutionStatusSuccess, ExecutionConfig: commonModels.JSONMap{},
	}
	targetLocator := "addp://engine/9/path/public/seal_fact__staging?type=table"
	writer := commonExecution.TaskExecution{
		TenantID: int(tenantID), ExecutionID: writerID, Module: commonExecution.ModuleManager,
		TaskType: "generic_existing_table_write", Source: commonExecution.ModuleOrchestrator,
		ParentExecutionID: &parentID, Status: commonExecution.ExecutionStatusSuccess,
		ExecutionConfig: commonModels.JSONMap{}, Metadata: commonModels.JSONMap{
			"outputs": commonModels.JSONMap{"execution_id": writerID, "target_locator": targetLocator, "row_count": int64(7)},
		},
	}
	for _, execution := range []*commonExecution.TaskExecution{&parent, &prepare, &writer} {
		lineage(execution)
		if err := tx.Create(execution).Error; err != nil {
			t.Fatalf("create execution %s: %v", execution.ExecutionID, err)
		}
	}
	batch := models.MaterializationBatch{
		ID: uuid.NewString(), TenantID: tenantID, LogicalTableID: table.ID, LogicalTableVersion: 1,
		EngineID: 9, TargetParentLocator: "addp://engine/9/path/public?type=schema", TargetName: "seal_fact",
		StagingName: "seal_fact__staging", SchemaFingerprint: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Status: models.MaterializationBatchPrepared, PrepareExecutionID: prepareID, CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&batch).Error; err != nil {
		t.Fatalf("create batch: %v", err)
	}
	repo := NewMaterializationBatchRepository(tx)
	newSealExecution := func() *commonExecution.TaskExecution {
		return &commonExecution.TaskExecution{
			TenantID: int(tenantID), ExecutionID: uuid.NewString(), Module: commonExecution.ModuleModel,
			TaskType: commonExecution.TaskTypeMaterializationSeal, Source: commonExecution.ModuleOrchestrator,
			ParentExecutionID: &parentID, Status: commonExecution.ExecutionStatusPending,
			ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, TriggerType: commonExecution.TriggerTypeManual,
			ExecutionConfig: commonModels.JSONMap{"batch_id": batch.ID}, MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
		}
	}
	invalid := newSealExecution()
	_, err = repo.CreateSealExecution(context.Background(), CreateSealExecutionInput{
		TenantID: tenantID, LogicalTableID: table.ID, BatchID: batch.ID, ParentExecutionID: parentID,
		WriterExecutionID: writerID, TargetLocator: targetLocator + "-other",
	}, invalid)
	if !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("mismatched writer output error = %v, want conflict", err)
	}
	seal := newSealExecution()
	sealedBatch, err := repo.CreateSealExecution(context.Background(), CreateSealExecutionInput{
		TenantID: tenantID, LogicalTableID: table.ID, BatchID: batch.ID, ParentExecutionID: parentID,
		WriterExecutionID: writerID, TargetLocator: targetLocator,
	}, seal)
	if err != nil {
		t.Fatalf("create seal execution: %v", err)
	}
	if sealedBatch.WriterExecutionID == nil || *sealedBatch.WriterExecutionID != writerID ||
		sealedBatch.SealExecutionID == nil || *sealedBatch.SealExecutionID != seal.ExecutionID {
		t.Fatalf("sealed batch binding = %#v", sealedBatch)
	}
	if seal.ActorPrincipalID == nil || *seal.ActorPrincipalID != principalID {
		t.Fatalf("seal execution did not inherit parent lineage: %#v", seal)
	}
}
