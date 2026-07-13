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

type CADPreviewExecutor interface {
	BuildCADPreview(context.Context, CADPreviewExecutionRequest) (*CADPreviewExecutionResult, error)
}

type CADPreviewCleaner interface {
	DeleteByStorageRef(context.Context, string) error
}

type CADPreviewExecutionRequest struct {
	Task        *models.CADPreviewTask
	ExecutionID string
	Config      CADPreviewExecutionConfig
}

type CADPreviewExecutionResult struct {
	StorageRef   string
	ManifestRef  string
	ThumbnailRef string
	TileCount    int64
	TileSize     int
	MinZoom      int
	MaxZoom      int
	Bounds       commonModels.JSONMap
	Metadata     commonModels.JSONMap
}

type CADPreviewExecutionConfig struct {
	Source  CADPreviewSourceConfig
	Result  CADPreviewResultConfig
	Options CADPreviewOptions
}

type CADPreviewSourceConfig struct {
	ItemLocator     string
	SourceEngineID  uint
	ItemFingerprint string
	ItemID          uint
	Format          string
	SourceSizeBytes int64
}

type CADPreviewResultConfig struct{ StorageRef string }
type CADPreviewOptions struct{ TileSize, MaxZoom int }

const maxCADPreviewTileCount int64 = 25000

type CADPreviewTaskService struct {
	repo         *repository.CADPreviewRepository
	taskExecRepo *commonExecution.TaskExecutionRepository
	executor     CADPreviewExecutor
	cleaner      CADPreviewCleaner
	bucket       string
}

func NewCADPreviewTaskService(repo *repository.CADPreviewRepository, taskExecRepo *commonExecution.TaskExecutionRepository) *CADPreviewTaskService {
	return &CADPreviewTaskService{repo: repo, taskExecRepo: taskExecRepo}
}

func (s *CADPreviewTaskService) SetExecutor(v CADPreviewExecutor) { s.executor = v }
func (s *CADPreviewTaskService) SetCleaner(v CADPreviewCleaner)   { s.cleaner = v }
func (s *CADPreviewTaskService) SetBucket(v string)               { s.bucket = strings.TrimSpace(v) }

func (s *CADPreviewTaskService) Create(ctx context.Context, task *models.CADPreviewTask) error {
	if err := normalizeCADPreviewTask(task, s.bucket); err != nil {
		return err
	}
	cfg, _ := normalizeCADPreviewTaskConfig(task.Config, s.bucket, task.TenantID)
	existing, err := s.repo.GetTaskByFingerprint(ctx, task.TenantID, cfg.Source.ItemFingerprint)
	if err != nil {
		return err
	}
	if existing != nil {
		existing.Name, existing.Description, existing.Enabled = task.Name, task.Description, task.Enabled
		existing.Config = task.Config.Clone()
		if err := s.repo.UpdateTask(ctx, existing); err != nil {
			return err
		}
		*task = *existing
		return nil
	}
	return s.repo.CreateTask(ctx, task)
}

func (s *CADPreviewTaskService) GetByID(ctx context.Context, id, tenantID uint) (*models.CADPreviewTask, error) {
	return s.repo.GetTask(ctx, id, tenantID)
}
func (s *CADPreviewTaskService) List(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.CADPreviewTask, int64, error) {
	return s.repo.ListTasks(ctx, tenantID, page, pageSize)
}
func (s *CADPreviewTaskService) Update(ctx context.Context, task *models.CADPreviewTask) error {
	if err := normalizeCADPreviewTask(task, s.bucket); err != nil {
		return err
	}
	return s.repo.UpdateTask(ctx, task)
}
func (s *CADPreviewTaskService) Delete(ctx context.Context, id, tenantID uint) error {
	return s.repo.DeleteTask(ctx, id, tenantID)
}
func (s *CADPreviewTaskService) ListResults(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.CADPreview, int64, error) {
	return s.repo.List(ctx, tenantID, page, pageSize)
}
func (s *CADPreviewTaskService) GetResult(ctx context.Context, id, tenantID uint) (*models.CADPreview, error) {
	return s.repo.GetByID(ctx, id, tenantID)
}
func (s *CADPreviewTaskService) GetReadyByFingerprint(ctx context.Context, tenantID uint, fingerprint string) (*models.CADPreview, error) {
	return s.repo.GetLatestReadyByFingerprint(ctx, tenantID, fingerprint)
}

func (s *CADPreviewTaskService) DeleteResult(ctx context.Context, id, tenantID uint) error {
	result, err := s.repo.GetByID(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("CAD preview result not found")
	}
	if s.cleaner != nil && strings.TrimSpace(result.StorageRef) != "" {
		if err := s.cleaner.DeleteByStorageRef(ctx, result.StorageRef); err != nil {
			return err
		}
	}
	return s.repo.Delete(ctx, id, tenantID)
}

func (s *CADPreviewTaskService) Execute(ctx context.Context, taskID, tenantID uint, triggerType, source string, parentExecutionID *string) (string, error) {
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
	triggerType, err = commonExecution.NormalizeTriggerType(triggerType)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(source) == "" {
		source = commonExecution.ModuleManager
	}
	executionID, now, step := uuid.New().String(), time.Now(), "生成 CAD 栅格瓦片预览"
	exec := &commonExecution.TaskExecution{
		ExecutionID: executionID, TenantID: int(tenantID), Module: commonExecution.ModuleManager,
		TaskType: commonExecution.TaskTypeCADPreviewGeneration, Source: source,
		SourceTaskID: commonExecution.NewSourceTaskIDFromUint(taskID), SourceTaskName: &task.Name,
		ParentExecutionID: parentExecutionID, Status: commonExecution.ExecutionStatusRunning,
		Progress: 0, CurrentStep: &step, TriggerType: triggerType, ExecutionConfig: task.Config.Clone(), StartedAt: &now,
	}
	if err := s.taskExecRepo.Create(ctx, exec); err != nil {
		return "", err
	}
	if err := s.repo.UpdateTaskLastExecution(ctx, taskID, tenantID, executionID, commonExecution.ExecutionStatusRunning, now); err != nil {
		return "", err
	}
	go s.run(context.Background(), task, executionID, now)
	return executionID, nil
}

func (s *CADPreviewTaskService) run(ctx context.Context, task *models.CADPreviewTask, executionID string, startedAt time.Time) {
	status, progress := commonExecution.ExecutionStatusSuccess, 100
	metadata := commonModels.JSONMap{}
	var errorDetails commonModels.JSONMap
	result, cfg, err := s.prepareResult(ctx, task, executionID)
	var built *CADPreviewExecutionResult
	if err == nil {
		if s.executor == nil {
			err = errors.New("CAD preview generation executor is not configured")
		} else {
			built, err = s.executor.BuildCADPreview(ctx, CADPreviewExecutionRequest{Task: task, ExecutionID: executionID, Config: cfg})
		}
	}
	if err != nil {
		status, progress = commonExecution.ExecutionStatusFailed, 0
		errorDetails, metadata = commonModels.JSONMap{"message": err.Error()}, commonModels.JSONMap{"error": err.Error()}
		if result != nil {
			_ = s.repo.UpdateFields(ctx, result.ID, task.TenantID, map[string]interface{}{"status": models.CADPreviewStatusFailed, "error_message": err.Error()})
		}
	} else if result != nil {
		fields := map[string]interface{}{
			"status": models.CADPreviewStatusReady, "error_message": "", "last_execution_id": executionID,
			"storage_ref": built.StorageRef, "manifest_ref": built.ManifestRef, "thumbnail_ref": built.ThumbnailRef,
			"tile_count": built.TileCount, "tile_size": built.TileSize, "min_zoom": built.MinZoom, "max_zoom": built.MaxZoom,
			"bounds": built.Bounds, "metadata": built.Metadata,
		}
		if err = s.repo.UpdateFields(ctx, result.ID, task.TenantID, fields); err != nil {
			status, progress = commonExecution.ExecutionStatusFailed, 0
			errorDetails = commonModels.JSONMap{"message": err.Error()}
		} else {
			metadata = built.Metadata.Clone()
			if metadata == nil {
				metadata = commonModels.JSONMap{}
			}
			metadata["result_id"], metadata["manifest_url"] = result.ID, cadPreviewManifestURL(result.ID)
		}
	}
	completedAt := time.Now()
	_ = s.taskExecRepo.UpdateFields(ctx, executionID, int(task.TenantID), map[string]interface{}{
		"status": status, "progress": progress, "metadata": metadata, "error_details": errorDetails,
		"completed_at": completedAt, "execution_time_ms": completedAt.Sub(startedAt).Milliseconds(), "updated_at": completedAt,
	})
	_ = s.repo.UpdateTaskLastExecution(ctx, task.ID, task.TenantID, executionID, status, completedAt)
}

func (s *CADPreviewTaskService) prepareResult(ctx context.Context, task *models.CADPreviewTask, executionID string) (*models.CADPreview, CADPreviewExecutionConfig, error) {
	cfg, err := normalizeCADPreviewTaskConfig(task.Config, s.bucket, task.TenantID)
	if err != nil {
		return nil, cfg, err
	}
	existing, err := s.repo.GetCurrentByFingerprint(ctx, task.TenantID, cfg.Source.ItemFingerprint)
	if err != nil {
		return nil, cfg, err
	}
	itemID := optionalUint(cfg.Source.ItemID)
	if existing != nil {
		if err := s.repo.UpdateFields(ctx, existing.ID, task.TenantID, map[string]interface{}{
			"item_id": itemID, "locator": cfg.Source.ItemLocator, "task_id": task.ID, "last_execution_id": executionID,
			"source_engine_id": cfg.Source.SourceEngineID, "source_format": cfg.Source.Format, "source_size_bytes": cfg.Source.SourceSizeBytes,
			"storage_ref": cfg.Result.StorageRef, "status": models.CADPreviewStatusBuilding, "metadata": commonModels.JSONMap{}, "error_message": "", "updated_at": time.Now(),
		}); err != nil {
			return nil, cfg, err
		}
		return existing, cfg, nil
	}
	result := &models.CADPreview{
		TenantID: task.TenantID, ItemFingerprint: cfg.Source.ItemFingerprint, ItemID: itemID, Locator: cfg.Source.ItemLocator,
		TaskID: &task.ID, LastExecutionID: &executionID, SourceEngineID: cfg.Source.SourceEngineID, SourceFormat: cfg.Source.Format,
		SourceSizeBytes: cfg.Source.SourceSizeBytes, StorageRef: cfg.Result.StorageRef, ManifestRef: "manifest.json",
		Status: models.CADPreviewStatusBuilding, Bounds: commonModels.JSONMap{}, Metadata: commonModels.JSONMap{}, CreatedBy: task.CreatedBy,
	}
	if err := s.repo.Create(ctx, result); err != nil {
		return nil, cfg, err
	}
	return result, cfg, nil
}

func normalizeCADPreviewTask(task *models.CADPreviewTask, bucket string) error {
	if task == nil {
		return errors.New("CAD preview generation task is nil")
	}
	task.Name, task.Description, task.Schedule = strings.TrimSpace(task.Name), strings.TrimSpace(task.Description), strings.TrimSpace(task.Schedule)
	if task.Name == "" {
		return errors.New("CAD preview generation task name is required")
	}
	if task.Config == nil || len(task.Config) == 0 {
		return errors.New("CAD preview generation task config is required")
	}
	if task.Schedule != "" || task.NextRunAt != nil {
		return errors.New("CAD preview generation task does not support schedule")
	}
	_, err := normalizeCADPreviewTaskConfig(task.Config, bucket, task.TenantID)
	return err
}

func normalizeCADPreviewTaskConfig(config commonModels.JSONMap, bucket string, tenantID uint) (CADPreviewExecutionConfig, error) {
	sourceMap, ok := asJSONMap(config["source"])
	if !ok {
		return CADPreviewExecutionConfig{}, errors.New("CAD preview config.source is required")
	}
	source := CADPreviewSourceConfig{
		ItemLocator: stringFromConfig(sourceMap["item_locator"]), SourceEngineID: uintFromConfig(sourceMap["source_engine_id"]),
		ItemFingerprint: strings.TrimSpace(stringFromConfig(sourceMap["item_fingerprint"])), ItemID: uintFromConfig(sourceMap["item_id"]),
		Format: strings.ToLower(strings.TrimSpace(stringFromConfig(sourceMap["format"]))), SourceSizeBytes: int64FromConfig(sourceMap["source_size_bytes"], 0),
	}
	if source.ItemLocator == "" || source.SourceEngineID == 0 || source.ItemFingerprint == "" {
		return CADPreviewExecutionConfig{}, errors.New("CAD preview config.source requires item_locator, source_engine_id and item_fingerprint")
	}
	if format.NormalizeFormat(source.Format) != format.FormatDWG {
		return CADPreviewExecutionConfig{}, errors.New("CAD preview config.source.format must be dwg")
	}
	if source.SourceSizeBytes < 0 {
		return CADPreviewExecutionConfig{}, errors.New("CAD preview config.source.source_size_bytes must not be negative")
	}
	loc, err := resourcetree.ParseURI(source.ItemLocator)
	if err != nil || loc.EngineID != source.SourceEngineID {
		return CADPreviewExecutionConfig{}, errors.New("CAD preview source locator is invalid or engine_id does not match")
	}
	source.Format = string(format.FormatDWG)
	config["source"] = commonModels.JSONMap{"item_locator": source.ItemLocator, "source_engine_id": source.SourceEngineID, "item_fingerprint": source.ItemFingerprint, "item_id": source.ItemID, "format": source.Format, "source_size_bytes": source.SourceSizeBytes}

	resultMap, _ := asJSONMap(config["result"])
	storageRef := stringFromConfig(resultMap["storage_ref"])
	if storageRef == "" {
		storageRef = rastercogref.ObjectStorageRef(firstNonEmptyConfig(bucket, "manager"), fmt.Sprintf("tenant_%d/cad-previews/%s", tenantID, source.ItemFingerprint))
	}
	result := CADPreviewResultConfig{StorageRef: storageRef}
	config["result"] = commonModels.JSONMap{"storage_ref": storageRef}
	optionsMap, _ := asJSONMap(config["options"])
	options := CADPreviewOptions{}
	if value, exists := optionsMap["tile_size"]; exists {
		options.TileSize = intFromConfig(value)
	} else {
		options.TileSize = 512
	}
	if options.TileSize < 128 || options.TileSize > 1024 {
		return CADPreviewExecutionConfig{}, errors.New("CAD preview options.tile_size must be between 128 and 1024")
	}
	if value, exists := optionsMap["max_zoom"]; exists {
		options.MaxZoom = intFromConfig(value)
	} else {
		options.MaxZoom = 4
	}
	if options.MaxZoom < 0 || options.MaxZoom > 8 {
		return CADPreviewExecutionConfig{}, errors.New("CAD preview options.max_zoom must be between 0 and 8")
	}
	if cadPreviewTileCount(options.MaxZoom) > maxCADPreviewTileCount {
		return CADPreviewExecutionConfig{}, fmt.Errorf("CAD preview options.max_zoom produces more than %d tiles", maxCADPreviewTileCount)
	}
	config["options"] = commonModels.JSONMap{"tile_size": options.TileSize, "max_zoom": options.MaxZoom}
	return CADPreviewExecutionConfig{Source: source, Result: result, Options: options}, nil
}

func cadPreviewTileCount(maxZoom int) int64 {
	var total int64
	for zoom := 0; zoom <= maxZoom; zoom++ {
		side := int64(1) << zoom
		total += side * side
	}
	return total
}

func cadPreviewManifestURL(id uint) string {
	return fmt.Sprintf("/api/v1/manager/cad-previews/%d/manifest", id)
}
