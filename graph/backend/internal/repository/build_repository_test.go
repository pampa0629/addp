package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/graph/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBuildExecutionLifecycleAndRerunClaimAreAtomic(t *testing.T) {
	db := newBuildRepositoryTestDB(t)
	repo := NewBuildRepository(db)
	task, material := createBuildRepositoryTestTask(t, db, 7, models.BuildStatusPending)
	createdAt := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	exec := newGraphRepositoryTestExecution("graph-run-1", 7, createdAt)

	claimed, materials, err := repo.ClaimExecution(context.Background(), task.ID, task.GraphID, task.TenantID, exec, BuildExecutionClaimRun)
	if err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	if claimed.Status != models.BuildStatusPending || claimed.ExecutionID != exec.ExecutionID || claimed.StartedAt != nil {
		t.Fatalf("claimed task status=%s execution_id=%s started_at=%v", claimed.Status, claimed.ExecutionID, claimed.StartedAt)
	}
	if len(materials) != 1 || materials[0].ID != material.ID {
		t.Fatalf("claimed materials = %#v", materials)
	}
	var storedExecution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("load pending execution: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusPending || storedExecution.StartedAt != nil {
		t.Fatalf("pending execution status=%s started_at=%v", storedExecution.Status, storedExecution.StartedAt)
	}

	duplicate := newGraphRepositoryTestExecution("graph-run-duplicate", 7, createdAt)
	if _, _, err := repo.ClaimExecution(context.Background(), task.ID, task.GraphID, task.TenantID, duplicate, BuildExecutionClaimRun); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("duplicate ClaimExecution error = %v, want conflict", err)
	}

	startedAt := createdAt.Add(time.Minute)
	if err := repo.StartExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, startedAt); err != nil {
		t.Fatalf("StartExecution: %v", err)
	}
	completedAt := startedAt.Add(time.Minute)
	if err := repo.FinishExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID,
		map[string]interface{}{"status": models.BuildStatusSuccess, "completed_at": completedAt},
		map[string]interface{}{
			"status": commonExecution.ExecutionStatusSuccess, "completed_at": completedAt,
			"execution_time_ms": completedAt.Sub(startedAt).Milliseconds(), "progress": 100,
		}); err != nil {
		t.Fatalf("FinishExecution: %v", err)
	}

	pendingReview := models.ReviewItem{
		TaskID: task.ID, MaterialID: material.ID, TenantID: task.TenantID, GraphID: task.GraphID,
		ItemType: models.ReviewItemEntity, Content: []byte(`{}`), Confidence: 0.5, Status: models.ReviewStatusPending,
	}
	approvedReview := pendingReview
	approvedReview.Status = models.ReviewStatusApproved
	if err := db.Create(&pendingReview).Error; err != nil {
		t.Fatalf("create pending review: %v", err)
	}
	if err := db.Create(&approvedReview).Error; err != nil {
		t.Fatalf("create approved review: %v", err)
	}
	if err := db.Model(&models.BuildMaterial{}).Where("id = ?", material.ID).Updates(map[string]interface{}{
		"status": models.BuildMaterialStatusCompleted, "processed_chunks": 4, "total_chunks": 4,
	}).Error; err != nil {
		t.Fatalf("prepare completed material: %v", err)
	}

	rerun := newGraphRepositoryTestExecution("graph-rerun-1", 7, completedAt.Add(time.Minute))
	rerunTask, rerunMaterials, err := repo.ClaimExecution(context.Background(), task.ID, task.GraphID, task.TenantID, rerun, BuildExecutionClaimRerun)
	if err != nil {
		t.Fatalf("rerun ClaimExecution: %v", err)
	}
	if rerunTask.ExecutionID != rerun.ExecutionID || rerunTask.Status != models.BuildStatusPending || rerunTask.StartedAt != nil {
		t.Fatalf("rerun task = %#v", rerunTask)
	}
	if len(rerunMaterials) != 1 || rerunMaterials[0].Status != models.BuildMaterialStatusPending ||
		rerunMaterials[0].ProcessedChunks != 0 || rerunMaterials[0].TotalChunks != 0 {
		t.Fatalf("rerun materials = %#v", rerunMaterials)
	}
	var pendingCount, approvedCount int64
	if err := db.Model(&models.ReviewItem{}).Where("task_id = ? AND status = ?", task.ID, models.ReviewStatusPending).Count(&pendingCount).Error; err != nil {
		t.Fatalf("count pending reviews: %v", err)
	}
	if err := db.Model(&models.ReviewItem{}).Where("task_id = ? AND status = ?", task.ID, models.ReviewStatusApproved).Count(&approvedCount).Error; err != nil {
		t.Fatalf("count approved reviews: %v", err)
	}
	if pendingCount != 0 || approvedCount != 1 {
		t.Fatalf("review counts pending=%d approved=%d, want 0/1", pendingCount, approvedCount)
	}
}

func TestBuildStartRollsBackWhenTaskCannotAdvance(t *testing.T) {
	db := newBuildRepositoryTestDB(t)
	repo := NewBuildRepository(db)
	task, _ := createBuildRepositoryTestTask(t, db, 8, models.BuildStatusPending)
	createdAt := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	exec := newGraphRepositoryTestExecution("graph-start-rollback", 8, createdAt)
	if _, _, err := repo.ClaimExecution(context.Background(), task.ID, task.GraphID, task.TenantID, exec, BuildExecutionClaimRun); err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	if err := db.Model(&models.BuildTask{}).Where("id = ?", task.ID).Update("execution_id", "other").Error; err != nil {
		t.Fatalf("change task execution id: %v", err)
	}

	err := repo.StartExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, createdAt.Add(time.Minute))
	if !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("StartExecution error = %v, want conflict", err)
	}
	var stored commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&stored).Error; err != nil {
		t.Fatalf("load execution after rollback: %v", err)
	}
	if stored.Status != commonExecution.ExecutionStatusPending || stored.StartedAt != nil {
		t.Fatalf("execution changed despite task rollback: status=%s started_at=%v", stored.Status, stored.StartedAt)
	}
}

func newBuildRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}
	if err := db.Exec(graphRepositoryExecutionTableSQL).Error; err != nil {
		t.Fatalf("create common execution test table: %v", err)
	}
	if err := db.AutoMigrate(&models.BuildTask{}, &models.BuildMaterial{}, &models.ReviewItem{}); err != nil {
		t.Fatalf("migrate graph execution test tables: %v", err)
	}
	return db
}

const graphRepositoryExecutionTableSQL = `CREATE TABLE common.task_executions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	tenant_id INTEGER NOT NULL,
	execution_id TEXT NOT NULL UNIQUE,
	module TEXT NOT NULL,
	task_type TEXT NOT NULL,
	source TEXT NOT NULL DEFAULT '',
	source_task_id TEXT,
	source_task_name TEXT,
	parent_execution_id TEXT,
	status TEXT NOT NULL,
	progress INTEGER,
	current_step TEXT,
	trigger_type TEXT NOT NULL,
	triggered_by INTEGER,
	actor_principal_id TEXT,
	actor_tenant_membership_id INTEGER,
	issued_authorization_version INTEGER,
	execution_authorization_id TEXT,
	authorization_effects JSON,
	authorization_expires_at DATETIME,
	execution_config JSON,
	error_details JSON,
	metadata JSON,
	execution_time_ms INTEGER,
	rows_affected INTEGER,
	records_read INTEGER,
	records_written INTEGER,
	bytes_read INTEGER,
	bytes_written INTEGER,
	started_at DATETIME,
	completed_at DATETIME,
	created_at DATETIME,
	updated_at DATETIME
)`

func createBuildRepositoryTestTask(t *testing.T, db *gorm.DB, tenantID uint, status string) (models.BuildTask, models.BuildMaterial) {
	t.Helper()
	task := models.BuildTask{
		TenantID: tenantID, GraphID: 42, Name: "kg-build", Status: status,
		ConfidenceThreshold: 0.7, ChunkSize: 1000, ChunkOverlap: 200, DocContextSize: 200,
		Stats: []byte(`{}`),
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create build task: %v", err)
	}
	material := models.BuildMaterial{
		TaskID: task.ID, TenantID: tenantID, GraphID: task.GraphID, Type: "document",
		FileName: "source.txt", FilePath: "build/source.txt", Status: models.BuildMaterialStatusPending,
		Stats: []byte(`{}`),
	}
	if err := db.Create(&material).Error; err != nil {
		t.Fatalf("create build material: %v", err)
	}
	return task, material
}

func newGraphRepositoryTestExecution(executionID string, tenantID int, createdAt time.Time) *commonExecution.TaskExecution {
	return &commonExecution.TaskExecution{
		ExecutionID: executionID, TenantID: tenantID, Module: commonExecution.ModuleGraph,
		TaskType: commonExecution.TaskTypeKGBuild, Source: commonExecution.ModuleGraph,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		ExecutionConfig: commonModels.JSONMap{}, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
}
