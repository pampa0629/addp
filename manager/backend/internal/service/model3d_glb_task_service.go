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

type Model3DGLBExecutor interface {
	BuildModel3DGLB(ctx context.Context, req Model3DGLBExecutionRequest) (*Model3DGLBExecutionResult, error)
}

type Model3DGLBExecutionRequest struct {
	Task        *models.Model3DGLBTask
	ExecutionID string
	Config      Model3DGLBExecutionConfig
}

type Model3DGLBExecutionResult struct {
	StorageRef string
	FileName   string
	SizeBytes  int64
	ContentURL string
	Metadata   commonModels.JSONMap
}

type Model3DGLBExecutionConfig struct {
	Source  Model3DGLBSourceConfig
	Result  Model3DGLBResultConfig
	Options commonModels.JSONMap
}

type Model3DGLBSourceConfig struct {
	ItemLocator     string
	SourceEngineID  uint
	ItemFingerprint string
	ItemID          uint
	Format          string
	SourceSizeBytes int64
}

type Model3DGLBResultConfig struct {
	StorageRef string
	FileName   string
}

type Model3DGLBCleaner interface {
	DeleteByStorageRef(ctx context.Context, storageRef string) error
}

type Model3DGLBTaskService struct {
	repo     *repository.Model3DGLBRepository
	executor Model3DGLBExecutor
	cleaner  Model3DGLBCleaner
	bucket   string
}

func NewModel3DGLBTaskService(repo *repository.Model3DGLBRepository) *Model3DGLBTaskService {
	return &Model3DGLBTaskService{repo: repo}
}

func (s *Model3DGLBTaskService) SetExecutor(executor Model3DGLBExecutor) {
	s.executor = executor
}

func (s *Model3DGLBTaskService) SetCleaner(cleaner Model3DGLBCleaner) {
	s.cleaner = cleaner
}

func (s *Model3DGLBTaskService) SetBucket(bucket string) {
	s.bucket = strings.TrimSpace(bucket)
}

func (s *Model3DGLBTaskService) Create(ctx context.Context, task *models.Model3DGLBTask) error {
	if err := normalizeModel3DGLBTask(task, s.bucket); err != nil {
		return err
	}
	cfg, err := normalizeModel3DGLBTaskConfig(task.Config, s.bucket, task.TenantID)
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
		if strings.Contains(err.Error(), "idx_model_3d_glb_tasks_source_unique") {
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

func (s *Model3DGLBTaskService) GetByID(ctx context.Context, id uint, tenantID uint) (*models.Model3DGLBTask, error) {
	return s.repo.GetTask(ctx, id, tenantID)
}

func (s *Model3DGLBTaskService) List(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.Model3DGLBTask, int64, error) {
	return s.repo.ListTasks(ctx, tenantID, page, pageSize)
}

func (s *Model3DGLBTaskService) Update(ctx context.Context, task *models.Model3DGLBTask) error {
	if err := normalizeModel3DGLBTask(task, s.bucket); err != nil {
		return err
	}
	return s.repo.UpdateTask(ctx, task)
}

func (s *Model3DGLBTaskService) Delete(ctx context.Context, id uint, tenantID uint) error {
	return s.repo.DeleteTask(ctx, id, tenantID)
}

func (s *Model3DGLBTaskService) reuseExistingTask(ctx context.Context, task *models.Model3DGLBTask, existing *models.Model3DGLBTask) error {
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

func (s *Model3DGLBTaskService) ListResults(ctx context.Context, filter repository.Model3DGLBFilter) ([]*models.Model3DGLB, int64, error) {
	return s.repo.List(ctx, filter)
}

func (s *Model3DGLBTaskService) GetResult(ctx context.Context, id uint, tenantID uint) (*models.Model3DGLB, error) {
	return s.repo.GetByID(ctx, id, tenantID)
}

func (s *Model3DGLBTaskService) DeleteResult(ctx context.Context, id uint, tenantID uint) error {
	result, err := s.repo.GetByID(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("model 3d GLB result not found")
	}
	if strings.TrimSpace(result.StorageRef) != "" && s.cleaner != nil {
		if err := s.cleaner.DeleteByStorageRef(ctx, result.StorageRef); err != nil {
			return err
		}
	}
	return s.repo.Delete(ctx, id, tenantID)
}

func (s *Model3DGLBTaskService) Execute(ctx context.Context, taskID uint, tenantID uint, triggerType string, source string, parentExecutionID *string, overwriteExistingResult bool) (string, error) {
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
	currentStep := "生成三维模型 GLB 快显"
	exec := &commonExecution.TaskExecution{
		ExecutionID:       executionID,
		TenantID:          int(tenantID),
		Module:            commonExecution.ModuleManager,
		TaskType:          commonExecution.TaskTypeModel3DGLBGeneration,
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
			return "", ErrTaskExecutionBusy
		}
		return "", err
	}

	go s.runModel3DGLBGeneration(context.Background(), claimedTask, executionID)
	return executionID, nil
}

func (s *Model3DGLBTaskService) runModel3DGLBGeneration(ctx context.Context, task *models.Model3DGLBTask, executionID string) {
	startedAt := time.Now()
	if err := s.repo.StartExecution(ctx, task.ID, task.TenantID, executionID, startedAt); err != nil {
		logger.L().Warn("领取三维模型 GLB execution 失败", "execution_id", executionID, "task_id", task.ID, "error", err)
		return
	}
	status := commonExecution.ExecutionStatusSuccess
	metadata := commonModels.JSONMap{}
	var errDetails commonModels.JSONMap
	var resultFields map[string]interface{}

	result, execCfg, err := s.prepareResult(ctx, task, executionID)
	var buildResult *Model3DGLBExecutionResult
	if err == nil {
		if s.executor == nil {
			err = errors.New("model 3d GLB generation executor is not configured")
		} else {
			buildResult, err = s.executor.BuildModel3DGLB(ctx, Model3DGLBExecutionRequest{Task: task, ExecutionID: executionID, Config: execCfg})
		}
	}
	if err == nil && buildResult == nil {
		err = errors.New("model 3d GLB generation executor returned no result")
	}
	if err != nil {
		status = commonExecution.ExecutionStatusFailed
		errDetails = commonModels.JSONMap{"message": err.Error()}
		metadata = commonModels.JSONMap{"error": err.Error()}
		if result != nil {
			resultFields = map[string]interface{}{
				"status":            models.Model3DGLBStatusFailed,
				"error_message":     err.Error(),
				"last_execution_id": executionID,
			}
		}
	} else if result != nil {
		if buildResult.ContentURL == "" {
			buildResult.ContentURL = model3DGLBContentURL(result.ID)
		}
		resultFields = map[string]interface{}{
			"status":            models.Model3DGLBStatusReady,
			"error_message":     "",
			"last_execution_id": executionID,
		}
		applyModel3DGLBResultFields(resultFields, buildResult)
		metadata = buildResult.Metadata.Clone()
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
		metadata["result_id"] = result.ID
		metadata["storage_ref"] = buildResult.StorageRef
		metadata["content_url"] = buildResult.ContentURL
	}

	completedAt := time.Now()
	progress := 100
	if status != commonExecution.ExecutionStatusSuccess {
		progress = 0
	}
	durationMs := completedAt.Sub(startedAt).Milliseconds()
	resultID := uint(0)
	if result != nil {
		resultID = result.ID
	}
	if err := s.repo.CompleteExecution(ctx, task.ID, task.TenantID, executionID, resultID, resultFields, map[string]interface{}{
		"status":            status,
		"progress":          progress,
		"metadata":          metadata,
		"error_details":     errDetails,
		"completed_at":      completedAt,
		"execution_time_ms": durationMs,
		"updated_at":        completedAt,
	}, completedAt); err != nil {
		logger.L().Warn("提交三维模型 GLB execution 终态失败", "execution_id", executionID, "task_id", task.ID, "error", err)
	}
}

func (s *Model3DGLBTaskService) prepareResult(ctx context.Context, task *models.Model3DGLBTask, executionID string) (*models.Model3DGLB, Model3DGLBExecutionConfig, error) {
	execCfg, err := normalizeModel3DGLBTaskConfig(task.Config, s.bucket, task.TenantID)
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
			"status":            models.Model3DGLBStatusBuilding,
			"metadata":          commonModels.JSONMap{},
			"error_message":     "",
			"content_url":       model3DGLBContentURL(existing.ID),
			"updated_at":        time.Now(),
		}
		if err := s.repo.UpdateFields(ctx, existing.ID, task.TenantID, updates); err != nil {
			return nil, execCfg, err
		}
		existing.StorageRef = execCfg.Result.StorageRef
		existing.FileName = execCfg.Result.FileName
		return existing, execCfg, nil
	}
	result := &models.Model3DGLB{
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
		Status:          models.Model3DGLBStatusBuilding,
		Metadata:        commonModels.JSONMap{},
		CreatedBy:       task.CreatedBy,
	}
	if err := s.repo.Create(ctx, result); err != nil {
		return nil, execCfg, err
	}
	result.ContentURL = model3DGLBContentURL(result.ID)
	_ = s.repo.UpdateFields(ctx, result.ID, task.TenantID, map[string]interface{}{"content_url": result.ContentURL})
	return result, execCfg, nil
}

func normalizeModel3DGLBTask(task *models.Model3DGLBTask, bucket string) error {
	if task == nil {
		return errors.New("model 3d GLB generation task is nil")
	}
	task.Name = strings.TrimSpace(task.Name)
	task.Description = strings.TrimSpace(task.Description)
	task.Schedule = strings.TrimSpace(task.Schedule)
	if task.Config == nil {
		task.Config = commonModels.JSONMap{}
	}
	if task.Name == "" {
		return errors.New("model 3d GLB generation task name is required")
	}
	if len(task.Config) == 0 {
		return errors.New("model 3d GLB generation task config is required")
	}
	if task.Schedule != "" || task.NextRunAt != nil {
		return errors.New("model 3d GLB generation task does not support schedule")
	}
	_, err := normalizeModel3DGLBTaskConfig(task.Config, bucket, task.TenantID)
	return err
}

func normalizeModel3DGLBTaskConfig(config commonModels.JSONMap, bucket string, tenantID uint) (Model3DGLBExecutionConfig, error) {
	source, err := normalizeModel3DGLBSource(config)
	if err != nil {
		return Model3DGLBExecutionConfig{}, err
	}
	result := normalizeModel3DGLBResult(config, source, bucket, tenantID)
	options, ok := asJSONMap(config["options"])
	if !ok || options == nil {
		options = commonModels.JSONMap{}
	}
	config["options"] = options
	return Model3DGLBExecutionConfig{Source: source, Result: result, Options: options}, nil
}

func normalizeModel3DGLBSource(config commonModels.JSONMap) (Model3DGLBSourceConfig, error) {
	sourceMap, ok := asJSONMap(config["source"])
	if !ok {
		return Model3DGLBSourceConfig{}, errors.New("model 3d GLB config.source is required")
	}
	source := Model3DGLBSourceConfig{
		ItemLocator:     stringFromConfig(sourceMap["item_locator"]),
		SourceEngineID:  uintFromConfig(sourceMap["source_engine_id"]),
		ItemFingerprint: strings.TrimSpace(stringFromConfig(sourceMap["item_fingerprint"])),
		ItemID:          uintFromConfig(sourceMap["item_id"]),
		Format:          strings.ToLower(firstNonEmptyConfig(stringFromConfig(sourceMap["format"]), string(format.FormatOSGB))),
		SourceSizeBytes: int64FromConfig(sourceMap["source_size_bytes"], 0),
	}
	if source.ItemLocator == "" || source.SourceEngineID == 0 || source.ItemFingerprint == "" {
		return Model3DGLBSourceConfig{}, errors.New("model 3d GLB config.source requires item_locator, source_engine_id and item_fingerprint")
	}
	if !isModel3DGLBTaskSourceFormat(source.Format) {
		return Model3DGLBSourceConfig{}, errors.New("model 3d GLB config.source.format must be osgb, gltf, fbx, obj, stl or ifc")
	}
	loc, err := resourcetree.ParseURI(source.ItemLocator)
	if err != nil {
		return Model3DGLBSourceConfig{}, fmt.Errorf("model 3d GLB config.source.item_locator is invalid: %w", err)
	}
	if loc.EngineID != source.SourceEngineID {
		return Model3DGLBSourceConfig{}, errors.New("model 3d GLB config.source.item_locator engine_id does not match source_engine_id")
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

func isModel3DGLBTaskSourceFormat(sourceFormat string) bool {
	switch strings.ToLower(strings.TrimSpace(sourceFormat)) {
	case string(format.FormatOSGB), string(format.FormatGLTF), string(format.FormatFBX), string(format.FormatOBJ), string(format.FormatSTL), string(format.FormatIFC):
		return true
	default:
		return false
	}
}

func normalizeModel3DGLBResult(config commonModels.JSONMap, source Model3DGLBSourceConfig, bucket string, tenantID uint) Model3DGLBResultConfig {
	resultMap, _ := asJSONMap(config["result"])
	fileName := safeGLBFileName(firstNonEmptyConfig(stringFromConfig(resultMap["file_name"]), defaultModel3DGLBFileName(source.ItemLocator)))
	storageRef := stringFromConfig(resultMap["storage_ref"])
	if storageRef == "" {
		objectName := joinFilePath(fmt.Sprintf("tenant_%d/model3d-quick-view/%s", tenantID, source.ItemFingerprint), fileName)
		storageRef = rastercogref.ObjectStorageRef(firstNonEmptyConfig(bucket, "manager"), objectName)
	}
	result := Model3DGLBResultConfig{StorageRef: storageRef, FileName: fileName}
	config["result"] = commonModels.JSONMap{
		"storage_ref": result.StorageRef,
		"file_name":   result.FileName,
	}
	return result
}

func defaultModel3DGLBFileName(locator string) string {
	loc, err := resourcetree.ParseURI(locator)
	base := "model"
	if err == nil && loc != nil {
		parts := strings.Split(strings.Trim(loc.FullName(), "/"), "/")
		if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) != "" {
			base = parts[len(parts)-1]
		}
	}
	for _, ext := range []string{".osgb", ".gltf", ".fbx", ".obj", ".stl", ".ifc"} {
		if strings.HasSuffix(strings.ToLower(base), ext) {
			base = base[:len(base)-len(ext)]
			break
		}
	}
	if base == "" {
		base = "model"
	}
	return base + ".glb"
}

func safeGLBFileName(name string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(name), "/"), "/")
	base := "model.glb"
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) != "" {
		base = parts[len(parts)-1]
	}
	if !strings.HasSuffix(strings.ToLower(base), ".glb") {
		base += ".glb"
	}
	return base
}

func model3DGLBContentURL(id uint) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf("/api/v1/manager/model_3d_glb/%d/content", id)
}

func applyModel3DGLBResultFields(fields map[string]interface{}, result *Model3DGLBExecutionResult) {
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

func optionalUint(value uint) *uint {
	if value == 0 {
		return nil
	}
	return &value
}
