package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

type Model3DTilesExecutor interface {
	BuildModel3DTiles(ctx context.Context, req Model3DTilesExecutionRequest) (*Model3DTilesExecutionResult, error)
}

type Model3DTilesMetaScanSubmitter interface {
	CreateManualScanRun(opts commonClient.MetaScanOptions) (*commonExecution.TaskExecution, error)
	SetTenantID(tenantID *uint)
}

type Model3DTilesExecutionRequest struct {
	Task        *models.Model3DTilesTask
	ExecutionID string
	Config      Model3DTilesExecutionConfig
}

type Model3DTilesExecutionResult struct {
	TilesetLocator string
	TilesetRef     string
	TileCount      int64
	Metadata       commonModels.JSONMap
}

type Model3DTilesExecutionConfig struct {
	Source  Model3DTilesSourceConfig
	Target  Model3DTilesTargetConfig
	Tiles   Model3DTilesTilesConfig
	Options commonModels.JSONMap
}

type Model3DTilesSourceConfig struct {
	ItemLocator    string
	SourceEngineID uint
	Format         string
}

type Model3DTilesTargetConfig struct {
	StorageLocator string
	TargetEngineID uint
	DatasetName    string
}

type Model3DTilesTilesConfig struct {
	Format string
}

type Model3DTilesTaskService struct {
	repo              *repository.Model3DTilesRepository
	taskExecRepo      *commonExecution.TaskExecutionRepository
	executor          Model3DTilesExecutor
	metaScanSubmitter Model3DTilesMetaScanSubmitter
}

func NewModel3DTilesTaskService(repo *repository.Model3DTilesRepository, taskExecRepo *commonExecution.TaskExecutionRepository) *Model3DTilesTaskService {
	return &Model3DTilesTaskService{repo: repo, taskExecRepo: taskExecRepo}
}

func (s *Model3DTilesTaskService) SetExecutor(executor Model3DTilesExecutor) {
	s.executor = executor
}

func (s *Model3DTilesTaskService) SetMetaScanSubmitter(submitter Model3DTilesMetaScanSubmitter) {
	s.metaScanSubmitter = submitter
}

func (s *Model3DTilesTaskService) Create(ctx context.Context, task *models.Model3DTilesTask) error {
	if err := normalizeModel3DTilesTask(task); err != nil {
		return err
	}
	return s.repo.CreateTask(ctx, task)
}

func (s *Model3DTilesTaskService) GetByID(ctx context.Context, id uint, tenantID uint) (*models.Model3DTilesTask, error) {
	return s.repo.GetTask(ctx, id, tenantID)
}

func (s *Model3DTilesTaskService) List(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.Model3DTilesTask, int64, error) {
	return s.repo.ListTasks(ctx, tenantID, page, pageSize)
}

func (s *Model3DTilesTaskService) Update(ctx context.Context, task *models.Model3DTilesTask) error {
	if err := normalizeModel3DTilesTask(task); err != nil {
		return err
	}
	return s.repo.UpdateTask(ctx, task)
}

func (s *Model3DTilesTaskService) Delete(ctx context.Context, id uint, tenantID uint) error {
	return s.repo.DeleteTask(ctx, id, tenantID)
}

func (s *Model3DTilesTaskService) Execute(ctx context.Context, taskID uint, tenantID uint, triggerType string, source string, parentExecutionID *string) (string, error) {
	task, err := s.repo.GetTask(ctx, taskID, tenantID)
	if err != nil {
		return "", err
	}
	if task == nil {
		return "", ErrTaskNotFound
	}
	if s.taskExecRepo == nil {
		return "", errors.New("task execution repository is required")
	}
	normalizedTriggerType, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil {
		return "", err
	}
	normalizedSource := strings.TrimSpace(source)
	if normalizedSource == "" {
		normalizedSource = commonExecution.ModuleManager
	}

	executionID := uuid.New().String()
	now := time.Now()
	currentStep := "生成三维模型 3D Tiles"
	executionConfig := task.Config.Clone()
	if executionConfig == nil {
		executionConfig = commonModels.JSONMap{}
	}
	exec := &commonExecution.TaskExecution{
		ExecutionID:       executionID,
		TenantID:          int(tenantID),
		Module:            commonExecution.ModuleManager,
		TaskType:          commonExecution.TaskTypeModel3DTilesGeneration,
		Source:            normalizedSource,
		SourceTaskID:      commonExecution.NewSourceTaskIDFromUint(taskID),
		SourceTaskName:    &task.Name,
		ParentExecutionID: parentExecutionID,
		Status:            commonExecution.ExecutionStatusRunning,
		Progress:          0,
		CurrentStep:       &currentStep,
		TriggerType:       normalizedTriggerType,
		ExecutionConfig:   executionConfig,
		StartedAt:         &now,
	}
	if err := s.taskExecRepo.Create(ctx, exec); err != nil {
		return "", err
	}
	if err := s.repo.UpdateTaskLastExecution(ctx, taskID, tenantID, executionID, commonExecution.ExecutionStatusRunning, now); err != nil {
		return "", err
	}

	go s.runModel3DTilesGeneration(context.Background(), task, executionID, now)
	return executionID, nil
}

func (s *Model3DTilesTaskService) runModel3DTilesGeneration(ctx context.Context, task *models.Model3DTilesTask, executionID string, startedAt time.Time) {
	status := commonExecution.ExecutionStatusSuccess
	progress := 100
	metadata := commonModels.JSONMap{}
	var errDetails commonModels.JSONMap

	cfg, err := normalizeModel3DTilesTaskConfig(task.Config)
	var result *Model3DTilesExecutionResult
	if err == nil {
		if s.executor == nil {
			err = errors.New("model 3d tiles generation executor is not configured")
		} else {
			result, err = s.executor.BuildModel3DTiles(ctx, Model3DTilesExecutionRequest{Task: task, ExecutionID: executionID, Config: cfg})
		}
	}
	if err == nil && result != nil {
		metadata = result.Metadata.Clone()
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
		metadata["tileset_locator"] = result.TilesetLocator
		metadata["tileset_ref"] = result.TilesetRef
		metadata["tile_count"] = result.TileCount
		var scanRun *commonExecution.TaskExecution
		scanRun, err = s.submitModel3DTilesMetaScan(task.TenantID, cfg)
		if err == nil && scanRun != nil {
			metadata["meta_scan"] = commonModels.JSONMap{
				"execution_id": scanRun.ExecutionID,
				"status":       scanRun.Status,
				"task_type":    scanRun.TaskType,
				"module":       scanRun.Module,
				"engine_id":    cfg.Target.TargetEngineID,
				"catalog_path": model3DTilesDatasetCatalogPath(cfg),
			}
		}
	}
	if err != nil {
		if model3DTilesExecutionTimedOut(err) {
			status = commonExecution.ExecutionStatusTimeout
		} else {
			status = commonExecution.ExecutionStatusFailed
		}
		progress = s.model3DTilesExistingExecutionProgress(ctx, executionID, int(task.TenantID))
		errDetails = commonModels.JSONMap{"message": err.Error()}
	}
	metadata = s.mergeModel3DTilesExistingExecutionMetadata(ctx, executionID, int(task.TenantID), metadata)

	completedAt := time.Now()
	durationMs := completedAt.Sub(startedAt).Milliseconds()
	if err := s.taskExecRepo.UpdateFields(ctx, executionID, int(task.TenantID), map[string]interface{}{
		"status":            status,
		"progress":          progress,
		"metadata":          metadata,
		"error_details":     errDetails,
		"completed_at":      completedAt,
		"execution_time_ms": durationMs,
		"updated_at":        completedAt,
	}); err != nil {
		_ = s.repo.UpdateTaskLastExecution(ctx, task.ID, task.TenantID, executionID, commonExecution.ExecutionStatusFailed, completedAt)
		return
	}
	_ = s.repo.UpdateTaskLastExecution(ctx, task.ID, task.TenantID, executionID, status, completedAt)
}

func model3DTilesExecutionTimedOut(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "timeout exceeded") ||
		strings.Contains(message, "context deadline exceeded") ||
		strings.Contains(message, "client.timeout")
}

func (s *Model3DTilesTaskService) model3DTilesExistingExecutionProgress(ctx context.Context, executionID string, tenantID int) int {
	if s == nil || s.taskExecRepo == nil {
		return 0
	}
	exec, err := s.taskExecRepo.GetByExecutionID(ctx, executionID, tenantID)
	if err != nil || exec == nil {
		return 0
	}
	if exec.Progress < 0 {
		return 0
	}
	if exec.Progress > 99 {
		return 99
	}
	return exec.Progress
}

func (s *Model3DTilesTaskService) submitModel3DTilesMetaScan(tenantID uint, cfg Model3DTilesExecutionConfig) (*commonExecution.TaskExecution, error) {
	if s == nil || s.metaScanSubmitter == nil {
		return nil, errors.New("model 3d tiles meta scan submitter is not configured")
	}
	if cfg.Target.TargetEngineID == 0 {
		return nil, errors.New("model 3d tiles target engine_id is required for meta scan")
	}
	catalogPath := model3DTilesDatasetCatalogPath(cfg)
	if catalogPath == "" {
		return nil, errors.New("model 3d tiles dataset catalog path is required for meta scan")
	}
	if tenantID > 0 {
		s.metaScanSubmitter.SetTenantID(&tenantID)
	}
	return s.metaScanSubmitter.CreateManualScanRun(commonClient.MetaScanOptions{
		EngineID:     cfg.Target.TargetEngineID,
		CatalogPaths: []string{catalogPath},
		ScanDepth:    "deep",
		Force:        true,
		TriggerType:  commonExecution.TriggerTypeManual,
		Source:       commonExecution.ModuleManager,
	})
}

func model3DTilesDatasetCatalogPath(cfg Model3DTilesExecutionConfig) string {
	loc, err := resourcetree.ParseURI(cfg.Target.StorageLocator)
	if err != nil || loc == nil {
		return ""
	}
	return strings.Trim(joinFilePath(strings.Trim(loc.FullName(), "/"), strings.Trim(cfg.Target.DatasetName, "/")), "/")
}

func (s *Model3DTilesTaskService) mergeModel3DTilesExistingExecutionMetadata(ctx context.Context, executionID string, tenantID int, metadata commonModels.JSONMap) commonModels.JSONMap {
	if metadata == nil {
		metadata = commonModels.JSONMap{}
	}
	if s == nil || s.taskExecRepo == nil {
		return metadata
	}
	exec, err := s.taskExecRepo.GetByExecutionID(ctx, executionID, tenantID)
	if err != nil || exec.Metadata == nil {
		return metadata
	}
	merged := exec.Metadata.Clone()
	if merged == nil {
		merged = commonModels.JSONMap{}
	}
	for key, value := range metadata {
		merged[key] = value
	}
	return merged
}

func normalizeModel3DTilesTask(task *models.Model3DTilesTask) error {
	if task == nil {
		return errors.New("model 3d tiles generation task is nil")
	}
	task.Name = strings.TrimSpace(task.Name)
	task.Description = strings.TrimSpace(task.Description)
	task.Schedule = strings.TrimSpace(task.Schedule)
	if task.Config == nil {
		task.Config = commonModels.JSONMap{}
	}
	if task.Name == "" {
		return errors.New("model 3d tiles generation task name is required")
	}
	if len(task.Config) == 0 {
		return errors.New("model 3d tiles generation task config is required")
	}
	if task.Schedule != "" || task.NextRunAt != nil {
		return errors.New("model 3d tiles generation task does not support schedule")
	}
	_, err := normalizeModel3DTilesTaskConfig(task.Config)
	return err
}

func normalizeModel3DTilesTaskConfig(config commonModels.JSONMap) (Model3DTilesExecutionConfig, error) {
	source, err := normalizeModel3DTilesSource(config)
	if err != nil {
		return Model3DTilesExecutionConfig{}, err
	}
	target, err := normalizeModel3DTilesTarget(config)
	if err != nil {
		return Model3DTilesExecutionConfig{}, err
	}
	tiles := normalizeModel3DTilesTiles(config)
	options := normalizeModel3DTilesOptions(config)
	return Model3DTilesExecutionConfig{Source: source, Target: target, Tiles: tiles, Options: options}, nil
}

func normalizeModel3DTilesSource(config commonModels.JSONMap) (Model3DTilesSourceConfig, error) {
	sourceMap, ok := asJSONMap(config["source"])
	if !ok {
		return Model3DTilesSourceConfig{}, errors.New("model 3d tiles config.source is required")
	}
	source := Model3DTilesSourceConfig{
		ItemLocator:    stringFromConfig(sourceMap["item_locator"]),
		SourceEngineID: uintFromConfig(sourceMap["source_engine_id"]),
		Format:         strings.ToLower(firstNonEmptyConfig(stringFromConfig(sourceMap["format"]), string(format.FormatOSGBScene))),
	}
	if source.ItemLocator == "" || source.SourceEngineID == 0 {
		return Model3DTilesSourceConfig{}, errors.New("model 3d tiles config.source requires item_locator and source_engine_id")
	}
	if source.Format != string(format.FormatOSGBScene) {
		return Model3DTilesSourceConfig{}, errors.New("model 3d tiles config.source.format must be osgb_scene")
	}
	loc, err := resourcetree.ParseURI(source.ItemLocator)
	if err != nil {
		return Model3DTilesSourceConfig{}, fmt.Errorf("model 3d tiles config.source.item_locator is invalid: %w", err)
	}
	if loc.EngineID != source.SourceEngineID {
		return Model3DTilesSourceConfig{}, errors.New("model 3d tiles config.source.item_locator engine_id does not match source_engine_id")
	}
	config["source"] = commonModels.JSONMap{
		"item_locator":     source.ItemLocator,
		"source_engine_id": source.SourceEngineID,
		"format":           source.Format,
	}
	return source, nil
}

func normalizeModel3DTilesTarget(config commonModels.JSONMap) (Model3DTilesTargetConfig, error) {
	targetMap, ok := asJSONMap(config["target"])
	if !ok {
		return Model3DTilesTargetConfig{}, errors.New("model 3d tiles config.target is required")
	}
	target := Model3DTilesTargetConfig{
		StorageLocator: stringFromConfig(targetMap["storage_locator"]),
		TargetEngineID: uintFromConfig(targetMap["target_engine_id"]),
		DatasetName:    stringFromConfig(targetMap["dataset_name"]),
	}
	if target.StorageLocator == "" || target.TargetEngineID == 0 {
		return Model3DTilesTargetConfig{}, errors.New("model 3d tiles config.target requires storage_locator and target_engine_id")
	}
	loc, err := resourcetree.ParseURI(target.StorageLocator)
	if err != nil {
		return Model3DTilesTargetConfig{}, fmt.Errorf("model 3d tiles config.target.storage_locator is invalid: %w", err)
	}
	if loc.EngineID != target.TargetEngineID {
		return Model3DTilesTargetConfig{}, errors.New("model 3d tiles config.target.storage_locator engine_id does not match target_engine_id")
	}
	if target.DatasetName == "" {
		target.DatasetName = "model_3d_tiles"
	}
	config["target"] = commonModels.JSONMap{
		"storage_locator":  target.StorageLocator,
		"target_engine_id": target.TargetEngineID,
		"dataset_name":     target.DatasetName,
	}
	return target, nil
}

func normalizeModel3DTilesTiles(config commonModels.JSONMap) Model3DTilesTilesConfig {
	tilesMap, _ := asJSONMap(config["tiles"])
	tiles := Model3DTilesTilesConfig{
		Format: strings.ToLower(firstNonEmptyConfig(stringFromConfig(tilesMap["format"]), "3dtiles")),
	}
	if tiles.Format != "3dtiles" {
		tiles.Format = "3dtiles"
	}
	config["tiles"] = commonModels.JSONMap{"format": tiles.Format}
	return tiles
}

func normalizeModel3DTilesOptions(config commonModels.JSONMap) commonModels.JSONMap {
	options, ok := asJSONMap(config["options"])
	if !ok || options == nil {
		options = commonModels.JSONMap{}
	}
	config["options"] = options
	return options
}
