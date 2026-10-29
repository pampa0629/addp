package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	execution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func queuedCancelFixture(t *testing.T) (*BuildService, *gorm.DB, models.BuildTask, *execution.TaskExecution) {
	t.Helper()
	db := setupGraphCleanupTestDB(t)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	ontology := models.Ontology{TenantID: 7, Name: "cancel"}
	if err := db.Create(&ontology).Error; err != nil {
		t.Fatal(err)
	}
	kg := models.KnowledgeGraph{TenantID: 7, OntologyID: ontology.ID, EngineID: 1, Name: "cancel", IndexManagement: models.GraphIndexesExternal}
	if err := db.Create(&kg).Error; err != nil {
		t.Fatal(err)
	}
	task := models.BuildTask{TenantID: 7, GraphID: kg.ID, Name: "cancel", Status: models.BuildStatusPending}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	mat := models.BuildMaterial{TenantID: 7, TaskID: task.ID, GraphID: kg.ID, Type: "document", FilePath: "build/source", Status: models.BuildMaterialStatusPending}
	if err := db.Create(&mat).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewBuildRepository(db)
	e := &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Module: execution.ModuleGraph, TaskType: execution.TaskTypeKGBuild, Source: execution.ModuleGraph, Status: execution.ExecutionStatusPending, TriggerType: execution.TriggerTypeManual}
	ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "7", nil))
	if _, _, err := repo.EnqueueExecution(ctx, task.ID, kg.ID, 7, e, repository.BuildExecutionClaimRun, []models.FrozenBuildMaterial{{Material: mat, SHA256: strings.Repeat("a", 64), Size: 1}}, 1, kg); err != nil {
		t.Fatal(err)
	}
	return &BuildService{buildRepo: repo}, db, task, e
}

func TestCancelTaskWithoutLocalRuntimePersistsIntent(t *testing.T) {
	svc, db, task, e := queuedCancelFixture(t)
	_, lease, runtime, _, err := svc.buildRepo.ClaimBuild(t.Context(), *e, "different-backend", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.CancelTask(t.Context(), task.ID, task.GraphID, 7, 7, e.ExecutionID, lease.Attempt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != execution.ExecutionStatusRunning {
		t.Fatal("intent pretended runtime stopped")
	}
	requested, err := svc.buildRepo.CancellationRequested(t.Context(), e.ExecutionID, 7)
	if err != nil || !requested {
		t.Fatalf("durable cancellation=%v %v", requested, err)
	}
	if err := svc.buildRepo.CompleteBuild(execution.ContextWithLease(t.Context(), *lease), runtime, models.BuildStatusSuccess, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	var actual execution.TaskExecution
	db.Where("execution_id = ?", e.ExecutionID).First(&actual)
	if actual.Status != execution.ExecutionStatusCancelled {
		t.Fatal("completion ignored accepted cancellation intent")
	}
}

func TestUnstartedCancellationDoesNotManufactureRuntimeOrEraseCheckpoints(t *testing.T) {
	svc, db, task, e := queuedCancelFixture(t)
	if err := db.Model(&models.BuildMaterial{}).Where("task_id = ?", task.ID).Update("processed_chunks", 3).Error; err != nil {
		t.Fatal(err)
	}
	result, err := svc.CancelTask(context.Background(), task.ID, task.GraphID, 7, 7, e.ExecutionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != execution.ExecutionStatusCancelled || result.Attempt != 0 || result.StartedAt != nil || result.ExecutionTimeMs != nil || result.LeaseToken != nil {
		t.Fatalf("unstarted cancellation=%+v", result)
	}
	var mat models.BuildMaterial
	db.Where("task_id = ?", task.ID).First(&mat)
	if mat.ProcessedChunks != 3 {
		t.Fatal("cancellation erased prior material facts")
	}
	if _, _, _, _, err := svc.buildRepo.ClaimBuild(t.Context(), *e, "late", time.Minute); err == nil {
		t.Fatal("cancelled pending execution was claimed")
	}
}
