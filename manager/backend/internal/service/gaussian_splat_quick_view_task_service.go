package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

type GaussianSplatQuickViewExecutor interface {
	BuildGaussianSplatQuickView(ctx context.Context, req GaussianSplatQuickViewExecutionRequest) (*GaussianSplatQuickViewExecutionResult, error)
}

type GaussianSplatQuickViewExecutionRequest struct {
	Task        *models.GaussianSplatQuickViewTask
	ExecutionID string
	Config      GaussianSplatQuickViewExecutionConfig
}

type GaussianSplatQuickViewExecutionResult struct {
	StorageRef string
	FileName   string
	SizeBytes  int64
	ContentURL string
	Metadata   commonModels.JSONMap
}

type GaussianSplatQuickViewExecutionConfig struct {
	Source  GaussianSplatQuickViewSourceConfig
	Result  GaussianSplatQuickViewResultConfig
	Options commonModels.JSONMap
}

type GaussianSplatQuickViewSourceConfig struct {
	ItemLocator     string
	SourceEngineID  uint
	ItemFingerprint string
	ItemID          uint
	Format          string
	SourceSizeBytes int64
}

type GaussianSplatQuickViewResultConfig struct {
	StorageRef string
	FileName   string
}

type GaussianSplatQuickViewCleaner interface {
	DeleteByStorageRef(ctx context.Context, storageRef string) error
}

type GaussianSplatQuickViewTaskService struct {
	repo         *repository.GaussianSplatQuickViewRepository
	taskExecRepo *commonExecution.TaskExecutionRepository
	executor     GaussianSplatQuickViewExecutor
	cleaner      GaussianSplatQuickViewCleaner
	bucket       string
}

func NewGaussianSplatQuickViewTaskService(repo *repository.GaussianSplatQuickViewRepository, taskExecRepo *commonExecution.TaskExecutionRepository) *GaussianSplatQuickViewTaskService {
	return &GaussianSplatQuickViewTaskService{repo: repo, taskExecRepo: taskExecRepo}
}

func (s *GaussianSplatQuickViewTaskService) SetExecutor(executor GaussianSplatQuickViewExecutor) {
	s.executor = executor
}

func (s *GaussianSplatQuickViewTaskService) SetCleaner(cleaner GaussianSplatQuickViewCleaner) {
	s.cleaner = cleaner
}

func (s *GaussianSplatQuickViewTaskService) SetBucket(bucket string) {
	s.bucket = strings.TrimSpace(bucket)
}

func (s *GaussianSplatQuickViewTaskService) Create(ctx context.Context, task *models.GaussianSplatQuickViewTask) error {
	if err := normalizeGaussianSplatQuickViewTask(task, s.bucket); err != nil {
		return err
	}
	cfg, err := normalizeGaussianSplatQuickViewTaskConfig(task.Config, s.bucket, task.TenantID)
	if err != nil {
		return err
	}
	existing, err := s.repo.GetTaskByItemFingerprint(ctx, task.TenantID, cfg.Source.ItemFingerprint)
	if err != nil {
		return err
	}
	if existing != nil {
		return s.reuseExistingTask(ctx, task, existing)
	}
	if err := s.repo.CreateTask(ctx, task); err != nil {
		if strings.Contains(err.Error(), "idx_gaussian_splat_quick_view_tasks_source_unique") {
			existing, lookupErr := s.repo.GetTaskByItemFingerprint(ctx, task.TenantID, cfg.Source.ItemFingerprint)
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

func (s *GaussianSplatQuickViewTaskService) GetByID(ctx context.Context, id uint, tenantID uint) (*models.GaussianSplatQuickViewTask, error) {
	return s.repo.GetTask(ctx, id, tenantID)
}

func (s *GaussianSplatQuickViewTaskService) List(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.GaussianSplatQuickViewTask, int64, error) {
	return s.repo.ListTasks(ctx, tenantID, page, pageSize)
}

func (s *GaussianSplatQuickViewTaskService) Update(ctx context.Context, task *models.GaussianSplatQuickViewTask) error {
	if err := normalizeGaussianSplatQuickViewTask(task, s.bucket); err != nil {
		return err
	}
	return s.repo.UpdateTask(ctx, task)
}

func (s *GaussianSplatQuickViewTaskService) Delete(ctx context.Context, id uint, tenantID uint) error {
	return s.repo.DeleteTask(ctx, id, tenantID)
}

func (s *GaussianSplatQuickViewTaskService) reuseExistingTask(ctx context.Context, task *models.GaussianSplatQuickViewTask, existing *models.GaussianSplatQuickViewTask) error {
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

func (s *GaussianSplatQuickViewTaskService) ListResults(ctx context.Context, filter repository.GaussianSplatQuickViewFilter) ([]*models.GaussianSplatQuickView, int64, error) {
	return s.repo.List(ctx, filter)
}

func (s *GaussianSplatQuickViewTaskService) GetResult(ctx context.Context, id uint, tenantID uint) (*models.GaussianSplatQuickView, error) {
	return s.repo.GetByID(ctx, id, tenantID)
}

func (s *GaussianSplatQuickViewTaskService) DeleteResult(ctx context.Context, id uint, tenantID uint) error {
	result, err := s.repo.GetByID(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("gaussian splat quick view result not found")
	}
	if strings.TrimSpace(result.StorageRef) != "" && s.cleaner != nil {
		if err := s.cleaner.DeleteByStorageRef(ctx, result.StorageRef); err != nil {
			return err
		}
	}
	return s.repo.Delete(ctx, id, tenantID)
}

func (s *GaussianSplatQuickViewTaskService) Execute(ctx context.Context, taskID uint, tenantID uint, triggerType string, source string, parentExecutionID *string) (string, error) {
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
	currentStep := "生成高斯泼溅 KSplat 快显"
	executionConfig := task.Config.Clone()
	if executionConfig == nil {
		executionConfig = commonModels.JSONMap{}
	}
	exec := &commonExecution.TaskExecution{
		ExecutionID:       executionID,
		TenantID:          int(tenantID),
		Module:            commonExecution.ModuleManager,
		TaskType:          commonExecution.TaskTypeGaussianSplatQuickViewGeneration,
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

	go s.runGaussianSplatQuickViewGeneration(context.Background(), task, executionID, now)
	return executionID, nil
}

func (s *GaussianSplatQuickViewTaskService) runGaussianSplatQuickViewGeneration(ctx context.Context, task *models.GaussianSplatQuickViewTask, executionID string, startedAt time.Time) {
	status := commonExecution.ExecutionStatusSuccess
	progress := 100
	metadata := commonModels.JSONMap{}
	var errDetails commonModels.JSONMap

	result, execCfg, err := s.prepareResult(ctx, task, executionID)
	var buildResult *GaussianSplatQuickViewExecutionResult
	if err == nil {
		if s.executor == nil {
			err = errors.New("gaussian splat quick view generation executor is not configured")
		} else {
			buildResult, err = s.executor.BuildGaussianSplatQuickView(ctx, GaussianSplatQuickViewExecutionRequest{Task: task, ExecutionID: executionID, Config: execCfg})
		}
	}
	if err != nil {
		status = commonExecution.ExecutionStatusFailed
		progress = 0
		errDetails = commonModels.JSONMap{"message": err.Error()}
		metadata = commonModels.JSONMap{"error": err.Error()}
		if result != nil {
			_ = s.repo.UpdateFields(ctx, result.ID, task.TenantID, map[string]interface{}{
				"status":        models.GaussianSplatQuickViewStatusFailed,
				"error_message": err.Error(),
			})
		}
	} else if result != nil {
		if buildResult.ContentURL == "" {
			buildResult.ContentURL = gaussianSplatQuickViewContentURL(result.ID)
		}
		fields := map[string]interface{}{
			"status":            models.GaussianSplatQuickViewStatusReady,
			"error_message":     "",
			"last_execution_id": executionID,
		}
		applyGaussianSplatQuickViewResultFields(fields, buildResult)
		if err := s.repo.UpdateFields(ctx, result.ID, task.TenantID, fields); err != nil {
			status = commonExecution.ExecutionStatusFailed
			progress = 0
			errDetails = commonModels.JSONMap{"message": fmt.Sprintf("update gaussian splat quick view result: %v", err)}
			metadata = errDetails.Clone()
		} else {
			metadata = buildResult.Metadata.Clone()
			if metadata == nil {
				metadata = commonModels.JSONMap{}
			}
			metadata["result_id"] = result.ID
			metadata["storage_ref"] = buildResult.StorageRef
			metadata["content_url"] = buildResult.ContentURL
		}
	}

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

func (s *GaussianSplatQuickViewTaskService) prepareResult(ctx context.Context, task *models.GaussianSplatQuickViewTask, executionID string) (*models.GaussianSplatQuickView, GaussianSplatQuickViewExecutionConfig, error) {
	execCfg, err := normalizeGaussianSplatQuickViewTaskConfig(task.Config, s.bucket, task.TenantID)
	if err != nil {
		return nil, execCfg, err
	}
	itemID := optionalUint(execCfg.Source.ItemID)
	existing, err := s.repo.GetCurrentByFingerprint(ctx, task.TenantID, execCfg.Source.ItemFingerprint)
	if err != nil {
		return nil, execCfg, err
	}
	if existing != nil {
		updates := map[string]interface{}{
			"item_id":           itemID,
			"locator":           execCfg.Source.ItemLocator,
			"task_id":           task.ID,
			"last_execution_id": executionID,
			"source_engine_id":  execCfg.Source.SourceEngineID,
			"source_format":     execCfg.Source.Format,
			"source_size_bytes": execCfg.Source.SourceSizeBytes,
			"storage_ref":       execCfg.Result.StorageRef,
			"file_name":         execCfg.Result.FileName,
			"status":            models.GaussianSplatQuickViewStatusBuilding,
			"metadata":          commonModels.JSONMap{},
			"error_message":     "",
			"content_url":       gaussianSplatQuickViewContentURL(existing.ID),
			"updated_at":        time.Now(),
		}
		if err := s.repo.UpdateFields(ctx, existing.ID, task.TenantID, updates); err != nil {
			return nil, execCfg, err
		}
		existing.StorageRef = execCfg.Result.StorageRef
		existing.FileName = execCfg.Result.FileName
		return existing, execCfg, nil
	}
	result := &models.GaussianSplatQuickView{
		TenantID:        task.TenantID,
		ItemFingerprint: execCfg.Source.ItemFingerprint,
		ItemID:          itemID,
		Locator:         execCfg.Source.ItemLocator,
		TaskID:          &task.ID,
		LastExecutionID: &executionID,
		SourceEngineID:  execCfg.Source.SourceEngineID,
		SourceFormat:    execCfg.Source.Format,
		SourceSizeBytes: execCfg.Source.SourceSizeBytes,
		StorageRef:      execCfg.Result.StorageRef,
		FileName:        execCfg.Result.FileName,
		Status:          models.GaussianSplatQuickViewStatusBuilding,
		Metadata:        commonModels.JSONMap{},
		CreatedBy:       task.CreatedBy,
	}
	if err := s.repo.Create(ctx, result); err != nil {
		return nil, execCfg, err
	}
	result.ContentURL = gaussianSplatQuickViewContentURL(result.ID)
	_ = s.repo.UpdateFields(ctx, result.ID, task.TenantID, map[string]interface{}{"content_url": result.ContentURL})
	return result, execCfg, nil
}

func normalizeGaussianSplatQuickViewTask(task *models.GaussianSplatQuickViewTask, bucket string) error {
	if task == nil {
		return errors.New("gaussian splat quick view generation task is nil")
	}
	task.Name = strings.TrimSpace(task.Name)
	task.Description = strings.TrimSpace(task.Description)
	task.Schedule = strings.TrimSpace(task.Schedule)
	if task.Config == nil {
		task.Config = commonModels.JSONMap{}
	}
	if task.Name == "" {
		return errors.New("gaussian splat quick view generation task name is required")
	}
	if len(task.Config) == 0 {
		return errors.New("gaussian splat quick view generation task config is required")
	}
	if task.Schedule != "" || task.NextRunAt != nil {
		return errors.New("gaussian splat quick view generation task does not support schedule")
	}
	_, err := normalizeGaussianSplatQuickViewTaskConfig(task.Config, bucket, task.TenantID)
	return err
}

func normalizeGaussianSplatQuickViewTaskConfig(config commonModels.JSONMap, bucket string, tenantID uint) (GaussianSplatQuickViewExecutionConfig, error) {
	source, err := normalizeGaussianSplatQuickViewSource(config)
	if err != nil {
		return GaussianSplatQuickViewExecutionConfig{}, err
	}
	result := normalizeGaussianSplatQuickViewResult(config, source, bucket, tenantID)
	options, ok := asJSONMap(config["options"])
	if !ok || options == nil {
		options = commonModels.JSONMap{}
	}
	config["options"] = options
	return GaussianSplatQuickViewExecutionConfig{Source: source, Result: result, Options: options}, nil
}

func normalizeGaussianSplatQuickViewSource(config commonModels.JSONMap) (GaussianSplatQuickViewSourceConfig, error) {
	sourceMap, ok := asJSONMap(config["source"])
	if !ok {
		return GaussianSplatQuickViewSourceConfig{}, errors.New("gaussian splat quick view config.source is required")
	}
	source := GaussianSplatQuickViewSourceConfig{
		ItemLocator:     stringFromConfig(sourceMap["item_locator"]),
		SourceEngineID:  uintFromConfig(sourceMap["source_engine_id"]),
		ItemFingerprint: strings.TrimSpace(stringFromConfig(sourceMap["item_fingerprint"])),
		ItemID:          uintFromConfig(sourceMap["item_id"]),
		Format:          strings.ToLower(firstNonEmptyConfig(stringFromConfig(sourceMap["format"]), string(format.FormatKSplat))),
		SourceSizeBytes: int64FromConfig(sourceMap["source_size_bytes"], 0),
	}
	if source.ItemLocator == "" || source.SourceEngineID == 0 || source.ItemFingerprint == "" {
		return GaussianSplatQuickViewSourceConfig{}, errors.New("gaussian splat quick view config.source requires item_locator, source_engine_id and item_fingerprint")
	}
	if !isGaussianSplatQuickViewTaskSourceFormat(source.Format) {
		return GaussianSplatQuickViewSourceConfig{}, errors.New("gaussian splat quick view config.source.format must be ksplat")
	}
	loc, err := resourcetree.ParseURI(source.ItemLocator)
	if err != nil {
		return GaussianSplatQuickViewSourceConfig{}, fmt.Errorf("gaussian splat quick view config.source.item_locator is invalid: %w", err)
	}
	if loc.EngineID != source.SourceEngineID {
		return GaussianSplatQuickViewSourceConfig{}, errors.New("gaussian splat quick view config.source.item_locator engine_id does not match source_engine_id")
	}
	config["source"] = commonModels.JSONMap{
		"item_locator":      source.ItemLocator,
		"source_engine_id":  source.SourceEngineID,
		"item_fingerprint":  source.ItemFingerprint,
		"item_id":           source.ItemID,
		"format":            source.Format,
		"source_size_bytes": source.SourceSizeBytes,
	}
	return source, nil
}

func isGaussianSplatQuickViewTaskSourceFormat(sourceFormat string) bool {
	switch strings.ToLower(strings.TrimSpace(sourceFormat)) {
	case string(format.FormatKSplat):
		return true
	default:
		return false
	}
}

func normalizeGaussianSplatQuickViewResult(config commonModels.JSONMap, source GaussianSplatQuickViewSourceConfig, bucket string, tenantID uint) GaussianSplatQuickViewResultConfig {
	resultMap, _ := asJSONMap(config["result"])
	fileName := safeKSplatFileName(firstNonEmptyConfig(stringFromConfig(resultMap["file_name"]), defaultGaussianSplatQuickViewFileName(source.ItemLocator)))
	storageRef := stringFromConfig(resultMap["storage_ref"])
	if storageRef == "" {
		objectName := joinFilePath(fmt.Sprintf("tenant_%d/gaussian-splat-quick-view/%s", tenantID, source.ItemFingerprint), fileName)
		storageRef = rastercogref.ObjectStorageRef(firstNonEmptyConfig(bucket, "manager"), objectName)
	}
	result := GaussianSplatQuickViewResultConfig{StorageRef: storageRef, FileName: fileName}
	config["result"] = commonModels.JSONMap{
		"storage_ref": result.StorageRef,
		"file_name":   result.FileName,
	}
	return result
}

func defaultGaussianSplatQuickViewFileName(locator string) string {
	loc, err := resourcetree.ParseURI(locator)
	base := "gaussian-splat"
	if err == nil && loc != nil {
		parts := strings.Split(strings.Trim(loc.FullName(), "/"), "/")
		if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) != "" {
			base = parts[len(parts)-1]
		}
	}
	for _, ext := range []string{".ply", ".splat", ".ksplat"} {
		if strings.HasSuffix(strings.ToLower(base), ext) {
			base = base[:len(base)-len(ext)]
			break
		}
	}
	if base == "" {
		base = "gaussian-splat"
	}
	return base + ".ksplat"
}

func safeKSplatFileName(name string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(name), "/"), "/")
	base := "gaussian-splat.ksplat"
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) != "" {
		base = parts[len(parts)-1]
	}
	if !strings.HasSuffix(strings.ToLower(base), ".ksplat") {
		base += ".ksplat"
	}
	return base
}

func gaussianSplatQuickViewContentURL(id uint) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf("/api/v1/manager/gaussian_splat_quick_view/%d/content", id)
}

func applyGaussianSplatQuickViewResultFields(fields map[string]interface{}, result *GaussianSplatQuickViewExecutionResult) {
	if result == nil {
		return
	}
	if strings.TrimSpace(result.StorageRef) != "" {
		fields["storage_ref"] = strings.TrimSpace(result.StorageRef)
	}
	if strings.TrimSpace(result.FileName) != "" {
		fields["file_name"] = strings.TrimSpace(result.FileName)
	}
	if result.SizeBytes > 0 {
		fields["size_bytes"] = result.SizeBytes
	}
	if strings.TrimSpace(result.ContentURL) != "" {
		fields["content_url"] = strings.TrimSpace(result.ContentURL)
	}
	if result.Metadata != nil {
		fields["metadata"] = result.Metadata
	}
}
