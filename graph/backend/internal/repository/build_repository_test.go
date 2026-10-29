package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/graph/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func graphRepositoryUserContext(tenant uint) context.Context {
	return commonExecution.WithUserOrigin(context.Background(), authtest.NewTenantUserAuthContext(strconv.FormatUint(uint64(tenant), 10), "7", nil))
}

func newBuildRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	if err := MigrateGraphTables(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func createBuildRepositoryTestTask(t *testing.T, db *gorm.DB, tenant uint, status string) (models.BuildTask, models.BuildMaterial) {
	t.Helper()
	ontology := models.Ontology{TenantID: tenant, Name: "immutable"}
	if err := db.Create(&ontology).Error; err != nil {
		t.Fatal(err)
	}
	kg := models.KnowledgeGraph{TenantID: tenant, OntologyID: ontology.ID, EngineID: 1, Name: "fixture", IndexManagement: models.GraphIndexesExternal, CatalogPath: plugin.EngineCatalogPath{Version: plugin.EngineCatalogPathVersion, EngineID: 1, Segments: []plugin.EngineCatalogSegment{{Term: "server", Kind: "server"}, {Term: "graph", Kind: "graph", Name: t.Name()}}}}
	if err := db.Create(&kg).Error; err != nil {
		t.Fatal(err)
	}
	task := models.BuildTask{TenantID: tenant, GraphID: kg.ID, Name: "fixture", Status: status, ConfidenceThreshold: .7, ChunkSize: 1000, ChunkOverlap: 200, DocContextSize: 200, Stats: []byte(`{}`)}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	m := models.BuildMaterial{TenantID: tenant, GraphID: kg.ID, TaskID: task.ID, Type: "document", FileName: "source.txt", FilePath: "build/source.txt", Status: models.BuildMaterialStatusPending, Stats: []byte(`{}`)}
	if err := db.Create(&m).Error; err != nil {
		t.Fatal(err)
	}
	return task, m
}

func newGraphRepositoryTestExecution(id string, tenant int, _ time.Time) *commonExecution.TaskExecution {
	return &commonExecution.TaskExecution{ExecutionID: id, TenantID: tenant, Module: commonExecution.ModuleGraph, TaskType: commonExecution.TaskTypeKGBuild, Source: commonExecution.ModuleGraph, Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual}
}

func enqueueRepositoryFixture(ctx context.Context, repo *BuildRepository, task models.BuildTask, e *commonExecution.TaskExecution, mode BuildExecutionClaimMode) (*models.BuildTask, []models.BuildMaterial, error) {
	materials, err := repo.ListMaterials(task.ID, task.TenantID)
	if err != nil {
		return nil, nil, err
	}
	var kg models.KnowledgeGraph
	if err := repo.db.Where("id = ? AND tenant_id = ?", task.GraphID, task.TenantID).First(&kg).Error; err != nil {
		return nil, nil, err
	}
	frozen := make([]models.FrozenBuildMaterial, len(materials))
	for i, m := range materials {
		frozen[i] = models.FrozenBuildMaterial{Material: m, SHA256: strings.Repeat("a", 64), Size: 10}
	}
	return repo.EnqueueExecution(ctx, task.ID, task.GraphID, task.TenantID, e, mode, frozen, 1, kg)
}

func TestBuildExecutionLifecycleAndRerunClaimAreAtomic(t *testing.T) {
	db := newBuildRepositoryTestDB(t)
	repo := NewBuildRepository(db)
	task, material := createBuildRepositoryTestTask(t, db, 7, models.BuildStatusPending)
	ctx := graphRepositoryUserContext(7)
	e := newGraphRepositoryTestExecution("graph-run", 7, time.Time{})
	queued, mats, err := enqueueRepositoryFixture(ctx, repo, task, e, BuildExecutionClaimRun)
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != models.BuildStatusPending || queued.StartedAt != nil || len(mats) != 1 || e.Attempt != 0 || e.LeaseToken != nil {
		t.Fatalf("enqueue created runtime facts: %+v %+v", queued, e)
	}
	if _, _, err := enqueueRepositoryFixture(ctx, repo, task, newGraphRepositoryTestExecution("duplicate", 7, time.Time{}), BuildExecutionClaimRun); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("duplicate=%v", err)
	}
	claimed, lease, runtime, control, err := repo.ClaimBuild(ctx, *e, "backend-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Attempt != 1 || runtime.StartedAt == nil || len(control.Snapshot) == 0 {
		t.Fatal("claim did not freeze ownership/input")
	}
	if err := repo.CompleteBuild(commonExecution.ContextWithLease(ctx, *lease), runtime, models.BuildStatusSuccess, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&material).Updates(map[string]interface{}{"processed_chunks": 3, "total_chunks": 3, "status": models.BuildMaterialStatusCompleted}).Error; err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{models.ReviewStatusPending, models.ReviewStatusApproved} {
		if err := db.Create(&models.ReviewItem{TenantID: 7, GraphID: task.GraphID, TaskID: task.ID, Content: []byte(`{}`), Status: status}).Error; err != nil {
			t.Fatal(err)
		}
	}
	run := newGraphRepositoryTestExecution("rerun", 7, time.Time{})
	if _, _, err := enqueueRepositoryFixture(ctx, repo, task, run, BuildExecutionClaimRerun); err != nil {
		t.Fatal(err)
	}
	var reset models.BuildMaterial
	if err := db.First(&reset, material.ID).Error; err != nil {
		t.Fatal(err)
	}
	var pending, approved int64
	db.Model(&models.ReviewItem{}).Where("task_id = ? AND status = ?", task.ID, models.ReviewStatusPending).Count(&pending)
	db.Model(&models.ReviewItem{}).Where("task_id = ? AND status = ?", task.ID, models.ReviewStatusApproved).Count(&approved)
	if reset.ProcessedChunks != 0 || reset.TotalChunks != 0 || pending != 0 || approved != 1 {
		t.Fatalf("reset lost reviewed facts: %+v pending=%d approved=%d", reset, pending, approved)
	}
	if _, _, _, _, err := repo.ClaimBuild(ctx, *e, "old", time.Minute); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("old claim=%v", err)
	}
}

func TestBuildExecutionInheritsOrchestratorParentActor(t *testing.T) {
	db := newBuildRepositoryTestDB(t)
	repo := NewBuildRepository(db)
	task, _ := createBuildRepositoryTestTask(t, db, 7, models.BuildStatusPending)
	principal, membership, version := int64(41), int64(51), int64(6)
	by := 41
	parent := commonExecution.TaskExecution{ExecutionID: "66666666-6666-4666-8666-666666666666", TenantID: 7, Module: commonExecution.ModuleOrchestrator, TaskType: commonExecution.TaskTypeOrchestration, Source: commonExecution.ModuleOrchestrator, Status: commonExecution.ExecutionStatusRunning, TriggerType: commonExecution.TriggerTypeManual, TriggeredBy: &by, ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version}
	if err := commonExecution.FreezeDisplayLanguage(&parent, "en"); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	child := newGraphRepositoryTestExecution("graph-child", 7, time.Time{})
	child.Source = commonExecution.ModuleOrchestrator
	child.ParentExecutionID = &parent.ExecutionID
	if _, _, err := enqueueRepositoryFixture(context.Background(), repo, task, child, BuildExecutionClaimRun); err != nil {
		t.Fatal(err)
	}
	if child.ActorPrincipalID == nil || *child.ActorPrincipalID != principal || *child.ActorTenantMembershipID != membership || *child.IssuedAuthorizationVersion != version {
		t.Fatal("parent identity was not inherited")
	}
}

func TestBuildStartRollsBackWhenTaskCannotAdvance(t *testing.T) {
	db := newBuildRepositoryTestDB(t)
	repo := NewBuildRepository(db)
	task, _ := createBuildRepositoryTestTask(t, db, 7, models.BuildStatusPending)
	ctx := graphRepositoryUserContext(7)
	e := newGraphRepositoryTestExecution("roll-back", 7, time.Time{})
	if _, _, err := enqueueRepositoryFixture(ctx, repo, task, e, BuildExecutionClaimRun); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&task).Update("execution_id", "replacement").Error; err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := repo.ClaimBuild(ctx, *e, "backend", time.Minute); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("claim=%v", err)
	}
	var row commonExecution.TaskExecution
	db.Where("execution_id = ?", e.ExecutionID).First(&row)
	if row.Status != commonExecution.ExecutionStatusPending || row.Attempt != 0 || row.StartedAt != nil {
		t.Fatal("failed claim advanced E")
	}
}

func TestExpiredGraphLeaseRejectsPrivateWritesAndPreservesEvidence(t *testing.T) {
	db := newBuildRepositoryTestDB(t)
	repo := NewBuildRepository(db)
	task, m := createBuildRepositoryTestTask(t, db, 7, models.BuildStatusPending)
	ctx := graphRepositoryUserContext(7)
	e := newGraphRepositoryTestExecution("expire", 7, time.Time{})
	if _, _, err := enqueueRepositoryFixture(ctx, repo, task, e, BuildExecutionClaimRun); err != nil {
		t.Fatal(err)
	}
	_, lease, _, _, err := repo.ClaimBuild(ctx, *e, "backend", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	owned := commonExecution.ContextWithLease(ctx, *lease)
	var kg models.KnowledgeGraph
	if err := db.First(&kg, task.GraphID).Error; err != nil {
		t.Fatal(err)
	}
	descriptor, _ := json.Marshal(models.GraphOperationTarget{EngineID: kg.EngineID, EngineVersion: 1, CatalogPath: kg.CatalogPath, IndexManagement: kg.IndexManagement})
	targetHash := sha256.Sum256(descriptor)
	op := models.GraphOperation{ID: "op", TenantID: 7, GraphID: task.GraphID, ExecutionID: e.ExecutionID, Attempt: lease.Attempt, Kind: "graph", TargetDigest: hex.EncodeToString(targetHash[:]), TargetDescriptor: string(descriptor), RequestDigest: strings.Repeat("b", 64)}
	if err := repo.BeginBuildOperation(owned, &op); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", e.ExecutionID).Update("lease_expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	m.ProcessedChunks = 99
	if err := repo.UpdateMaterial(owned, &m); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("stale material write=%v", err)
	}
	if err := repo.FailExpiredBuilds(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.AppendLateEvidence(owned, op, strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	var actual models.BuildMaterial
	db.First(&actual, m.ID)
	var result commonExecution.TaskExecution
	db.Where("execution_id = ?", e.ExecutionID).First(&result)
	var operation models.GraphOperation
	db.First(&operation, "id = ?", op.ID)
	if actual.ProcessedChunks != 0 || result.Status != commonExecution.ExecutionStatusFailed || operation.State != models.GraphOperationUncertain || operation.LateEvidenceDigest != strings.Repeat("c", 64) {
		t.Fatalf("stale writes escaped fence: %+v %+v %+v", actual, result, operation)
	}
	if err := CheckGraphOpen(db, task.GraphID, 7); !errors.Is(err, ErrGraphAdmissionClosed) {
		t.Fatalf("unknown effects reopened: %v", err)
	}
}
