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

type PointCloudCOPCExecutor interface {
	BuildPointCloudCOPC(ctx context.Context, req PointCloudCOPCExecutionRequest) (*PointCloudCOPCExecutionResult, error)
}

type PointCloudCOPCExecutionRequest struct {
	Task        *models.PointCloudCOPCTask
	ExecutionID string
	Config      PointCloudCOPCExecutionConfig
}

type PointCloudCOPCExecutionResult struct {
	StorageRef string
	FileName   string
	SizeBytes  int64
	ContentURL string
	Metadata   commonModels.JSONMap
}

type PointCloudCOPCProgressEvent struct {
	Phase           string
	Event           string
	Message         string
	OverallProgress *int
	Metadata        commonModels.JSONMap
}

type PointCloudCOPCExecutionConfig struct {
	Source  PointCloudCOPCSourceConfig
	Result  PointCloudCOPCResultConfig
	Options commonModels.JSONMap
}

type PointCloudCOPCSourceConfig struct {
	ItemLocator     string
	SourceEngineID  uint
	ItemFingerprint string
	ItemID          uint
	Format          string
	SourceSizeBytes int64
}

type PointCloudCOPCResultConfig struct {
	StorageRef string
	FileName   string
}

type PointCloudCOPCCleaner interface {
	DeleteByStorageRef(ctx context.Context, storageRef string) error
}

var (
	ErrPointCloudCOPCProgressTargetMismatch = errors.New("point cloud COPC progress event target mismatch")
	ErrPointCloudCOPCExecutionCompleted     = errors.New("point cloud COPC execution is already completed")
	ErrPointCloudCOPCExecutionNotRunning    = errors.New("point cloud COPC execution is not running")
)

type PointCloudCOPCTaskService struct {
	repo     *repository.PointCloudCOPCRepository
	executor PointCloudCOPCExecutor
	cleaner  PointCloudCOPCCleaner
	bucket   string
}

func NewPointCloudCOPCTaskService(repo *repository.PointCloudCOPCRepository) *PointCloudCOPCTaskService {
	return &PointCloudCOPCTaskService{repo: repo}
}

func (s *PointCloudCOPCTaskService) SetExecutor(executor PointCloudCOPCExecutor) {
	s.executor = executor
}

func (s *PointCloudCOPCTaskService) SetCleaner(cleaner PointCloudCOPCCleaner) {
	s.cleaner = cleaner
}

func (s *PointCloudCOPCTaskService) SetBucket(bucket string) {
	s.bucket = strings.TrimSpace(bucket)
}

func (s *PointCloudCOPCTaskService) Create(ctx context.Context, task *models.PointCloudCOPCTask) error {
	if err := normalizePointCloudCOPCTask(task, s.bucket); err != nil {
		return err
	}
	cfg, err := normalizePointCloudCOPCTaskConfig(task.Config, s.bucket, task.TenantID)
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
		if strings.Contains(err.Error(), "idx_point_cloud_copc_tasks_source_unique") {
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

func (s *PointCloudCOPCTaskService) GetByID(ctx context.Context, id uint, tenantID uint) (*models.PointCloudCOPCTask, error) {
	return s.repo.GetTask(ctx, id, tenantID)
}

func (s *PointCloudCOPCTaskService) List(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.PointCloudCOPCTask, int64, error) {
	return s.repo.ListTasks(ctx, tenantID, page, pageSize)
}

func (s *PointCloudCOPCTaskService) Update(ctx context.Context, task *models.PointCloudCOPCTask) error {
	if err := normalizePointCloudCOPCTask(task, s.bucket); err != nil {
		return err
	}
	return s.repo.UpdateTask(ctx, task)
}

func (s *PointCloudCOPCTaskService) Delete(ctx context.Context, id uint, tenantID uint) error {
	return s.repo.DeleteTask(ctx, id, tenantID)
}

func (s *PointCloudCOPCTaskService) reuseExistingTask(ctx context.Context, task *models.PointCloudCOPCTask, existing *models.PointCloudCOPCTask) error {
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

func (s *PointCloudCOPCTaskService) ListResults(ctx context.Context, filter repository.PointCloudCOPCFilter) ([]*models.PointCloudCOPC, int64, error) {
	return s.repo.List(ctx, filter)
}

func (s *PointCloudCOPCTaskService) GetResult(ctx context.Context, id uint, tenantID uint) (*models.PointCloudCOPC, error) {
	return s.repo.GetByID(ctx, id, tenantID)
}

func (s *PointCloudCOPCTaskService) DeleteResult(ctx context.Context, id uint, tenantID uint) error {
	result, err := s.repo.GetByID(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("point cloud COPC result not found")
	}
	if strings.TrimSpace(result.StorageRef) != "" && s.cleaner != nil {
		if err := s.cleaner.DeleteByStorageRef(ctx, result.StorageRef); err != nil {
			return err
		}
	}
	return s.repo.Delete(ctx, id, tenantID)
}

func (s *PointCloudCOPCTaskService) Execute(ctx context.Context, taskID uint, tenantID uint, triggerType string, source string, parentExecutionID *string, overwriteExistingResult bool) (string, error) {
	task, err := s.repo.GetTask(ctx, taskID, tenantID)
	if err != nil {
		return "", err
	}
	if task == nil {
		return "", ErrTaskNotFound
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
	currentStep := "生成点云 COPC 快显"
	exec := &commonExecution.TaskExecution{
		ExecutionID:       executionID,
		TenantID:          int(tenantID),
		Module:            commonExecution.ModuleManager,
		TaskType:          commonExecution.TaskTypePointCloudCOPCGeneration,
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

	go s.runPointCloudCOPCGeneration(context.Background(), claimedTask, executionID)
	return executionID, nil
}

func (s *PointCloudCOPCTaskService) RecordProgressEvent(ctx context.Context, tenantID uint, executionID string, event PointCloudCOPCProgressEvent) error {
	executionID = strings.TrimSpace(executionID)
	if executionID == "" {
		return errors.New("execution_id is required")
	}
	if tenantID == 0 {
		return errors.New("tenant_id is required")
	}
	event.Phase = strings.TrimSpace(event.Phase)
	event.Event = strings.TrimSpace(event.Event)
	event.Message = strings.TrimSpace(event.Message)
	if event.Phase == "" {
		return errors.New("phase is required")
	}
	if event.Event == "" {
		return errors.New("event is required")
	}

	exec, err := s.repo.GetExecution(ctx, tenantID, executionID)
	if err != nil {
		return err
	}
	if exec.Module != commonExecution.ModuleManager || exec.TaskType != commonExecution.TaskTypePointCloudCOPCGeneration {
		return ErrPointCloudCOPCProgressTargetMismatch
	}
	if exec.IsCompleted() {
		return ErrPointCloudCOPCExecutionCompleted
	}
	if exec.Status != commonExecution.ExecutionStatusRunning {
		return ErrPointCloudCOPCExecutionNotRunning
	}

	now := time.Now()
	nextProgress := pointCloudCOPCProgressPercent(event, exec.Progress)
	currentStep := pointCloudCOPCProgressStep(event)
	metadata := pointCloudCOPCProgressMetadata(exec.Metadata, event)
	elapsedMs := int64(0)
	if exec.StartedAt != nil {
		elapsedMs = now.Sub(*exec.StartedAt).Milliseconds()
	}

	fields := map[string]interface{}{
		"progress":     nextProgress,
		"current_step": currentStep,
		"metadata":     metadata,
		"updated_at":   now,
	}
	if elapsedMs >= 0 {
		fields["execution_time_ms"] = elapsedMs
	}
	if err := s.repo.UpdateRunningExecutionProgress(ctx, tenantID, executionID, fields); errors.Is(err, commonAPI.ErrConflict) {
		return ErrPointCloudCOPCExecutionNotRunning
	} else {
		return err
	}
}

func (s *PointCloudCOPCTaskService) runPointCloudCOPCGeneration(ctx context.Context, task *models.PointCloudCOPCTask, executionID string) {
	startedAt := time.Now()
	if err := s.repo.StartExecution(ctx, task.ID, task.TenantID, executionID, startedAt); err != nil {
		logger.L().Warn("领取点云 COPC execution 失败", "execution_id", executionID, "task_id", task.ID, "error", err)
		return
	}
	status := commonExecution.ExecutionStatusSuccess
	metadata := commonModels.JSONMap{}
	var errDetails commonModels.JSONMap
	var resultFields map[string]interface{}

	result, execCfg, err := s.prepareResult(ctx, task, executionID)
	var buildResult *PointCloudCOPCExecutionResult
	if err == nil {
		if s.executor == nil {
			err = errors.New("point cloud COPC generation executor is not configured")
		} else {
			buildResult, err = s.executor.BuildPointCloudCOPC(ctx, PointCloudCOPCExecutionRequest{Task: task, ExecutionID: executionID, Config: execCfg})
		}
	}
	if err == nil && buildResult == nil {
		err = errors.New("point cloud COPC generation executor returned no result")
	}
	if err != nil {
		status = commonExecution.ExecutionStatusFailed
		errDetails = commonModels.JSONMap{"message": err.Error()}
		metadata = commonModels.JSONMap{"error": err.Error()}
		if result != nil {
			resultFields = map[string]interface{}{
				"status":            models.PointCloudCOPCStatusFailed,
				"error_message":     err.Error(),
				"last_execution_id": executionID,
			}
		}
	} else if result != nil {
		if buildResult.ContentURL == "" {
			buildResult.ContentURL = pointCloudCOPCContentURL(result.ID)
		}
		resultFields = map[string]interface{}{
			"status":            models.PointCloudCOPCStatusReady,
			"error_message":     "",
			"last_execution_id": executionID,
		}
		applyPointCloudCOPCResultFields(resultFields, buildResult)
		metadata = buildResult.Metadata.Clone()
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
		metadata["result_id"] = result.ID
		metadata["storage_ref"] = buildResult.StorageRef
		metadata["content_url"] = buildResult.ContentURL
		if outputRef, lineageErr := managerInfraObjectLineageRef(buildResult.StorageRef, s.bucket); lineageErr == nil {
			metadata = managerExecutionLineage(metadata, commonExecution.TaskTypePointCloudCOPCGeneration,
				[]commonExecution.LineageResourceRef{managerItemLineageRef(execCfg.Source.ItemLocator, execCfg.Source.ItemFingerprint, execCfg.Source.ItemID)},
				[]commonExecution.LineageResourceRef{outputRef},
			)
		}
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
		logger.L().Warn("提交点云 COPC execution 终态失败", "execution_id", executionID, "task_id", task.ID, "error", err)
	}
}

func (s *PointCloudCOPCTaskService) prepareResult(ctx context.Context, task *models.PointCloudCOPCTask, executionID string) (*models.PointCloudCOPC, PointCloudCOPCExecutionConfig, error) {
	execCfg, err := normalizePointCloudCOPCTaskConfig(task.Config, s.bucket, task.TenantID)
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
			"status":            models.PointCloudCOPCStatusBuilding,
			"metadata":          commonModels.JSONMap{},
			"error_message":     "",
			"content_url":       pointCloudCOPCContentURL(existing.ID),
			"updated_at":        time.Now(),
		}
		if err := s.repo.UpdateFields(ctx, existing.ID, task.TenantID, updates); err != nil {
			return nil, execCfg, err
		}
		existing.StorageRef = execCfg.Result.StorageRef
		existing.FileName = execCfg.Result.FileName
		return existing, execCfg, nil
	}
	result := &models.PointCloudCOPC{
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
		Status:          models.PointCloudCOPCStatusBuilding,
		Metadata:        commonModels.JSONMap{},
		CreatedBy:       task.CreatedBy,
	}
	if err := s.repo.Create(ctx, result); err != nil {
		return nil, execCfg, err
	}
	result.ContentURL = pointCloudCOPCContentURL(result.ID)
	_ = s.repo.UpdateFields(ctx, result.ID, task.TenantID, map[string]interface{}{"content_url": result.ContentURL})
	return result, execCfg, nil
}

func normalizePointCloudCOPCTask(task *models.PointCloudCOPCTask, bucket string) error {
	if task == nil {
		return errors.New("point cloud COPC generation task is nil")
	}
	task.Name = strings.TrimSpace(task.Name)
	task.Description = strings.TrimSpace(task.Description)
	task.Schedule = strings.TrimSpace(task.Schedule)
	if task.Config == nil {
		task.Config = commonModels.JSONMap{}
	}
	if task.Name == "" {
		return errors.New("point cloud COPC generation task name is required")
	}
	if len(task.Config) == 0 {
		return errors.New("point cloud COPC generation task config is required")
	}
	if task.Schedule != "" || task.NextRunAt != nil {
		return errors.New("point cloud COPC generation task does not support schedule")
	}
	_, err := normalizePointCloudCOPCTaskConfig(task.Config, bucket, task.TenantID)
	return err
}

func normalizePointCloudCOPCTaskConfig(config commonModels.JSONMap, bucket string, tenantID uint) (PointCloudCOPCExecutionConfig, error) {
	source, err := normalizePointCloudCOPCSource(config)
	if err != nil {
		return PointCloudCOPCExecutionConfig{}, err
	}
	result := normalizePointCloudCOPCResult(config, source, bucket, tenantID)
	options, ok := asJSONMap(config["options"])
	if !ok || options == nil {
		options = commonModels.JSONMap{}
	}
	config["options"] = options
	return PointCloudCOPCExecutionConfig{Source: source, Result: result, Options: options}, nil
}

func normalizePointCloudCOPCSource(config commonModels.JSONMap) (PointCloudCOPCSourceConfig, error) {
	sourceMap, ok := asJSONMap(config["source"])
	if !ok {
		return PointCloudCOPCSourceConfig{}, errors.New("point cloud COPC config.source is required")
	}
	source := PointCloudCOPCSourceConfig{
		ItemLocator:     stringFromConfig(sourceMap["item_locator"]),
		SourceEngineID:  uintFromConfig(sourceMap["source_engine_id"]),
		ItemFingerprint: strings.TrimSpace(stringFromConfig(sourceMap["item_fingerprint"])),
		ItemID:          uintFromConfig(sourceMap["item_id"]),
		Format:          strings.ToLower(strings.TrimSpace(stringFromConfig(sourceMap["format"]))),
		SourceSizeBytes: int64FromConfig(sourceMap["source_size_bytes"], 0),
	}
	if source.ItemLocator == "" || source.SourceEngineID == 0 || source.ItemFingerprint == "" {
		return PointCloudCOPCSourceConfig{}, errors.New("point cloud COPC config.source requires item_locator, source_engine_id and item_fingerprint")
	}
	if !isPointCloudCOPCTaskSourceFormat(source.Format) {
		return PointCloudCOPCSourceConfig{}, errors.New("point cloud COPC config.source.format must be las, laz, e57, pcd or xyz")
	}
	loc, err := resourcetree.ParseURI(source.ItemLocator)
	if err != nil {
		return PointCloudCOPCSourceConfig{}, fmt.Errorf("point cloud COPC config.source.item_locator is invalid: %w", err)
	}
	if loc.EngineID != source.SourceEngineID {
		return PointCloudCOPCSourceConfig{}, errors.New("point cloud COPC config.source.item_locator engine_id does not match source_engine_id")
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

func isPointCloudCOPCTaskSourceFormat(sourceFormat string) bool {
	switch format.NormalizeFormat(sourceFormat) {
	case format.FormatLAS, format.FormatLAZ, format.FormatE57, format.FormatPCD, format.FormatXYZ:
		return true
	default:
		return false
	}
}

func normalizePointCloudCOPCResult(config commonModels.JSONMap, source PointCloudCOPCSourceConfig, bucket string, tenantID uint) PointCloudCOPCResultConfig {
	resultMap, _ := asJSONMap(config["result"])
	fileName := safeCOPCFileName(firstNonEmptyConfig(stringFromConfig(resultMap["file_name"]), defaultPointCloudCOPCFileName(source.ItemLocator)))
	storageRef := stringFromConfig(resultMap["storage_ref"])
	if storageRef == "" {
		objectName := joinFilePath(fmt.Sprintf("tenant_%d/point-cloud-copc/%s", tenantID, source.ItemFingerprint), fileName)
		storageRef = rastercogref.ObjectStorageRef(firstNonEmptyConfig(bucket, "manager"), objectName)
	}
	result := PointCloudCOPCResultConfig{StorageRef: storageRef, FileName: fileName}
	config["result"] = commonModels.JSONMap{
		"storage_ref": result.StorageRef,
		"file_name":   result.FileName,
	}
	return result
}

func defaultPointCloudCOPCFileName(locator string) string {
	loc, err := resourcetree.ParseURI(locator)
	base := "point-cloud"
	if err == nil && loc != nil {
		parts := strings.Split(strings.Trim(loc.FullName(), "/"), "/")
		if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) != "" {
			base = parts[len(parts)-1]
		}
	}
	for _, ext := range []string{".copc.laz", ".las", ".laz", ".e57"} {
		if strings.HasSuffix(strings.ToLower(base), ext) {
			base = base[:len(base)-len(ext)]
			break
		}
	}
	if base == "" {
		base = "point-cloud"
	}
	return base + ".copc.laz"
}

func safeCOPCFileName(name string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(name), "/"), "/")
	base := "point-cloud.copc.laz"
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) != "" {
		base = parts[len(parts)-1]
	}
	if !strings.HasSuffix(strings.ToLower(base), ".copc.laz") {
		base += ".copc.laz"
	}
	return base
}

func pointCloudCOPCContentURL(id uint) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf("/api/v1/manager/point_cloud_copc/%d/content", id)
}

func applyPointCloudCOPCResultFields(fields map[string]interface{}, result *PointCloudCOPCExecutionResult) {
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

func pointCloudCOPCProgressPercent(event PointCloudCOPCProgressEvent, current int) int {
	next := current
	if event.OverallProgress != nil {
		next = *event.OverallProgress
	} else {
		switch event.Phase {
		case "prepare":
			next = 1
		case "convert":
			next = 10
		case "publish":
			next = 90
		default:
			next = current
		}
	}
	next = clampPointCloudCOPCProgress(next)
	if next < current {
		return clampPointCloudCOPCProgress(current)
	}
	return next
}

func pointCloudCOPCProgressStep(event PointCloudCOPCProgressEvent) string {
	if event.Message != "" {
		return event.Message
	}
	return fmt.Sprintf("生成点云 COPC：%s", event.Phase)
}

func pointCloudCOPCProgressMetadata(existing commonModels.JSONMap, event PointCloudCOPCProgressEvent) commonModels.JSONMap {
	metadata := commonModels.JSONMap{}
	if existing != nil {
		metadata = existing.Clone()
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
	}
	progress := commonModels.JSONMap{
		"phase":   event.Phase,
		"event":   event.Event,
		"message": event.Message,
	}
	if event.OverallProgress != nil {
		progress["overall_progress"] = clampPointCloudCOPCProgress(*event.OverallProgress)
	}
	if event.Metadata != nil {
		progress["metadata"] = event.Metadata.Clone()
	}
	metadata["progress_event"] = progress
	return metadata
}

func clampPointCloudCOPCProgress(value int) int {
	if value < 0 {
		return 0
	}
	if value > 99 {
		return 99
	}
	return value
}
