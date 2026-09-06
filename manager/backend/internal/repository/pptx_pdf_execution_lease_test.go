package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

func TestPPTXPDFExecutionUsesLeaseAndRejectsLateCompletion(t *testing.T) {
	db := newPPTXPDFExecutionLeaseTestDB(t)
	repo := NewPPTXPDFRepository(db)
	task := createPPTXPDFExecutionLeaseTestTask(t, db, 7)
	createdAt := time.Now().UTC()
	execution := newManagerRepositoryTestExecution(
		"manager-pptx-lease-1", int(task.TenantID), commonExecution.TaskTypePPTXPDFGeneration, createdAt,
	)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, execution, false); err != nil {
		t.Fatalf("enqueue PPTX execution: %v", err)
	}

	claimedExecution, lease, claimedTask, err := repo.ClaimPendingExecution(
		context.Background(), "pptx-worker-1", createdAt.Add(time.Second), 30*time.Second,
	)
	if err != nil {
		t.Fatalf("claim pending PPTX execution: %v", err)
	}
	if claimedExecution == nil || lease == nil || claimedTask == nil {
		t.Fatalf("claim = execution %#v lease %#v task %#v", claimedExecution, lease, claimedTask)
	}
	if claimedExecution.ExecutionID != execution.ExecutionID || claimedExecution.Status != commonExecution.ExecutionStatusRunning ||
		claimedExecution.Attempt != 1 || lease.Token == "" || claimedTask.ID != task.ID {
		t.Fatalf("claimed execution = %#v lease = %#v task = %#v", claimedExecution, lease, claimedTask)
	}

	completedAt := createdAt.Add(2 * time.Second)
	if err := repo.CompleteExecutionWithLease(
		context.Background(), task.ID, task.TenantID, *lease, 0, nil,
		map[string]interface{}{"status": commonExecution.ExecutionStatusSuccess, "progress": 100}, completedAt,
	); err != nil {
		t.Fatalf("complete PPTX execution with lease: %v", err)
	}
	if err := repo.CompleteExecutionWithLease(
		context.Background(), task.ID, task.TenantID, *lease, 0, nil,
		map[string]interface{}{"status": commonExecution.ExecutionStatusFailed, "progress": 0}, completedAt,
	); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("late completion error = %v, want conflict", err)
	}
}

func TestPPTXPDFExpiredLeaseFailsExecutionTaskAndBuildingResult(t *testing.T) {
	db := newPPTXPDFExecutionLeaseTestDB(t)
	repo := NewPPTXPDFRepository(db)
	task := createPPTXPDFExecutionLeaseTestTask(t, db, 8)
	createdAt := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	execution := newManagerRepositoryTestExecution(
		"manager-pptx-expired-1", int(task.TenantID), commonExecution.TaskTypePPTXPDFGeneration, createdAt,
	)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, execution, false); err != nil {
		t.Fatalf("enqueue PPTX execution: %v", err)
	}
	_, lease, _, err := repo.ClaimPendingExecution(
		context.Background(), "pptx-worker-1", createdAt.Add(time.Second), 30*time.Second,
	)
	if err != nil || lease == nil {
		t.Fatalf("claim pending PPTX execution lease = %#v error = %v", lease, err)
	}
	result := &models.PPTXPDF{
		TenantID: task.TenantID, ItemFingerprint: task.ItemFingerprint, ArtifactVariant: models.PPTXPDFArtifactVariant,
		SourceVersion: task.SourceVersion, SourceEngineID: task.SourceEngineID, ItemID: task.ItemID,
		Locator: task.Locator, TaskID: &task.ID, LastExecutionID: &execution.ExecutionID,
		StorageRef: "object-store-ref", FileName: "slides.pdf", Status: models.PPTXPDFStatusBuilding,
		Metadata: commonModels.JSONMap{},
	}
	if err := repo.CreateResult(context.Background(), result); err != nil {
		t.Fatalf("create building PPTX result: %v", err)
	}

	recovered, err := repo.RecoverExpiredExecutions(context.Background(), createdAt.Add(32*time.Second), 100)
	if err != nil {
		t.Fatalf("recover expired PPTX execution: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered executions = %d, want 1", recovered)
	}
	var storedExecution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", execution.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("load recovered execution: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusFailed || storedExecution.LeaseToken != nil ||
		storedExecution.ErrorDetails["code"] != "manager.pptx_pdf.lease_expired" {
		t.Fatalf("recovered execution = %#v", storedExecution)
	}
	var storedTask models.PPTXPDFTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load recovered task: %v", err)
	}
	if storedTask.LastExecutionStatus == nil || *storedTask.LastExecutionStatus != commonExecution.ExecutionStatusFailed {
		t.Fatalf("recovered task status = %v", storedTask.LastExecutionStatus)
	}
	var storedResult models.PPTXPDF
	if err := db.First(&storedResult, result.ID).Error; err != nil {
		t.Fatalf("load recovered result: %v", err)
	}
	if storedResult.Status != models.PPTXPDFStatusFailed || storedResult.ErrorMessage == "" {
		t.Fatalf("recovered result = %#v", storedResult)
	}
}

func newPPTXPDFExecutionLeaseTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newTileCacheExecutionRepositoryTestDB(t)
	for _, statement := range []string{
		`CREATE TABLE manager.pptx_pdf_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
			description TEXT, enabled BOOLEAN, schedule TEXT, next_run_at DATETIME,
			item_fingerprint TEXT NOT NULL, artifact_variant TEXT NOT NULL, source_engine_id INTEGER NOT NULL,
			item_id INTEGER NOT NULL, locator TEXT NOT NULL, source_version TEXT NOT NULL,
			source_size_bytes INTEGER NOT NULL, last_run_at DATETIME, last_execution_id TEXT,
			last_execution_status TEXT, config JSON, created_by INTEGER, created_at DATETIME,
			updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE manager.pptx_pdf (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL,
			artifact_variant TEXT NOT NULL, source_version TEXT NOT NULL, source_engine_id INTEGER NOT NULL,
			item_id INTEGER NOT NULL, locator TEXT NOT NULL, task_id INTEGER, last_execution_id TEXT,
			storage_ref TEXT NOT NULL, file_name TEXT NOT NULL, size_bytes INTEGER NOT NULL,
			page_count INTEGER NOT NULL, content_url TEXT, status TEXT NOT NULL, metadata JSON,
			error_message TEXT, created_by INTEGER, created_at DATETIME, updated_at DATETIME,
			deleted_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create PPTX execution lease table: %v", err)
		}
	}
	return db
}

func createPPTXPDFExecutionLeaseTestTask(t *testing.T, db *gorm.DB, tenantID uint) models.PPTXPDFTask {
	t.Helper()
	task := models.PPTXPDFTask{
		TenantID: tenantID, Name: "slides.pptx", Enabled: true, ItemFingerprint: "pptx-fingerprint",
		ArtifactVariant: models.PPTXPDFArtifactVariant, SourceEngineID: 12, ItemID: 77,
		Locator:       "addp://engine/12/path/doc/slides.pptx?type=object&item_id=77",
		SourceVersion: "version-1", SourceSizeBytes: 1024, Config: commonModels.JSONMap{"version": 1},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create PPTX execution lease task: %v", err)
	}
	return task
}
