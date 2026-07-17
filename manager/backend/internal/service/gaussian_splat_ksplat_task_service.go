package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
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

type GaussianSplatKSplatExecutor interface {
	BuildGaussianSplatKSplat(ctx context.Context, req GaussianSplatKSplatExecutionRequest) (*GaussianSplatKSplatExecutionResult, error)
}

type GaussianSplatKSplatExecutionRequest struct {
	Task        *models.GaussianSplatKSplatTask
	ExecutionID string
	Config      GaussianSplatKSplatExecutionConfig
}

type GaussianSplatKSplatExecutionResult struct {
	StorageRef string
	FileName   string
	SizeBytes  int64
	ContentURL string
	Metadata   commonModels.JSONMap
}

type GaussianSplatKSplatExecutionConfig struct {
	Source  GaussianSplatKSplatSourceConfig
	Result  GaussianSplatKSplatResultConfig
	Options commonModels.JSONMap
}

type GaussianSplatKSplatSourceConfig struct {
	ItemLocator              string
	SourceEngineID           uint
	ItemFingerprint          string
	ItemID                   uint
	Format                   string
	SourceSizeBytes          int64
	Bounds3D                 *datatype.Bounds3D
	SampledBounds3D          *datatype.Bounds3D
	SampledBoundsSampleCount *int64
}

type GaussianSplatKSplatResultConfig struct {
	StorageRef string
	FileName   string
}

type GaussianSplatKSplatCleaner interface {
	DeleteByStorageRef(ctx context.Context, storageRef string) error
}

type GaussianSplatKSplatTaskService struct {
	repo       *repository.GaussianSplatKSplatRepository
	executor   GaussianSplatKSplatExecutor
	cleaner    GaussianSplatKSplatCleaner
	metaClient *commonClient.MetaClient
	bucket     string
}

func NewGaussianSplatKSplatTaskService(repo *repository.GaussianSplatKSplatRepository) *GaussianSplatKSplatTaskService {
	return &GaussianSplatKSplatTaskService{repo: repo}
}

func (s *GaussianSplatKSplatTaskService) SetExecutor(executor GaussianSplatKSplatExecutor) {
	s.executor = executor
}

func (s *GaussianSplatKSplatTaskService) SetCleaner(cleaner GaussianSplatKSplatCleaner) {
	s.cleaner = cleaner
}

func (s *GaussianSplatKSplatTaskService) SetMetaClient(metaClient *commonClient.MetaClient) {
	s.metaClient = metaClient
}

func (s *GaussianSplatKSplatTaskService) SetBucket(bucket string) {
	s.bucket = strings.TrimSpace(bucket)
}

func (s *GaussianSplatKSplatTaskService) Create(ctx context.Context, task *models.GaussianSplatKSplatTask) error {
	if err := normalizeGaussianSplatKSplatTask(task, s.bucket); err != nil {
		return err
	}
	if err := s.enrichGaussianSplatKSplatTaskSourceFacts(ctx, task); err != nil {
		return err
	}
	cfg, err := normalizeGaussianSplatKSplatTaskConfig(task.Config, s.bucket, task.TenantID)
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
		if strings.Contains(err.Error(), "idx_gaussian_splat_ksplat_tasks_source_unique") {
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

func (s *GaussianSplatKSplatTaskService) GetByID(ctx context.Context, id uint, tenantID uint) (*models.GaussianSplatKSplatTask, error) {
	return s.repo.GetTask(ctx, id, tenantID)
}

func (s *GaussianSplatKSplatTaskService) List(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.GaussianSplatKSplatTask, int64, error) {
	return s.repo.ListTasks(ctx, tenantID, page, pageSize)
}

func (s *GaussianSplatKSplatTaskService) Update(ctx context.Context, task *models.GaussianSplatKSplatTask) error {
	if err := normalizeGaussianSplatKSplatTask(task, s.bucket); err != nil {
		return err
	}
	if err := s.enrichGaussianSplatKSplatTaskSourceFacts(ctx, task); err != nil {
		return err
	}
	return s.repo.UpdateTask(ctx, task)
}

func (s *GaussianSplatKSplatTaskService) Delete(ctx context.Context, id uint, tenantID uint) error {
	return s.repo.DeleteTask(ctx, id, tenantID)
}

func (s *GaussianSplatKSplatTaskService) reuseExistingTask(ctx context.Context, task *models.GaussianSplatKSplatTask, existing *models.GaussianSplatKSplatTask) error {
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

func (s *GaussianSplatKSplatTaskService) ListResults(ctx context.Context, filter repository.GaussianSplatKSplatFilter) ([]*models.GaussianSplatKSplat, int64, error) {
	return s.repo.List(ctx, filter)
}

func (s *GaussianSplatKSplatTaskService) GetResult(ctx context.Context, id uint, tenantID uint) (*models.GaussianSplatKSplat, error) {
	return s.repo.GetByID(ctx, id, tenantID)
}

func (s *GaussianSplatKSplatTaskService) DeleteResult(ctx context.Context, id uint, tenantID uint) error {
	result, err := s.repo.GetByID(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("gaussian splat KSplat result not found")
	}
	if strings.TrimSpace(result.StorageRef) != "" && s.cleaner != nil {
		if err := s.cleaner.DeleteByStorageRef(ctx, result.StorageRef); err != nil {
			return err
		}
	}
	return s.repo.Delete(ctx, id, tenantID)
}

func (s *GaussianSplatKSplatTaskService) Execute(ctx context.Context, taskID uint, tenantID uint, triggerType string, source string, parentExecutionID *string, confirmExistingResult bool) (string, error) {
	task, err := s.repo.GetTask(ctx, taskID, tenantID)
	if err != nil {
		return "", err
	}
	if task == nil {
		return "", ErrTaskNotFound
	}
	if err := s.enrichGaussianSplatKSplatTaskSourceFacts(ctx, task); err != nil {
		return "", err
	}
	if err := s.repo.UpdateTask(ctx, task); err != nil {
		return "", err
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
	currentStep := "生成 3DGS KSplat 快显"
	exec := &commonExecution.TaskExecution{
		ExecutionID:       executionID,
		TenantID:          int(tenantID),
		Module:            commonExecution.ModuleManager,
		TaskType:          commonExecution.TaskTypeGaussianSplatKSplatGeneration,
		Source:            normalizedSource,
		ParentExecutionID: parentExecutionID,
		Status:            commonExecution.ExecutionStatusPending,
		Progress:          0,
		CurrentStep:       &currentStep,
		TriggerType:       normalizedTriggerType,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	claimedTask, err := s.repo.ClaimExecution(ctx, taskID, tenantID, exec, confirmExistingResult)
	if err != nil {
		if errors.Is(err, repository.ErrExistingResultConfirmationRequired) {
			return "", ErrExistingResultConfirmationRequired
		}
		if errors.Is(err, commonAPI.ErrNotFound) {
			return "", ErrTaskNotFound
		}
		if errors.Is(err, commonAPI.ErrConflict) {
			return "", ErrTaskExecutionBusy
		}
		return "", err
	}

	go s.runGaussianSplatKSplatGeneration(context.Background(), claimedTask, executionID)
	return executionID, nil
}

func (s *GaussianSplatKSplatTaskService) runGaussianSplatKSplatGeneration(ctx context.Context, task *models.GaussianSplatKSplatTask, executionID string) {
	startedAt := time.Now()
	if err := s.repo.StartExecution(ctx, task.ID, task.TenantID, executionID, startedAt); err != nil {
		logger.L().Warn("领取 3DGS KSplat execution 失败", "execution_id", executionID, "task_id", task.ID, "error", err)
		return
	}
	status := commonExecution.ExecutionStatusSuccess
	metadata := commonModels.JSONMap{}
	var errDetails commonModels.JSONMap
	var resultFields map[string]interface{}

	result, execCfg, err := s.prepareResult(ctx, task, executionID)
	var buildResult *GaussianSplatKSplatExecutionResult
	if err == nil {
		if s.executor == nil {
			err = errors.New("gaussian splat KSplat generation executor is not configured")
		} else {
			buildResult, err = s.executor.BuildGaussianSplatKSplat(ctx, GaussianSplatKSplatExecutionRequest{Task: task, ExecutionID: executionID, Config: execCfg})
		}
	}
	if err == nil && buildResult == nil {
		err = errors.New("gaussian splat KSplat generation executor returned no result")
	}
	if err != nil {
		status = commonExecution.ExecutionStatusFailed
		errDetails = commonModels.JSONMap{"message": err.Error()}
		metadata = commonModels.JSONMap{"error": err.Error()}
		if result != nil {
			resultFields = map[string]interface{}{
				"status":            models.GaussianSplatKSplatStatusFailed,
				"error_message":     err.Error(),
				"last_execution_id": executionID,
			}
		}
	} else if result != nil {
		if buildResult.ContentURL == "" {
			buildResult.ContentURL = gaussianSplatKSplatContentURL(result.ID)
		}
		resultFields = map[string]interface{}{
			"status":            models.GaussianSplatKSplatStatusReady,
			"error_message":     "",
			"last_execution_id": executionID,
		}
		applyGaussianSplatKSplatResultFields(resultFields, buildResult)
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
		logger.L().Warn("提交 3DGS KSplat execution 终态失败", "execution_id", executionID, "task_id", task.ID, "error", err)
	}
}

func (s *GaussianSplatKSplatTaskService) prepareResult(ctx context.Context, task *models.GaussianSplatKSplatTask, executionID string) (*models.GaussianSplatKSplat, GaussianSplatKSplatExecutionConfig, error) {
	execCfg, err := normalizeGaussianSplatKSplatTaskConfig(task.Config, s.bucket, task.TenantID)
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
			"status":            models.GaussianSplatKSplatStatusBuilding,
			"metadata":          commonModels.JSONMap{},
			"error_message":     "",
			"content_url":       gaussianSplatKSplatContentURL(existing.ID),
			"updated_at":        time.Now(),
		}
		if err := s.repo.UpdateFields(ctx, existing.ID, task.TenantID, updates); err != nil {
			return nil, execCfg, err
		}
		existing.StorageRef = execCfg.Result.StorageRef
		existing.FileName = execCfg.Result.FileName
		return existing, execCfg, nil
	}
	result := &models.GaussianSplatKSplat{
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
		Status:          models.GaussianSplatKSplatStatusBuilding,
		Metadata:        commonModels.JSONMap{},
		CreatedBy:       task.CreatedBy,
	}
	if err := s.repo.Create(ctx, result); err != nil {
		return nil, execCfg, err
	}
	result.ContentURL = gaussianSplatKSplatContentURL(result.ID)
	_ = s.repo.UpdateFields(ctx, result.ID, task.TenantID, map[string]interface{}{"content_url": result.ContentURL})
	return result, execCfg, nil
}

func normalizeGaussianSplatKSplatTask(task *models.GaussianSplatKSplatTask, bucket string) error {
	if task == nil {
		return errors.New("gaussian splat KSplat generation task is nil")
	}
	task.Name = strings.TrimSpace(task.Name)
	task.Description = strings.TrimSpace(task.Description)
	task.Schedule = strings.TrimSpace(task.Schedule)
	if task.Config == nil {
		task.Config = commonModels.JSONMap{}
	}
	if task.Name == "" {
		return errors.New("gaussian splat KSplat generation task name is required")
	}
	if len(task.Config) == 0 {
		return errors.New("gaussian splat KSplat generation task config is required")
	}
	if task.Schedule != "" || task.NextRunAt != nil {
		return errors.New("gaussian splat KSplat generation task does not support schedule")
	}
	_, err := normalizeGaussianSplatKSplatTaskConfig(task.Config, bucket, task.TenantID)
	return err
}

func normalizeGaussianSplatKSplatTaskConfig(config commonModels.JSONMap, bucket string, tenantID uint) (GaussianSplatKSplatExecutionConfig, error) {
	source, err := normalizeGaussianSplatKSplatSource(config)
	if err != nil {
		return GaussianSplatKSplatExecutionConfig{}, err
	}
	result := normalizeGaussianSplatKSplatResult(config, source, bucket, tenantID)
	options, ok := asJSONMap(config["options"])
	if !ok || options == nil {
		options = commonModels.JSONMap{}
	}
	config["options"] = options
	return GaussianSplatKSplatExecutionConfig{Source: source, Result: result, Options: options}, nil
}

func normalizeGaussianSplatKSplatSource(config commonModels.JSONMap) (GaussianSplatKSplatSourceConfig, error) {
	sourceMap, ok := asJSONMap(config["source"])
	if !ok {
		return GaussianSplatKSplatSourceConfig{}, errors.New("gaussian splat KSplat config.source is required")
	}
	source := GaussianSplatKSplatSourceConfig{
		ItemLocator:              stringFromConfig(sourceMap["item_locator"]),
		SourceEngineID:           uintFromConfig(sourceMap["source_engine_id"]),
		ItemFingerprint:          strings.TrimSpace(stringFromConfig(sourceMap["item_fingerprint"])),
		ItemID:                   uintFromConfig(sourceMap["item_id"]),
		Format:                   strings.ToLower(strings.TrimSpace(stringFromConfig(sourceMap["format"]))),
		SourceSizeBytes:          int64FromConfig(sourceMap["source_size_bytes"], 0),
		Bounds3D:                 bounds3DFromTaskConfig(sourceMap["bounds_3d"]),
		SampledBounds3D:          bounds3DFromTaskConfig(sourceMap["sampled_bounds_3d"]),
		SampledBoundsSampleCount: int64PtrFromConfig(sourceMap["sampled_bounds_sample_count"]),
	}
	if source.ItemLocator == "" || source.SourceEngineID == 0 || source.ItemFingerprint == "" {
		return GaussianSplatKSplatSourceConfig{}, errors.New("gaussian splat KSplat config.source requires item_locator, source_engine_id and item_fingerprint")
	}
	if !isGaussianSplatKSplatTaskSourceFormat(source.Format) {
		return GaussianSplatKSplatSourceConfig{}, errors.New("gaussian splat KSplat config.source.format must be ply or splat")
	}
	loc, err := resourcetree.ParseURI(source.ItemLocator)
	if err != nil {
		return GaussianSplatKSplatSourceConfig{}, fmt.Errorf("gaussian splat KSplat config.source.item_locator is invalid: %w", err)
	}
	if loc.EngineID != source.SourceEngineID {
		return GaussianSplatKSplatSourceConfig{}, errors.New("gaussian splat KSplat config.source.item_locator engine_id does not match source_engine_id")
	}
	normalized := commonModels.JSONMap{
		"item_locator":      source.ItemLocator,
		"source_engine_id":  source.SourceEngineID,
		"item_fingerprint":  source.ItemFingerprint,
		"item_id":           source.ItemID,
		"format":            source.Format,
		"source_size_bytes": source.SourceSizeBytes,
	}
	if bounds := bounds3DToTaskConfig(source.Bounds3D); bounds != nil {
		normalized["bounds_3d"] = bounds
	}
	if bounds := bounds3DToTaskConfig(source.SampledBounds3D); bounds != nil {
		normalized["sampled_bounds_3d"] = bounds
	}
	if source.SampledBoundsSampleCount != nil {
		normalized["sampled_bounds_sample_count"] = *source.SampledBoundsSampleCount
	}
	config["source"] = normalized
	return source, nil
}

func isGaussianSplatKSplatTaskSourceFormat(sourceFormat string) bool {
	switch strings.ToLower(strings.TrimSpace(sourceFormat)) {
	case string(format.FormatPLY), string(format.FormatSplat):
		return true
	default:
		return false
	}
}

func bounds3DFromTaskConfig(value interface{}) *datatype.Bounds3D {
	payload, ok := asJSONMap(value)
	if !ok {
		return nil
	}
	return bounds3DFromPayload(payload)
}

func (s *GaussianSplatKSplatTaskService) enrichGaussianSplatKSplatTaskSourceFacts(ctx context.Context, task *models.GaussianSplatKSplatTask) error {
	if s == nil || s.metaClient == nil || task == nil || task.Config == nil {
		return nil
	}
	sourceMap, ok := asJSONMap(task.Config["source"])
	if !ok || sourceMap == nil {
		return nil
	}
	if bounds3DFromTaskConfig(sourceMap["bounds_3d"]) != nil && bounds3DFromTaskConfig(sourceMap["sampled_bounds_3d"]) != nil {
		return nil
	}
	itemID := uintFromConfig(sourceMap["item_id"])
	if itemID == 0 {
		locator := strings.TrimSpace(stringFromConfig(sourceMap["item_locator"]))
		if loc, err := resourcetree.ParseURI(locator); err == nil && loc.ItemID != nil {
			itemID = *loc.ItemID
		}
	}
	if itemID == 0 {
		return nil
	}
	metaClient := s.metaClient.WithTenantID(task.TenantID)
	item, err := metaClient.GetItemByID(itemID)
	if err != nil {
		return fmt.Errorf("load gaussian splat source metadata: %w", err)
	}
	if item == nil || item.Attributes == nil {
		return nil
	}
	source := GaussianSplatKSplatSourceFromAttributes(item.Attributes)
	if source == nil {
		return nil
	}
	if sourceMap["bounds_3d"] == nil {
		if bounds := bounds3DToTaskConfig(source.Bounds3D); bounds != nil {
			sourceMap["bounds_3d"] = bounds
		}
	}
	if sourceMap["sampled_bounds_3d"] == nil {
		if bounds := bounds3DToTaskConfig(source.SampledBounds3D); bounds != nil {
			sourceMap["sampled_bounds_3d"] = bounds
		}
	}
	if sourceMap["sampled_bounds_sample_count"] == nil && source.SampledBoundsSampleCount != nil {
		sourceMap["sampled_bounds_sample_count"] = *source.SampledBoundsSampleCount
	}
	task.Config["source"] = sourceMap
	return nil
}

func bounds3DToTaskConfig(bounds *datatype.Bounds3D) commonModels.JSONMap {
	bounds = datatype.NormalizeBounds3D(bounds)
	if bounds == nil {
		return nil
	}
	payload := commonModels.JSONMap{}
	if bounds.MinX != nil {
		payload["min_x"] = *bounds.MinX
	}
	if bounds.MinY != nil {
		payload["min_y"] = *bounds.MinY
	}
	if bounds.MinZ != nil {
		payload["min_z"] = *bounds.MinZ
	}
	if bounds.MaxX != nil {
		payload["max_x"] = *bounds.MaxX
	}
	if bounds.MaxY != nil {
		payload["max_y"] = *bounds.MaxY
	}
	if bounds.MaxZ != nil {
		payload["max_z"] = *bounds.MaxZ
	}
	return payload
}

func int64PtrFromConfig(value interface{}) *int64 {
	if value == nil {
		return nil
	}
	parsed := int64FromConfig(value, 0)
	return &parsed
}

func normalizeGaussianSplatKSplatResult(config commonModels.JSONMap, source GaussianSplatKSplatSourceConfig, bucket string, tenantID uint) GaussianSplatKSplatResultConfig {
	resultMap, _ := asJSONMap(config["result"])
	fileName := safeKSplatFileName(firstNonEmptyConfig(stringFromConfig(resultMap["file_name"]), defaultGaussianSplatKSplatFileName(source.ItemLocator)))
	storageRef := stringFromConfig(resultMap["storage_ref"])
	if storageRef == "" {
		objectName := joinFilePath(fmt.Sprintf("tenant_%d/gaussian-splat-ksplat/%s", tenantID, source.ItemFingerprint), fileName)
		storageRef = rastercogref.ObjectStorageRef(firstNonEmptyConfig(bucket, "manager"), objectName)
	}
	result := GaussianSplatKSplatResultConfig{StorageRef: storageRef, FileName: fileName}
	config["result"] = commonModels.JSONMap{
		"storage_ref": result.StorageRef,
		"file_name":   result.FileName,
	}
	return result
}

func defaultGaussianSplatKSplatFileName(locator string) string {
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

func gaussianSplatKSplatContentURL(id uint) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf("/api/v1/manager/gaussian_splat_ksplat/%d/content", id)
}

func applyGaussianSplatKSplatResultFields(fields map[string]interface{}, result *GaussianSplatKSplatExecutionResult) {
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
