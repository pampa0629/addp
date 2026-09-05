package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

var (
	ErrModel3DTilesResultNotFound    = errors.New("model3d tiles result not found")
	ErrModel3DTilesTaskExecutionBusy = errors.New("model3d tiles task already has an active execution")
)

type Model3DTilesExecutor interface {
	BuildModel3DTiles(ctx context.Context, req Model3DTilesExecutionRequest) (*Model3DTilesExecutionResult, error)
}

type Model3DTilesArtifactCleaner interface {
	DeleteByStorageRef(ctx context.Context, storageRef string) error
}

type Model3DTilesExecutionRequest struct {
	Task        *models.Model3DTilesTask
	ExecutionID string
	Config      Model3DTilesExecutionConfig
}

type Model3DTilesExecutionResult struct {
	StorageRef  string
	ManifestRef string
	FileCount   int64
	SizeBytes   int64
	Metadata    commonModels.JSONMap
}

type Model3DTilesExecutionConfig struct {
	Source       Model3DTilesSourceConfig
	TargetFormat string
	Result       Model3DTilesResultConfig
	Options      commonModels.JSONMap
}

type Model3DTilesSourceConfig struct {
	ItemLocator     string
	SourceEngineID  uint
	ItemFingerprint string
	ItemID          uint
	Format          string
	SourceSizeBytes int64
}

type Model3DTilesResultConfig struct{ StorageRef string }

type Model3DTilesTaskService struct {
	repo     *repository.Model3DTilesRepository
	executor Model3DTilesExecutor
	cleaner  Model3DTilesArtifactCleaner
	bucket   string
}

func NewModel3DTilesTaskService(repo *repository.Model3DTilesRepository) *Model3DTilesTaskService {
	return &Model3DTilesTaskService{repo: repo}
}

func (s *Model3DTilesTaskService) SetExecutor(executor Model3DTilesExecutor) {
	s.executor = executor
}

func (s *Model3DTilesTaskService) SetCleaner(cleaner Model3DTilesArtifactCleaner) {
	s.cleaner = cleaner
}

func (s *Model3DTilesTaskService) SetBucket(bucket string) { s.bucket = strings.TrimSpace(bucket) }

func (s *Model3DTilesTaskService) Create(ctx context.Context, task *models.Model3DTilesTask) error {
	if err := normalizeModel3DTilesTask(task, s.bucket); err != nil {
		return err
	}
	cfg, err := normalizeModel3DTilesTaskConfig(task.Config, s.bucket, task.TenantID)
	if err != nil {
		return err
	}
	existing, err := s.repo.GetTaskByItemFingerprintAndFormat(ctx, task.TenantID, cfg.Source.ItemFingerprint, cfg.TargetFormat)
	if err != nil {
		return err
	}
	if existing != nil {
		return s.reuseExistingTask(ctx, task, existing)
	}
	if err := s.repo.CreateTask(ctx, task); err != nil {
		if strings.Contains(err.Error(), "idx_model3d_tiles_tasks_source_format_unique") {
			existing, lookupErr := s.repo.GetTaskByItemFingerprintAndFormat(ctx, task.TenantID, cfg.Source.ItemFingerprint, cfg.TargetFormat)
			if lookupErr != nil {
				return lookupErr
			}
			if existing != nil {
				return s.reuseExistingTask(ctx, task, existing)
			}
		}
		return err
	}
	return nil
}

func (s *Model3DTilesTaskService) GetByID(ctx context.Context, id uint, tenantID uint) (*models.Model3DTilesTask, error) {
	return s.repo.GetTask(ctx, id, tenantID)
}

func (s *Model3DTilesTaskService) List(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.Model3DTilesTask, int64, error) {
	return s.repo.ListTasks(ctx, tenantID, page, pageSize)
}

func (s *Model3DTilesTaskService) ListResults(ctx context.Context, filter repository.Model3DTilesFilter) ([]*models.Model3DTiles, int64, error) {
	return s.repo.ListResults(ctx, filter)
}

func (s *Model3DTilesTaskService) DeleteResult(ctx context.Context, id uint, tenantID uint) error {
	result, err := s.repo.GetResult(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if result == nil {
		return ErrModel3DTilesResultNotFound
	}
	if strings.TrimSpace(result.StorageRef) != "" {
		if s.cleaner == nil {
			return errors.New("model3d tiles artifact cleaner is not configured")
		}
		if err := s.cleaner.DeleteByStorageRef(ctx, result.StorageRef); err != nil {
			return err
		}
	}
	return s.repo.DeleteResult(ctx, id, tenantID)
}

func (s *Model3DTilesTaskService) Update(ctx context.Context, task *models.Model3DTilesTask) error {
	if err := normalizeModel3DTilesTask(task, s.bucket); err != nil {
		return err
	}
	return s.repo.UpdateTask(ctx, task)
}

func (s *Model3DTilesTaskService) Delete(ctx context.Context, id uint, tenantID uint) error {
	return s.repo.DeleteTask(ctx, id, tenantID)
}

func (s *Model3DTilesTaskService) reuseExistingTask(ctx context.Context, task *models.Model3DTilesTask, existing *models.Model3DTilesTask) error {
	existing.Name = task.Name
	existing.Description = task.Description
	existing.Enabled = task.Enabled
	existing.Schedule = ""
	existing.NextRunAt = nil
	existing.Config = task.Config.Clone()
	if existing.CreatedBy == nil {
		existing.CreatedBy = task.CreatedBy
	}
	if err := s.repo.UpdateTask(ctx, existing); err != nil {
		return err
	}
	*task = *existing
	return nil
}

func (s *Model3DTilesTaskService) Execute(
	ctx context.Context, taskID uint, tenantID uint, triggerType string, source string,
	parentExecutionID *string, overwriteExistingResult bool,
) (string, error) {
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
	currentStep := "生成分块三维模型瓦片"
	exec := &commonExecution.TaskExecution{
		ExecutionID:       executionID,
		TenantID:          int(tenantID),
		Module:            commonExecution.ModuleManager,
		TaskType:          commonExecution.TaskTypeModel3DTilesGeneration,
		Source:            normalizedSource,
		ParentExecutionID: parentExecutionID,
		Status:            commonExecution.ExecutionStatusPending,
		Progress:          0,
		CurrentStep:       &currentStep,
		TriggerType:       normalizedTriggerType,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	claimedTask, err := s.repo.ClaimExecution(ctx, taskID, tenantID, exec, overwriteExistingResult)
	if err != nil {
		if errors.Is(err, repository.ErrExistingResultActionRequired) {
			return "", ErrExistingResultActionRequired
		}
		if errors.Is(err, commonAPI.ErrNotFound) {
			return "", ErrTaskNotFound
		}
		if errors.Is(err, commonAPI.ErrConflict) {
			return "", ErrModel3DTilesTaskExecutionBusy
		}
		return "", err
	}
	go s.runModel3DTilesGeneration(context.Background(), claimedTask, executionID)
	return executionID, nil
}

func (s *Model3DTilesTaskService) runModel3DTilesGeneration(ctx context.Context, task *models.Model3DTilesTask, executionID string) {
	startedAt := time.Now()
	if err := s.repo.StartExecution(ctx, task.ID, task.TenantID, executionID, startedAt); err != nil {
		logger.L().Warn("领取分块三维模型瓦片 execution 失败", "execution_id", executionID, "task_id", task.ID, "error", err)
		return
	}
	status := commonExecution.ExecutionStatusSuccess
	progress := 100
	metadata := commonModels.JSONMap{}
	var errDetails commonModels.JSONMap

	artifact, cfg, err := s.prepareModel3DTilesResult(ctx, task, executionID)
	var result *Model3DTilesExecutionResult
	if err == nil {
		if s.executor == nil {
			err = errors.New("model 3d tiles generation executor is not configured")
		} else {
			result, err = s.executor.BuildModel3DTiles(ctx, Model3DTilesExecutionRequest{Task: task, ExecutionID: executionID, Config: cfg})
		}
	}
	if err == nil && result == nil {
		err = errors.New("model3d tiles generation returned no result")
	}
	if err == nil && result != nil {
		metadata = result.Metadata.Clone()
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
		metadata["storage_ref"] = result.StorageRef
		metadata["manifest_ref"] = result.ManifestRef
		metadata["file_count"] = result.FileCount
		if outputRef, lineageErr := managerInfraLineageRef(result.StorageRef, s.bucket, "prefix"); lineageErr == nil {
			metadata = managerExecutionLineage(metadata, commonExecution.TaskTypeModel3DTilesGeneration,
				[]commonExecution.LineageResourceRef{managerItemLineageRef(cfg.Source.ItemLocator, cfg.Source.ItemFingerprint, cfg.Source.ItemID)},
				[]commonExecution.LineageResourceRef{outputRef},
			)
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
	var artifactFields map[string]interface{}
	if artifact != nil {
		artifactFields = map[string]interface{}{"last_execution_id": executionID, "updated_at": time.Now()}
		if err != nil {
			artifactFields["status"] = models.Model3DTilesStatusFailed
			artifactFields["error_message"] = err.Error()
		} else {
			artifactFields["status"] = models.Model3DTilesStatusReady
			artifactFields["error_message"] = ""
			artifactFields["storage_ref"] = result.StorageRef
			artifactFields["manifest_ref"] = result.ManifestRef
			artifactFields["file_count"] = result.FileCount
			artifactFields["size_bytes"] = result.SizeBytes
			artifactFields["metadata"] = metadata
		}
	}
	metadata = s.mergeModel3DTilesExistingExecutionMetadata(ctx, executionID, task.TenantID, metadata)

	completedAt := time.Now()
	durationMs := completedAt.Sub(startedAt).Milliseconds()
	artifactID := uint(0)
	if artifact != nil {
		artifactID = artifact.ID
	}
	if err := s.repo.CompleteExecution(ctx, task.ID, task.TenantID, executionID, artifactID, artifactFields, map[string]interface{}{
		"status":            status,
		"progress":          progress,
		"metadata":          metadata,
		"error_details":     errDetails,
		"completed_at":      completedAt,
		"execution_time_ms": durationMs,
		"updated_at":        completedAt,
	}, completedAt); err != nil {
		logger.L().Warn("提交分块三维模型瓦片 execution 终态失败", "execution_id", executionID, "task_id", task.ID, "error", err)
	}
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
	if s == nil || s.repo == nil {
		return 0
	}
	exec, err := s.repo.GetExecution(ctx, executionID, uint(tenantID))
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

func (s *Model3DTilesTaskService) mergeModel3DTilesExistingExecutionMetadata(ctx context.Context, executionID string, tenantID uint, metadata commonModels.JSONMap) commonModels.JSONMap {
	if metadata == nil {
		metadata = commonModels.JSONMap{}
	}
	if s == nil || s.repo == nil {
		return metadata
	}
	exec, err := s.repo.GetExecution(ctx, executionID, tenantID)
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

func normalizeModel3DTilesTask(task *models.Model3DTilesTask, bucket string) error {
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
	_, err := normalizeModel3DTilesTaskConfig(task.Config, bucket, task.TenantID)
	return err
}

func normalizeModel3DTilesTaskConfig(config commonModels.JSONMap, bucket string, tenantID uint) (Model3DTilesExecutionConfig, error) {
	source, err := normalizeModel3DTilesSource(config)
	if err != nil {
		return Model3DTilesExecutionConfig{}, err
	}
	targetFormat := strings.ToLower(strings.TrimSpace(stringFromConfig(config["target_format"])))
	if targetFormat != models.Model3DTilesTargetFormat3DTiles && targetFormat != models.Model3DTilesTargetFormatS3M {
		return Model3DTilesExecutionConfig{}, errors.New("model3d tiles config.target_format must be 3d_tiles or s3m")
	}
	config["target_format"] = targetFormat
	resultMap, _ := asJSONMap(config["result"])
	storageRef := stringFromConfig(resultMap["storage_ref"])
	if storageRef == "" {
		prefix := fmt.Sprintf("tenant_%d/model3d-tiles/%s/%s", tenantID, source.ItemFingerprint, targetFormat)
		storageRef = rastercogref.ObjectStorageRef(firstNonEmptyConfig(bucket, "manager"), prefix)
	}
	result := Model3DTilesResultConfig{StorageRef: storageRef}
	config["result"] = commonModels.JSONMap{"storage_ref": storageRef}
	options := normalizeModel3DTilesOptions(config)
	return Model3DTilesExecutionConfig{Source: source, TargetFormat: targetFormat, Result: result, Options: options}, nil
}

func normalizeModel3DTilesSource(config commonModels.JSONMap) (Model3DTilesSourceConfig, error) {
	sourceMap, ok := asJSONMap(config["source"])
	if !ok {
		return Model3DTilesSourceConfig{}, errors.New("model 3d tiles config.source is required")
	}
	source := Model3DTilesSourceConfig{
		ItemLocator: stringFromConfig(sourceMap["item_locator"]), SourceEngineID: uintFromConfig(sourceMap["source_engine_id"]),
		ItemFingerprint: strings.TrimSpace(stringFromConfig(sourceMap["item_fingerprint"])), ItemID: uintFromConfig(sourceMap["item_id"]),
		Format:          strings.ToLower(firstNonEmptyConfig(stringFromConfig(sourceMap["format"]), string(format.FormatOSGBScene))),
		SourceSizeBytes: int64FromConfig(sourceMap["source_size_bytes"], 0),
	}
	if source.ItemLocator == "" || source.SourceEngineID == 0 || source.ItemFingerprint == "" {
		return Model3DTilesSourceConfig{}, errors.New("model3d tiles config.source requires item_locator, source_engine_id and item_fingerprint")
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
		"item_locator": source.ItemLocator, "source_engine_id": source.SourceEngineID,
		"item_fingerprint": source.ItemFingerprint, "item_id": source.ItemID,
		"format": source.Format, "source_size_bytes": source.SourceSizeBytes,
	}
	return source, nil
}

func (s *Model3DTilesTaskService) prepareModel3DTilesResult(ctx context.Context, task *models.Model3DTilesTask, executionID string) (*models.Model3DTiles, Model3DTilesExecutionConfig, error) {
	cfg, err := normalizeModel3DTilesTaskConfig(task.Config, s.bucket, task.TenantID)
	if err != nil {
		return nil, cfg, err
	}
	existing, err := s.repo.GetCurrentResult(ctx, task.TenantID, cfg.Source.ItemFingerprint, cfg.TargetFormat)
	if err != nil {
		return nil, cfg, err
	}
	fields := map[string]interface{}{"task_id": task.ID, "last_execution_id": executionID, "storage_ref": cfg.Result.StorageRef, "manifest_ref": model3DTilesManifestRef(cfg.TargetFormat), "status": models.Model3DTilesStatusBuilding, "error_message": "", "updated_at": time.Now()}
	if existing != nil {
		if err := s.repo.UpdateResultFields(ctx, existing.ID, task.TenantID, fields); err != nil {
			return nil, cfg, err
		}
		return existing, cfg, nil
	}
	result := &models.Model3DTiles{TenantID: task.TenantID, ItemFingerprint: cfg.Source.ItemFingerprint, ItemID: optionalUint(cfg.Source.ItemID), Locator: cfg.Source.ItemLocator, TaskID: optionalUint(task.ID), LastExecutionID: &executionID, SourceEngineID: cfg.Source.SourceEngineID, SourceFormat: cfg.Source.Format, SourceSizeBytes: cfg.Source.SourceSizeBytes, TargetFormat: cfg.TargetFormat, StorageRef: cfg.Result.StorageRef, ManifestRef: model3DTilesManifestRef(cfg.TargetFormat), Status: models.Model3DTilesStatusBuilding, Metadata: commonModels.JSONMap{}, CreatedBy: task.CreatedBy}
	if err := s.repo.CreateResult(ctx, result); err != nil {
		return nil, cfg, err
	}
	return result, cfg, nil
}

func model3DTilesManifestRef(targetFormat string) string {
	if targetFormat == models.Model3DTilesTargetFormatS3M {
		return "config/scene.scp"
	}
	return "tileset.json"
}

func normalizeModel3DTilesOptions(config commonModels.JSONMap) commonModels.JSONMap {
	options, ok := asJSONMap(config["options"])
	if !ok || options == nil {
		options = commonModels.JSONMap{}
	}
	config["options"] = options
	return options
}
