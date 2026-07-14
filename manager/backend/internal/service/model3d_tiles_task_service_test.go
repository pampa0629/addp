package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"gorm.io/gorm"
)

func TestModel3DTilesTaskServiceReusesSemanticTaskIdentity(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	createModel3DTilesTaskTableForTest(t, db)
	repo := repository.NewModel3DTilesRepository(db)
	svc := NewModel3DTilesTaskService(repo, nil)
	svc.SetBucket("manager")

	first := newModel3DTilesTaskForTest("S3M 快显", "fp-reuse", models.Model3DTilesTargetFormatS3M)
	if err := svc.Create(context.Background(), first); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	second := newModel3DTilesTaskForTest("S3M 快显-新名称", "fp-reuse", models.Model3DTilesTargetFormatS3M)
	if err := svc.Create(context.Background(), second); err != nil {
		t.Fatalf("second Create() error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("reused task id = %d, want %d", second.ID, first.ID)
	}
	if second.Name != "S3M 快显-新名称" {
		t.Fatalf("reused task name = %q", second.Name)
	}

	threeDTiles := newModel3DTilesTaskForTest("3D Tiles 快显", "fp-reuse", models.Model3DTilesTargetFormat3DTiles)
	if err := svc.Create(context.Background(), threeDTiles); err != nil {
		t.Fatalf("3D Tiles Create() error = %v", err)
	}
	if threeDTiles.ID == first.ID {
		t.Fatalf("different target formats reused task id %d", first.ID)
	}

	var count int64
	if err := db.Model(&models.Model3DTilesTask{}).Count(&count).Error; err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 2 {
		t.Fatalf("task count = %d, want 2", count)
	}
}

func TestModel3DTilesTaskServiceRepeatedExecutionRefreshesCurrentResult(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	createModel3DTilesTaskTableForTest(t, db)
	createModel3DTilesResultTableForTest(t, db)
	repo := repository.NewModel3DTilesRepository(db)
	execRepo := commonExecution.NewTaskExecutionRepository(db)
	svc := NewModel3DTilesTaskService(repo, execRepo)
	svc.SetBucket("manager")
	svc.SetExecutor(staticModel3DTilesExecutor{})
	task := newModel3DTilesTaskForTest("S3M 快显", "fp-repeat", models.Model3DTilesTargetFormatS3M)
	if err := svc.Create(context.Background(), task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	firstExecutionID, err := svc.Execute(context.Background(), task.ID, task.TenantID, "manual", "manager", nil)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	waitForModel3DTilesExecution(t, execRepo, firstExecutionID, int(task.TenantID))
	firstResult, err := repo.GetCurrentResult(context.Background(), task.TenantID, "fp-repeat", models.Model3DTilesTargetFormatS3M)
	if err != nil || firstResult == nil {
		t.Fatalf("first result = %#v, %v", firstResult, err)
	}

	secondExecutionID, err := svc.Execute(context.Background(), task.ID, task.TenantID, "manual", "manager", nil)
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	waitForModel3DTilesExecution(t, execRepo, secondExecutionID, int(task.TenantID))
	secondResult, err := repo.GetCurrentResult(context.Background(), task.TenantID, "fp-repeat", models.Model3DTilesTargetFormatS3M)
	if err != nil || secondResult == nil {
		t.Fatalf("second result = %#v, %v", secondResult, err)
	}
	if firstExecutionID == secondExecutionID {
		t.Fatalf("execution id was reused: %s", firstExecutionID)
	}
	if secondResult.ID != firstResult.ID {
		t.Fatalf("result id changed from %d to %d", firstResult.ID, secondResult.ID)
	}
	if secondResult.LastExecutionID == nil || *secondResult.LastExecutionID != secondExecutionID {
		t.Fatalf("last execution id = %#v, want %s", secondResult.LastExecutionID, secondExecutionID)
	}
}

func TestModel3DTilesTaskServiceRejectsConcurrentExecution(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	createModel3DTilesTaskTableForTest(t, db)
	createModel3DTilesResultTableForTest(t, db)
	repo := repository.NewModel3DTilesRepository(db)
	execRepo := commonExecution.NewTaskExecutionRepository(db)
	svc := NewModel3DTilesTaskService(repo, execRepo)
	svc.SetBucket("manager")
	executor := &blockingModel3DTilesExecutor{started: make(chan struct{}), release: make(chan struct{})}
	svc.SetExecutor(executor)
	task := newModel3DTilesTaskForTest("S3M 快显", "fp-active", models.Model3DTilesTargetFormatS3M)
	if err := svc.Create(context.Background(), task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	firstExecutionID, err := svc.Execute(context.Background(), task.ID, task.TenantID, "manual", "manager", nil)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	<-executor.started
	if _, err := svc.Execute(context.Background(), task.ID, task.TenantID, "manual", "manager", nil); !errors.Is(err, ErrModel3DTilesTaskExecutionBusy) {
		t.Fatalf("concurrent Execute() error = %v", err)
	}
	close(executor.release)
	waitForModel3DTilesExecution(t, execRepo, firstExecutionID, int(task.TenantID))
}

type staticModel3DTilesExecutor struct{}

func (staticModel3DTilesExecutor) BuildModel3DTiles(_ context.Context, req Model3DTilesExecutionRequest) (*Model3DTilesExecutionResult, error) {
	return &Model3DTilesExecutionResult{
		StorageRef:  req.Config.Result.StorageRef,
		ManifestRef: model3DTilesManifestRef(req.Config.TargetFormat),
		FileCount:   2,
		SizeBytes:   1024,
		Metadata:    commonModels.JSONMap{"target_format": req.Config.TargetFormat},
	}, nil
}

type blockingModel3DTilesExecutor struct {
	started chan struct{}
	release chan struct{}
}

func (e *blockingModel3DTilesExecutor) BuildModel3DTiles(ctx context.Context, req Model3DTilesExecutionRequest) (*Model3DTilesExecutionResult, error) {
	close(e.started)
	select {
	case <-e.release:
		return staticModel3DTilesExecutor{}.BuildModel3DTiles(ctx, req)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func newModel3DTilesTaskForTest(name, fingerprint, targetFormat string) *models.Model3DTilesTask {
	return &models.Model3DTilesTask{
		TenantID: 7,
		Name:     name,
		Enabled:  true,
		Config: commonModels.JSONMap{
			"source": commonModels.JSONMap{
				"item_locator":      "addp://engine/26/path/models/osgb?type=item&item_id=77",
				"source_engine_id":  uint(26),
				"item_fingerprint":  fingerprint,
				"item_id":           uint(77),
				"source_size_bytes": int64(1024),
			},
			"target_format": targetFormat,
		},
	}
}

func createModel3DTilesTaskTableForTest(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`CREATE TABLE manager.model3d_tiles_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		enabled BOOLEAN NOT NULL,
		schedule TEXT,
		next_run_at DATETIME,
		last_run_at DATETIME,
		last_execution_id TEXT,
		last_execution_status TEXT,
		config JSON NOT NULL,
		created_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create model3d_tiles_tasks table: %v", err)
	}
}

func waitForModel3DTilesExecution(t *testing.T, repo *commonExecution.TaskExecutionRepository, executionID string, tenantID int) *commonExecution.TaskExecution {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		exec, err := repo.GetByExecutionID(context.Background(), executionID, tenantID)
		if err == nil && exec.Status != commonExecution.ExecutionStatusRunning {
			return exec
		}
		time.Sleep(10 * time.Millisecond)
	}
	exec, err := repo.GetByExecutionID(context.Background(), executionID, tenantID)
	if err != nil {
		t.Fatalf("load execution after wait: %v", err)
	}
	t.Fatalf("execution status still %s after wait", exec.Status)
	return nil
}

func TestModel3DTilesTaskNormalizesInfraResultByTargetFormat(t *testing.T) {
	for _, targetFormat := range []string{"3d_tiles", "s3m"} {
		t.Run(targetFormat, func(t *testing.T) {
			config := commonModels.JSONMap{
				"source": commonModels.JSONMap{
					"item_locator": "addp://engine/26/path/models/osgb?type=item&item_id=77", "source_engine_id": uint(26),
					"item_fingerprint": "fp-osgb-scene", "item_id": uint(77), "source_size_bytes": int64(1024),
				},
				"target_format": targetFormat,
			}
			cfg, err := normalizeModel3DTilesTaskConfig(config, "manager", 7)
			if err != nil {
				t.Fatalf("normalize model3d tiles config: %v", err)
			}
			if cfg.Source.Format != "osgb_scene" || cfg.TargetFormat != targetFormat {
				t.Fatalf("config = %+v", cfg)
			}
			if !strings.Contains(cfg.Result.StorageRef, "tenant_7/model3d-tiles/fp-osgb-scene/"+targetFormat) {
				t.Fatalf("storage_ref = %q", cfg.Result.StorageRef)
			}
		})
	}
}

type recordingModel3DTilesCleaner struct {
	storageRefs []string
	err         error
}

func (c *recordingModel3DTilesCleaner) DeleteByStorageRef(_ context.Context, storageRef string) error {
	c.storageRefs = append(c.storageRefs, storageRef)
	return c.err
}

func TestModel3DTilesTaskServiceDeletesManagedResultAndArtifact(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	createModel3DTilesResultTableForTest(t, db)
	repo := repository.NewModel3DTilesRepository(db)
	result := &models.Model3DTiles{
		TenantID: 7, ItemFingerprint: "fp-delete", Locator: "addp://engine/26/path/3d/site?type=item&item_id=77",
		SourceEngineID: 26, SourceFormat: "osgb_scene", TargetFormat: "s3m",
		StorageRef: "storage-ref-to-delete", ManifestRef: "config/scene.scp", Status: models.Model3DTilesStatusReady,
		Metadata: commonModels.JSONMap{},
	}
	if err := repo.CreateResult(context.Background(), result); err != nil {
		t.Fatalf("create result: %v", err)
	}
	cleaner := &recordingModel3DTilesCleaner{}
	svc := NewModel3DTilesTaskService(repo, nil)
	svc.SetCleaner(cleaner)

	if err := svc.DeleteResult(context.Background(), result.ID, result.TenantID); err != nil {
		t.Fatalf("DeleteResult() error = %v", err)
	}
	if len(cleaner.storageRefs) != 1 || cleaner.storageRefs[0] != result.StorageRef {
		t.Fatalf("cleaned storage refs = %#v", cleaner.storageRefs)
	}
	if current, err := repo.GetResult(context.Background(), result.ID, result.TenantID); err != nil || current != nil {
		t.Fatalf("GetResult() after delete = %#v, %v", current, err)
	}
	var deleted models.Model3DTiles
	if err := db.Unscoped().Where("id = ?", result.ID).First(&deleted).Error; err != nil {
		t.Fatalf("load deleted result: %v", err)
	}
	if deleted.Status != models.Model3DTilesStatusDeleted || !deleted.DeletedAt.Valid {
		t.Fatalf("deleted result status=%q deleted_at=%v", deleted.Status, deleted.DeletedAt.Valid)
	}
}

func TestModel3DTilesTaskServiceKeepsResultWhenArtifactCleanupFails(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	createModel3DTilesResultTableForTest(t, db)
	repo := repository.NewModel3DTilesRepository(db)
	result := &models.Model3DTiles{
		TenantID: 7, ItemFingerprint: "fp-cleanup-fail", SourceEngineID: 26, SourceFormat: "osgb_scene",
		TargetFormat: "3d_tiles", StorageRef: "storage-ref-cleanup-fail", ManifestRef: "tileset.json",
		Status: models.Model3DTilesStatusReady, Metadata: commonModels.JSONMap{},
	}
	if err := repo.CreateResult(context.Background(), result); err != nil {
		t.Fatalf("create result: %v", err)
	}
	cleanupErr := errors.New("cleanup failed")
	svc := NewModel3DTilesTaskService(repo, nil)
	svc.SetCleaner(&recordingModel3DTilesCleaner{err: cleanupErr})

	if err := svc.DeleteResult(context.Background(), result.ID, result.TenantID); !errors.Is(err, cleanupErr) {
		t.Fatalf("DeleteResult() error = %v, want cleanup failure", err)
	}
	if current, err := repo.GetResult(context.Background(), result.ID, result.TenantID); err != nil || current == nil {
		t.Fatalf("result was deleted after cleanup failure: %#v, %v", current, err)
	}
}

func TestModel3DTilesTaskRejectsInvalidSourceOrTargetFormat(t *testing.T) {
	base := commonModels.JSONMap{"source": commonModels.JSONMap{"item_locator": "addp://engine/26/path/models/tiles?type=item&item_id=77", "source_engine_id": uint(26), "item_fingerprint": "fp", "format": "3dtiles"}, "target_format": "3d_tiles"}
	if _, err := normalizeModel3DTilesTaskConfig(base, "manager", 7); err == nil || err.Error() != "model 3d tiles config.source.format must be osgb_scene" {
		t.Fatalf("source error = %v", err)
	}
	base["source"].(commonModels.JSONMap)["format"] = "osgb_scene"
	base["target_format"] = "unknown"
	if _, err := normalizeModel3DTilesTaskConfig(base, "manager", 7); err == nil || err.Error() != "model3d tiles config.target_format must be 3d_tiles or s3m" {
		t.Fatalf("target error = %v", err)
	}
}

func TestModel3DTilesTaskServiceListsManagedResultsByFormatAndStatus(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	createModel3DTilesResultTableForTest(t, db)
	repo := repository.NewModel3DTilesRepository(db)
	now := time.Now()
	for _, result := range []*models.Model3DTiles{
		{TenantID: 7, ItemFingerprint: "fp-baita", Locator: "addp://engine/26/path/3d/baita?type=file&item_id=1", SourceEngineID: 26, SourceFormat: "osgb_scene", TargetFormat: "s3m", StorageRef: "s3m-ref", ManifestRef: "config/scene.scp", Status: "ready", Metadata: commonModels.JSONMap{}, UpdatedAt: now},
		{TenantID: 7, ItemFingerprint: "fp-baita", Locator: "addp://engine/26/path/3d/baita?type=file&item_id=1", SourceEngineID: 26, SourceFormat: "osgb_scene", TargetFormat: "3d_tiles", StorageRef: "tiles-ref", ManifestRef: "tileset.json", Status: "ready", Metadata: commonModels.JSONMap{}, UpdatedAt: now.Add(-time.Minute)},
		{TenantID: 8, ItemFingerprint: "fp-baita", Locator: "addp://engine/26/path/3d/baita?type=file&item_id=1", SourceEngineID: 26, SourceFormat: "osgb_scene", TargetFormat: "s3m", StorageRef: "other-tenant", ManifestRef: "config/scene.scp", Status: "ready", Metadata: commonModels.JSONMap{}, UpdatedAt: now},
	} {
		if err := db.Create(result).Error; err != nil {
			t.Fatalf("create model3d tiles result: %v", err)
		}
	}

	results, total, err := NewModel3DTilesTaskService(repo, nil).ListResults(context.Background(), repository.Model3DTilesFilter{
		TenantID: 7, TargetFormat: "s3m", Status: "ready", Q: "baita", Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListResults() error = %v", err)
	}
	if total != 1 || len(results) != 1 || results[0].TargetFormat != "s3m" || results[0].TenantID != 7 {
		t.Fatalf("results = %#v total=%d, want one tenant-scoped ready S3M result", results, total)
	}
}
