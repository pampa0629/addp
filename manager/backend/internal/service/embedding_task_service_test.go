package service

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEmbeddingTaskExecuteReusesSingleExecution(t *testing.T) {
	db := newEmbeddingTaskServiceTestDB(t)
	embeddingRepo := repository.NewEmbeddingRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	provider := testEmbeddingConfigurationProvider("qwen3-vl-embedding", 2560, 10)
	embeddingSvc := &EmbeddingService{
		vectorRepo:            embeddingRepo,
		taskExecRepo:          taskExecRepo,
		configurationProvider: provider,
		log:                   slog.New(slog.NewTextHandler(os.Stdout, nil)),
	}
	taskSvc := NewEmbeddingTaskService(embeddingRepo, embeddingSvc, taskExecRepo, provider)

	task := &models.EmbeddingTask{
		TenantID: 7,
		Name:     "目录向量化",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{
				"scope":     "node",
				"engine_id": 11,
				"node_id":   23,
				"locator":   "addp://engine/11/path/datasets?type=directory&node_id=23",
				"recursive": true,
			},
			"filters": commonModels.JSONMap{
				"max_file_size_mb": 10,
			},
			"embedding": commonModels.JSONMap{
				"model":     "qwen3-vl-embedding",
				"dimension": 2560,
			},
		},
	}
	if err := embeddingRepo.CreateEmbeddingTask(context.Background(), task); err != nil {
		t.Fatalf("create embedding task: %v", err)
	}

	parentExecutionID := "parent-execution"
	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleOrchestrator, &parentExecutionID)
	if err != nil {
		t.Fatalf("execute embedding task: %v", err)
	}

	exec := waitForEmbeddingTaskExecution(t, taskExecRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusFailed {
		t.Fatalf("execution status = %s, want failed", exec.Status)
	}
	if exec.Module != commonExecution.ModuleManager || exec.TaskType != commonExecution.TaskTypeEmbedding {
		t.Fatalf("execution owner = %s/%s, want manager/embedding", exec.Module, exec.TaskType)
	}
	if exec.Source != commonExecution.ModuleOrchestrator {
		t.Fatalf("execution source = %s, want orchestrator", exec.Source)
	}
	if exec.ParentExecutionID == nil || *exec.ParentExecutionID != parentExecutionID {
		t.Fatalf("parent execution = %#v, want %s", exec.ParentExecutionID, parentExecutionID)
	}
	target, ok := exec.ExecutionConfig["target"].(map[string]interface{})
	if !ok {
		t.Fatalf("execution_config.target = %#v, want object", exec.ExecutionConfig["target"])
	}
	if target["scope"] != "node" {
		t.Fatalf("execution_config.target.scope = %v, want node", target["scope"])
	}
	if target["engine_id"] != float64(11) && target["engine_id"] != 11 {
		t.Fatalf("execution_config.target.engine_id = %v, want 11", target["engine_id"])
	}
	if target["node_id"] != float64(23) && target["node_id"] != 23 {
		t.Fatalf("execution_config.target.node_id = %v, want 23", target["node_id"])
	}

	var count int64
	if err := db.Model(&commonExecution.TaskExecution{}).
		Where("module = ? AND task_type = ?", commonExecution.ModuleManager, commonExecution.TaskTypeEmbedding).
		Count(&count).Error; err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if count != 1 {
		t.Fatalf("manager embedding execution count = %d, want 1", count)
	}

	var refreshed models.EmbeddingTask
	if err := db.First(&refreshed, task.ID).Error; err != nil {
		t.Fatalf("load refreshed task: %v", err)
	}
	if refreshed.LastExecutionID == nil || *refreshed.LastExecutionID != executionID {
		t.Fatalf("last_execution_id = %#v, want %s", refreshed.LastExecutionID, executionID)
	}
	if refreshed.LastExecutionStatus == nil || *refreshed.LastExecutionStatus != commonExecution.ExecutionStatusFailed {
		t.Fatalf("last_execution_status = %#v, want failed", refreshed.LastExecutionStatus)
	}
}

func TestEmbeddingTaskExecuteRecordsFailedExecutionWhenEmbeddingServiceUnavailable(t *testing.T) {
	db := newEmbeddingTaskServiceTestDB(t)
	embeddingRepo := repository.NewEmbeddingRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewEmbeddingTaskService(embeddingRepo, nil, taskExecRepo, nil)

	task := newEmbeddingTaskDefinition()
	if err := embeddingRepo.CreateEmbeddingTask(context.Background(), task); err != nil {
		t.Fatalf("create embedding task: %v", err)
	}

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil)
	if err != nil {
		t.Fatalf("execute embedding task: %v", err)
	}

	exec, err := taskExecRepo.GetByExecutionID(context.Background(), executionID, int(task.TenantID))
	if err != nil {
		t.Fatalf("load execution: %v", err)
	}
	if exec.Status != commonExecution.ExecutionStatusFailed {
		t.Fatalf("execution status = %s, want failed", exec.Status)
	}
	if exec.ErrorDetails["message"] != models.EmbeddingReasonEmbeddingServiceNil {
		t.Fatalf("error message = %#v, want %s", exec.ErrorDetails["message"], models.EmbeddingReasonEmbeddingServiceNil)
	}
}

func TestEmbeddingTaskSchedulerClaimsDueTaskAndCreatesScheduledExecution(t *testing.T) {
	db := newEmbeddingTaskServiceTestDB(t)
	embeddingRepo := repository.NewEmbeddingRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewEmbeddingTaskService(embeddingRepo, nil, taskExecRepo, nil)
	scheduler := NewEmbeddingTaskScheduler(taskSvc)

	task := newEmbeddingTaskDefinition()
	task.Schedule = "* * * * *"
	dueAt := time.Now().Add(-time.Minute)
	task.NextRunAt = &dueAt
	if err := embeddingRepo.CreateEmbeddingTask(context.Background(), task); err != nil {
		t.Fatalf("create embedding task: %v", err)
	}

	scheduler.runDueScheduledTasks(context.Background())

	var executions []*commonExecution.TaskExecution
	if err := db.Where("module = ? AND task_type = ?", commonExecution.ModuleManager, commonExecution.TaskTypeEmbedding).Find(&executions).Error; err != nil {
		t.Fatalf("list executions: %v", err)
	}
	if len(executions) != 1 {
		t.Fatalf("execution count = %d, want 1", len(executions))
	}
	if executions[0].TriggerType != commonExecution.TriggerTypeScheduled {
		t.Fatalf("trigger_type = %s, want scheduled", executions[0].TriggerType)
	}
	if executions[0].Status != commonExecution.ExecutionStatusFailed {
		t.Fatalf("status = %s, want failed because embedding service is unavailable", executions[0].Status)
	}

	refreshed, err := embeddingRepo.GetEmbeddingTask(context.Background(), task.ID, task.TenantID)
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if refreshed.NextRunAt == nil || !refreshed.NextRunAt.After(dueAt) {
		t.Fatalf("next_run_at = %#v, want after %s", refreshed.NextRunAt, dueAt)
	}
	if refreshed.LastExecutionID == nil || *refreshed.LastExecutionID != executions[0].ExecutionID {
		t.Fatalf("last_execution_id = %#v, want %s", refreshed.LastExecutionID, executions[0].ExecutionID)
	}
}

func TestEmbeddingTaskSchedulerRecordsFailedExecutionWhenInferenceRuntimeUnavailable(t *testing.T) {
	db := newEmbeddingTaskServiceTestDB(t)
	embeddingRepo := repository.NewEmbeddingRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	provider := testEmbeddingConfigurationProvider("current-model", 768, 10)
	taskSvc := NewEmbeddingTaskService(embeddingRepo, nil, taskExecRepo, provider)
	scheduler := NewEmbeddingTaskScheduler(taskSvc)

	task := newEmbeddingTaskDefinition()
	task.Schedule = "* * * * *"
	dueAt := time.Now().Add(-time.Minute)
	task.NextRunAt = &dueAt
	if err := embeddingRepo.CreateEmbeddingTask(context.Background(), task); err != nil {
		t.Fatalf("create embedding task: %v", err)
	}

	scheduler.runDueScheduledTasks(context.Background())

	var executions []*commonExecution.TaskExecution
	if err := db.Where("module = ? AND task_type = ?", commonExecution.ModuleManager, commonExecution.TaskTypeEmbedding).Find(&executions).Error; err != nil {
		t.Fatalf("list executions: %v", err)
	}
	if len(executions) != 1 {
		t.Fatalf("execution count = %d, want 1", len(executions))
	}
	exec := executions[0]
	if exec.TriggerType != commonExecution.TriggerTypeScheduled {
		t.Fatalf("trigger_type = %s, want scheduled", exec.TriggerType)
	}
	if exec.Status != commonExecution.ExecutionStatusFailed {
		t.Fatalf("status = %s, want failed", exec.Status)
	}
	if message, _ := exec.ErrorDetails["message"].(string); message != models.EmbeddingReasonEmbeddingServiceNil {
		t.Fatalf("error_details.message = %#v, want embedding service unavailable", exec.ErrorDetails["message"])
	}

	refreshed, err := embeddingRepo.GetEmbeddingTask(context.Background(), task.ID, task.TenantID)
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if refreshed.NextRunAt == nil || !refreshed.NextRunAt.After(dueAt) {
		t.Fatalf("next_run_at = %#v, want after %s", refreshed.NextRunAt, dueAt)
	}
	if refreshed.LastExecutionID == nil || *refreshed.LastExecutionID != exec.ExecutionID {
		t.Fatalf("last_execution_id = %#v, want %s", refreshed.LastExecutionID, exec.ExecutionID)
	}
	if refreshed.LastExecutionStatus == nil || *refreshed.LastExecutionStatus != commonExecution.ExecutionStatusFailed {
		t.Fatalf("last_execution_status = %#v, want failed", refreshed.LastExecutionStatus)
	}
}

func TestEmbeddingTaskDefinitionRemovesLegacyModelFields(t *testing.T) {
	db := newEmbeddingTaskServiceTestDB(t)
	embeddingRepo := repository.NewEmbeddingRepository(db)
	provider := testEmbeddingConfigurationProvider("current-model", 768, 10)
	taskSvc := NewEmbeddingTaskService(embeddingRepo, nil, nil, provider)

	task := newEmbeddingTaskDefinition()
	task.Config["embedding"] = commonModels.JSONMap{
		"model":     "other-model",
		"dimension": 768,
	}
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("Create error = %v", err)
	}
	embeddingCfg, ok := asJSONMap(task.Config["embedding"])
	if !ok || embeddingCfg["model"] != nil || intFromConfig(embeddingCfg["dimension"]) != 768 {
		t.Fatalf("config.embedding = %#v, want legacy model removed and dimension normalized", task.Config["embedding"])
	}

	task = newEmbeddingTaskDefinition()
	delete(task.Config, "embedding")
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("Create with omitted embedding config: %v", err)
	}
	embeddingCfg, ok = asJSONMap(task.Config["embedding"])
	if !ok {
		t.Fatalf("config.embedding = %#v, want JSONMap", task.Config["embedding"])
	}
	if embeddingCfg["model"] != nil || intFromConfig(embeddingCfg["dimension"]) != 768 {
		t.Fatalf("config.embedding = %#v, want dimension-only test snapshot", embeddingCfg)
	}
}

func TestEmbeddingTaskDefinitionSupportsItemScope(t *testing.T) {
	db := newEmbeddingTaskServiceTestDB(t)
	embeddingRepo := repository.NewEmbeddingRepository(db)
	provider := testEmbeddingConfigurationProvider("qwen3-vl-embedding", 2560, 10)
	taskSvc := NewEmbeddingTaskService(embeddingRepo, nil, nil, provider)

	task := &models.EmbeddingTask{
		TenantID: 7,
		Name:     "单文件向量化",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{
				"scope":     "item",
				"engine_id": 11,
				"item_id":   99,
				"locator":   "addp://engine/11/path/datasets/a.jpg?type=object&item_id=99",
			},
		},
	}

	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("Create item-scope task: %v", err)
	}
	_, req, err := taskSvc.embeddingTaskExecutionConfig(context.Background(), task)
	if err != nil {
		t.Fatalf("embeddingTaskExecutionConfig: %v", err)
	}
	if req.Scope != EmbeddingExecutionScopeItem {
		t.Fatalf("scope = %s, want item", req.Scope)
	}
	if req.Target.ItemID != 99 || req.Target.NodeID != 0 || req.Target.EngineID != 11 {
		t.Fatalf("target = %#v, want item_id=99 engine_id=11 and no node_id", req.Target)
	}
	filters, ok := asJSONMap(task.Config["filters"])
	if !ok || intFromConfig(filters["max_file_size_mb"]) != 10 {
		t.Fatalf("config.filters = %#v, want default max_file_size_mb=10", task.Config["filters"])
	}
}

func TestEmbeddingTaskCreatePreservesDisabledFlag(t *testing.T) {
	db := newEmbeddingTaskServiceTestDB(t)
	embeddingRepo := repository.NewEmbeddingRepository(db)
	taskSvc := NewEmbeddingTaskService(embeddingRepo, nil, nil, nil)

	task := newEmbeddingTaskDefinition()
	task.Enabled = false
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create disabled embedding task: %v", err)
	}

	refreshed, err := embeddingRepo.GetEmbeddingTask(context.Background(), task.ID, task.TenantID)
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if refreshed.Enabled {
		t.Fatal("enabled = true, want false")
	}
}

func newEmbeddingTaskDefinition() *models.EmbeddingTask {
	return &models.EmbeddingTask{
		TenantID: 7,
		Name:     "目录向量化",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{
				"scope":     "node",
				"engine_id": 11,
				"node_id":   23,
				"locator":   "addp://engine/11/path/datasets?type=directory&node_id=23",
				"recursive": true,
			},
			"filters": commonModels.JSONMap{
				"max_file_size_mb": 10,
			},
			"embedding": commonModels.JSONMap{
				"model":     "qwen3-vl-embedding",
				"dimension": 2560,
			},
		},
	}
}

func newEmbeddingTaskServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
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
	addTaskExecutionRuntimeColumns(t, db)
	if err := db.Exec(`CREATE TABLE manager.embedding_tasks (
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
		t.Fatalf("create embedding_tasks table: %v", err)
	}
	return db
}

func waitForEmbeddingTaskExecution(t *testing.T, repo *commonExecution.TaskExecutionRepository, executionID string, tenantID int) *commonExecution.TaskExecution {
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

func testEmbeddingConfigurationProvider(model string, dimension, maxFileSizeMB int) *EmbeddingConfigurationProvider {
	return NewEmbeddingConfigurationProvider(EffectiveEmbeddingConfiguration{
		Dimension:        dimension,
		MaxDistance:      0.78,
		MaxFileSizeMB:    maxFileSizeMB,
		BatchConcurrency: 1,
	})
}
