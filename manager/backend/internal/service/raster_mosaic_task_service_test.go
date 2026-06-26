package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRasterMosaicTaskNormalizesInPlacePlacement(t *testing.T) {
	db := newRasterMosaicTaskServiceTestDB(t)
	taskSvc := NewRasterMosaicTaskService(repository.NewRasterMosaicRepository(db), commonExecution.NewTaskExecutionRepository(db))

	task := &models.RasterMosaicTask{
		TenantID: 7,
		Name:     "原 node mosaic",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"source": commonModels.JSONMap{
				"node_locator":     "addp://engine/26/path/rasters?type=node",
				"source_engine_id": uint(26),
			},
			"placement": commonModels.JSONMap{
				"mode": "in_place",
			},
		},
	}

	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create raster mosaic task: %v", err)
	}
	placement, ok := asJSONMap(task.Config["placement"])
	if !ok {
		t.Fatalf("placement = %#v, want object", task.Config["placement"])
	}
	if mode := stringFromConfig(placement["mode"]); mode != "in_place" {
		t.Fatalf("placement.mode = %q, want in_place", mode)
	}
	target, ok := asJSONMap(task.Config["target"])
	if !ok {
		t.Fatalf("target = %#v, want normalized object", task.Config["target"])
	}
	if locator := stringFromConfig(target["storage_locator"]); locator != "addp://engine/26/path/rasters?type=node" {
		t.Fatalf("target.storage_locator = %q, want source node locator", locator)
	}
	if engineID := uintFromConfig(target["target_engine_id"]); engineID != 26 {
		t.Fatalf("target.target_engine_id = %d, want 26", engineID)
	}
}

func TestRasterMosaicTaskNormalizesDetachedPlacement(t *testing.T) {
	db := newRasterMosaicTaskServiceTestDB(t)
	taskSvc := NewRasterMosaicTaskService(repository.NewRasterMosaicRepository(db), commonExecution.NewTaskExecutionRepository(db))

	task := &models.RasterMosaicTask{
		TenantID: 7,
		Name:     "独立 mosaic",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"source": commonModels.JSONMap{
				"node_locator":     "addp://engine/26/path/rasters?type=node",
				"source_engine_id": uint(26),
			},
			"placement": commonModels.JSONMap{
				"mode": "detached",
			},
			"target": commonModels.JSONMap{
				"storage_locator":  "addp://engine/27/path/mosaics?type=node",
				"target_engine_id": uint(27),
				"dataset_name":     "srtm_mosaic",
			},
		},
	}

	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create raster mosaic task: %v", err)
	}
	placement, ok := asJSONMap(task.Config["placement"])
	if !ok {
		t.Fatalf("placement = %#v, want object", task.Config["placement"])
	}
	if mode := stringFromConfig(placement["mode"]); mode != "detached" {
		t.Fatalf("placement.mode = %q, want detached", mode)
	}
	target, ok := asJSONMap(task.Config["target"])
	if !ok {
		t.Fatalf("target = %#v, want object", task.Config["target"])
	}
	if datasetName := stringFromConfig(target["dataset_name"]); datasetName != "srtm_mosaic" {
		t.Fatalf("target.dataset_name = %q, want srtm_mosaic", datasetName)
	}
}

func TestRasterMosaicTaskRejectsInvalidPlacementTarget(t *testing.T) {
	tests := []struct {
		name    string
		config  commonModels.JSONMap
		wantErr string
	}{
		{
			name: "missing placement",
			config: commonModels.JSONMap{
				"source": commonModels.JSONMap{
					"node_locator":     "addp://engine/26/path/rasters?type=node",
					"source_engine_id": uint(26),
				},
			},
			wantErr: "config.placement is required",
		},
		{
			name: "detached requires target",
			config: commonModels.JSONMap{
				"source": commonModels.JSONMap{
					"node_locator":     "addp://engine/26/path/rasters?type=node",
					"source_engine_id": uint(26),
				},
				"placement": commonModels.JSONMap{"mode": "detached"},
			},
			wantErr: "config.target is required when placement.mode is detached",
		},
		{
			name: "detached target must differ",
			config: commonModels.JSONMap{
				"source": commonModels.JSONMap{
					"node_locator":     "addp://engine/26/path/rasters?type=node",
					"source_engine_id": uint(26),
				},
				"placement": commonModels.JSONMap{"mode": "detached"},
				"target": commonModels.JSONMap{
					"storage_locator":  "addp://engine/26/path/rasters?type=node",
					"target_engine_id": uint(26),
				},
			},
			wantErr: "must differ from source.node_locator",
		},
		{
			name: "in place target must equal source",
			config: commonModels.JSONMap{
				"source": commonModels.JSONMap{
					"node_locator":     "addp://engine/26/path/rasters?type=node",
					"source_engine_id": uint(26),
				},
				"placement": commonModels.JSONMap{"mode": "in_place"},
				"target": commonModels.JSONMap{
					"storage_locator":  "addp://engine/26/path/other?type=node",
					"target_engine_id": uint(26),
				},
			},
			wantErr: "must equal source.node_locator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newRasterMosaicTaskServiceTestDB(t)
			taskSvc := NewRasterMosaicTaskService(repository.NewRasterMosaicRepository(db), commonExecution.NewTaskExecutionRepository(db))
			err := taskSvc.Create(context.Background(), &models.RasterMosaicTask{
				TenantID: 7,
				Name:     tt.name,
				Enabled:  true,
				Config:   tt.config,
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Create() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRasterMosaicRecordProgressEventUpdatesExecution(t *testing.T) {
	db := newRasterMosaicTaskServiceTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewRasterMosaicTaskService(repository.NewRasterMosaicRepository(db), taskExecRepo)
	startedAt := time.Now().Add(-time.Minute)
	currentStep := "开始"
	exec := &commonExecution.TaskExecution{
		TenantID:    7,
		ExecutionID: "mosaic-exec-1",
		Module:      commonExecution.ModuleManager,
		TaskType:    commonExecution.TaskTypeRasterMosaicGeneration,
		Source:      commonExecution.ModuleManager,
		Status:      commonExecution.ExecutionStatusRunning,
		Progress:    10,
		CurrentStep: &currentStep,
		TriggerType: commonExecution.TriggerTypeManual,
		StartedAt:   &startedAt,
		Metadata: commonModels.JSONMap{
			"keep": "value",
		},
	}
	if err := taskExecRepo.Create(context.Background(), exec); err != nil {
		t.Fatalf("create execution: %v", err)
	}

	overallProgress := 22
	fileProgress := 60
	if err := taskSvc.RecordProgressEvent(context.Background(), 7, "mosaic-exec-1", RasterMosaicProgressEvent{
		Phase:           "leaf_cog",
		Event:           "file_progress",
		TotalFiles:      100,
		ProcessedFiles:  25,
		FailedFiles:     1,
		CurrentFile:     "a.tif",
		FileProgress:    &fileProgress,
		OverallProgress: &overallProgress,
		Metadata: commonModels.JSONMap{
			"worker": "gdal",
		},
	}); err != nil {
		t.Fatalf("RecordProgressEvent: %v", err)
	}

	got, err := taskExecRepo.GetByExecutionID(context.Background(), "mosaic-exec-1", 7)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if got.Progress != 22 {
		t.Fatalf("progress = %d, want 22", got.Progress)
	}
	if got.CurrentStep == nil || *got.CurrentStep != "构建栅格 mosaic：leaf_cog 25/100" {
		t.Fatalf("current_step = %#v", got.CurrentStep)
	}
	if got.Metadata["keep"] != "value" {
		t.Fatalf("metadata.keep = %#v, want value", got.Metadata["keep"])
	}
	progressEvent, ok := got.Metadata["progress_event"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata.progress_event = %#v, want object", got.Metadata["progress_event"])
	}
	if progressEvent["phase"] != "leaf_cog" || progressEvent["event"] != "file_progress" {
		t.Fatalf("progress_event = %#v", progressEvent)
	}
	if progressEvent["file_progress"] != float64(60) || progressEvent["overall_progress"] != float64(22) {
		t.Fatalf("progress_event percentages = %#v", progressEvent)
	}
}

func TestRasterMosaicRecordProgressEventDoesNotMoveProgressBackwards(t *testing.T) {
	db := newRasterMosaicTaskServiceTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewRasterMosaicTaskService(repository.NewRasterMosaicRepository(db), taskExecRepo)
	if err := taskExecRepo.Create(context.Background(), &commonExecution.TaskExecution{
		TenantID:    7,
		ExecutionID: "mosaic-exec-2",
		Module:      commonExecution.ModuleManager,
		TaskType:    commonExecution.TaskTypeRasterMosaicGeneration,
		Source:      commonExecution.ModuleManager,
		Status:      commonExecution.ExecutionStatusRunning,
		Progress:    40,
		TriggerType: commonExecution.TriggerTypeManual,
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}

	overallProgress := 5
	if err := taskSvc.RecordProgressEvent(context.Background(), 7, "mosaic-exec-2", RasterMosaicProgressEvent{
		Phase:           "overview",
		Event:           "phase_progress",
		OverallProgress: &overallProgress,
	}); err != nil {
		t.Fatalf("RecordProgressEvent: %v", err)
	}
	got, err := taskExecRepo.GetByExecutionID(context.Background(), "mosaic-exec-2", 7)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if got.Progress != 40 {
		t.Fatalf("progress = %d, want 40", got.Progress)
	}
}

func TestRasterMosaicRecordProgressEventRejectsWrongExecution(t *testing.T) {
	db := newRasterMosaicTaskServiceTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewRasterMosaicTaskService(repository.NewRasterMosaicRepository(db), taskExecRepo)
	if err := taskExecRepo.Create(context.Background(), &commonExecution.TaskExecution{
		TenantID:    7,
		ExecutionID: "cog-exec-1",
		Module:      commonExecution.ModuleManager,
		TaskType:    commonExecution.TaskTypeRasterCOGGeneration,
		Source:      commonExecution.ModuleManager,
		Status:      commonExecution.ExecutionStatusRunning,
		TriggerType: commonExecution.TriggerTypeManual,
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}

	err := taskSvc.RecordProgressEvent(context.Background(), 7, "cog-exec-1", RasterMosaicProgressEvent{
		Phase: "leaf_cog",
		Event: "file_progress",
	})
	if !errors.Is(err, ErrRasterMosaicProgressTargetMismatch) {
		t.Fatalf("RecordProgressEvent error = %v, want ErrRasterMosaicProgressTargetMismatch", err)
	}
}

func TestRasterMosaicRecordProgressEventUsesTenantScope(t *testing.T) {
	db := newRasterMosaicTaskServiceTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewRasterMosaicTaskService(repository.NewRasterMosaicRepository(db), taskExecRepo)
	if err := taskExecRepo.Create(context.Background(), &commonExecution.TaskExecution{
		TenantID:    7,
		ExecutionID: "mosaic-exec-3",
		Module:      commonExecution.ModuleManager,
		TaskType:    commonExecution.TaskTypeRasterMosaicGeneration,
		Source:      commonExecution.ModuleManager,
		Status:      commonExecution.ExecutionStatusRunning,
		TriggerType: commonExecution.TriggerTypeManual,
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}

	err := taskSvc.RecordProgressEvent(context.Background(), 8, "mosaic-exec-3", RasterMosaicProgressEvent{
		Phase: "leaf_cog",
		Event: "file_progress",
	})
	if !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("RecordProgressEvent error = %v, want ErrNotFound", err)
	}
}

func TestRasterMosaicGenerationSubmitsMetaScanForDatasetRoot(t *testing.T) {
	db := newRasterMosaicTaskServiceTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewRasterMosaicTaskService(repository.NewRasterMosaicRepository(db), taskExecRepo)
	taskSvc.SetExecutor(fakeRasterMosaicExecutor{
		result: &RasterMosaicExecutionResult{
			ManifestLocator: "addp://engine/27/path/mosaics/srtm_mosaic/mosaic.addp.json?type=object",
			ManifestRef:     "mosaic.addp.json",
			IndexRef:        "index/source-index.json",
			OverviewRef:     "overview/global-overview.tif",
			LeafCount:       12,
			Metadata:        commonModels.JSONMap{"operator": "build_raster_mosaic"},
		},
	})
	scanSubmitter := &fakeRasterMosaicMetaScanSubmitter{
		run: &commonExecution.TaskExecution{
			ExecutionID: "scan-exec-1",
			Module:      commonExecution.ModuleMeta,
			TaskType:    commonExecution.TaskTypeScan,
			Status:      commonExecution.ExecutionStatusPending,
		},
	}
	taskSvc.SetMetaScanSubmitter(scanSubmitter)

	task := &models.RasterMosaicTask{
		TenantID: 7,
		Name:     "detached mosaic",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"source": commonModels.JSONMap{
				"node_locator":     "addp://engine/26/path/rasters?type=node",
				"source_engine_id": uint(26),
			},
			"placement": commonModels.JSONMap{"mode": "detached"},
			"target": commonModels.JSONMap{
				"storage_locator":  "addp://engine/27/path/mosaics?type=node",
				"target_engine_id": uint(27),
				"dataset_name":     "srtm_mosaic",
			},
		},
	}
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	startedAt := time.Now().Add(-time.Minute)
	if err := taskExecRepo.Create(context.Background(), &commonExecution.TaskExecution{
		TenantID:    7,
		ExecutionID: "mosaic-exec-scan",
		Module:      commonExecution.ModuleManager,
		TaskType:    commonExecution.TaskTypeRasterMosaicGeneration,
		Source:      commonExecution.ModuleManager,
		Status:      commonExecution.ExecutionStatusRunning,
		TriggerType: commonExecution.TriggerTypeManual,
		StartedAt:   &startedAt,
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}

	taskSvc.runRasterMosaicGeneration(context.Background(), task, "mosaic-exec-scan", startedAt)

	if scanSubmitter.tenantID == nil || *scanSubmitter.tenantID != 7 {
		t.Fatalf("scan tenant id = %#v, want 7", scanSubmitter.tenantID)
	}
	if scanSubmitter.opts.EngineID != 27 {
		t.Fatalf("scan engine_id = %d, want 27", scanSubmitter.opts.EngineID)
	}
	if len(scanSubmitter.opts.CatalogPaths) != 1 || scanSubmitter.opts.CatalogPaths[0] != "mosaics/srtm_mosaic" {
		t.Fatalf("scan catalog paths = %#v, want mosaics/srtm_mosaic", scanSubmitter.opts.CatalogPaths)
	}
	if scanSubmitter.opts.ScanDepth != "deep" || !scanSubmitter.opts.Force {
		t.Fatalf("scan options = %#v, want deep force", scanSubmitter.opts)
	}
	if scanSubmitter.opts.TriggerType != commonExecution.TriggerTypeManual || scanSubmitter.opts.Source != commonExecution.ModuleManager {
		t.Fatalf("scan trigger/source = %#v", scanSubmitter.opts)
	}

	got, err := taskExecRepo.GetByExecutionID(context.Background(), "mosaic-exec-scan", 7)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if got.Status != commonExecution.ExecutionStatusSuccess || got.Progress != 100 {
		t.Fatalf("execution status/progress = %s/%d, want success/100", got.Status, got.Progress)
	}
	metaScan, ok := got.Metadata["meta_scan"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata.meta_scan = %#v, want object", got.Metadata["meta_scan"])
	}
	if metaScan["execution_id"] != "scan-exec-1" || metaScan["catalog_path"] != "mosaics/srtm_mosaic" {
		t.Fatalf("metadata.meta_scan = %#v", metaScan)
	}
}

func TestRasterMosaicGenerationFailsWhenMetaScanSubmitFails(t *testing.T) {
	db := newRasterMosaicTaskServiceTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewRasterMosaicTaskService(repository.NewRasterMosaicRepository(db), taskExecRepo)
	taskSvc.SetExecutor(fakeRasterMosaicExecutor{
		result: &RasterMosaicExecutionResult{
			ManifestRef: "mosaic.addp.json",
			Metadata:    commonModels.JSONMap{},
		},
	})
	taskSvc.SetMetaScanSubmitter(&fakeRasterMosaicMetaScanSubmitter{err: errors.New("meta unavailable")})

	task := &models.RasterMosaicTask{
		TenantID: 7,
		Name:     "in place mosaic",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"source": commonModels.JSONMap{
				"node_locator":     "addp://engine/26/path/rasters?type=node",
				"source_engine_id": uint(26),
			},
			"placement": commonModels.JSONMap{"mode": "in_place"},
		},
	}
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	startedAt := time.Now().Add(-time.Minute)
	if err := taskExecRepo.Create(context.Background(), &commonExecution.TaskExecution{
		TenantID:    7,
		ExecutionID: "mosaic-exec-scan-failed",
		Module:      commonExecution.ModuleManager,
		TaskType:    commonExecution.TaskTypeRasterMosaicGeneration,
		Source:      commonExecution.ModuleManager,
		Status:      commonExecution.ExecutionStatusRunning,
		TriggerType: commonExecution.TriggerTypeManual,
		StartedAt:   &startedAt,
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}

	taskSvc.runRasterMosaicGeneration(context.Background(), task, "mosaic-exec-scan-failed", startedAt)

	got, err := taskExecRepo.GetByExecutionID(context.Background(), "mosaic-exec-scan-failed", 7)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if got.Status != commonExecution.ExecutionStatusFailed {
		t.Fatalf("execution status = %s, want failed", got.Status)
	}
	if got.ErrorDetails == nil || !strings.Contains(stringFromConfig(got.ErrorDetails["message"]), "meta unavailable") {
		t.Fatalf("error_details = %#v, want meta unavailable", got.ErrorDetails)
	}
}

type fakeRasterMosaicExecutor struct {
	result *RasterMosaicExecutionResult
	err    error
}

func (f fakeRasterMosaicExecutor) BuildRasterMosaic(ctx context.Context, req RasterMosaicExecutionRequest) (*RasterMosaicExecutionResult, error) {
	return f.result, f.err
}

type fakeRasterMosaicMetaScanSubmitter struct {
	opts     commonClient.MetaScanOptions
	tenantID *uint
	run      *commonExecution.TaskExecution
	err      error
}

func (f *fakeRasterMosaicMetaScanSubmitter) CreateManualScanRun(opts commonClient.MetaScanOptions) (*commonExecution.TaskExecution, error) {
	f.opts = opts
	return f.run, f.err
}

func (f *fakeRasterMosaicMetaScanSubmitter) SetTenantID(tenantID *uint) {
	if tenantID == nil {
		f.tenantID = nil
		return
	}
	value := *tenantID
	f.tenantID = &value
}

func newRasterMosaicTaskServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(sqlite.Open("file:"+dbName+"?mode=memory&cache=shared"), &gorm.Config{})
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
	if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
		t.Fatalf("attach manager schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE common.task_executions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		execution_id TEXT NOT NULL,
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
	)`).Error; err != nil {
		t.Fatalf("create task_executions table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.raster_mosaic_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		enabled BOOLEAN,
		last_execution_id TEXT,
		last_execution_status TEXT,
		last_run_at DATETIME,
		next_run_at DATETIME,
		schedule TEXT,
		created_by INTEGER,
		config JSON,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create raster_mosaic_tasks table: %v", err)
	}
	return db
}
