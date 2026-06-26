package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

type RasterCOGExecutor interface {
	BuildRasterCOG(ctx context.Context, req RasterCOGExecutionRequest) (*RasterCOGExecutionResult, error)
}

type RasterCOGCleaner interface {
	DeleteByStorageRef(ctx context.Context, storageRef string) error
}

type RasterCOGExecutionRequest struct {
	Task   *models.RasterCOGTask
	Config RasterCOGExecutionConfig
}

type RasterCOGExecutionResult struct {
	StorageRef string
	FileName   string
	SizeBytes  int64
	Width      int64
	Height     int64
	BandCount  int64
	SourceSRID int
	SourceCRS  string
	Extent     []float64
	ExtentSRID int
	Metadata   commonModels.JSONMap
}

type RasterCOGExecutionConfig struct {
	Target RasterCOGTargetConfig
	Raster RasterCOGRasterConfig
	COG    RasterCOGOptionsConfig
	Result RasterCOGTargetResultConfig
}

type RasterCOGTargetConfig struct {
	SourceEngineID  uint
	ItemID          uint
	ItemFingerprint string
	Locator         string
	FullName        string
}

type RasterCOGRasterConfig struct {
	SourceProfile   string
	SourceSizeBytes int64
	Width           int64
	Height          int64
	BandCount       int64
	SourceSRID      int
	SourceCRS       string
	Extent          []float64
	ExtentSRID      int
}

type RasterCOGOptionsConfig struct {
	Compression        string
	BlockSize          int
	OverviewResampling string
}

type RasterCOGTargetResultConfig struct {
	TargetKind string
	StorageRef string
	FileName   string
}

type RasterCOGTaskService struct {
	repo         *repository.RasterCOGRepository
	taskExecRepo *commonExecution.TaskExecutionRepository
	executor     RasterCOGExecutor
	cleaner      RasterCOGCleaner
	bucket       string
}

func NewRasterCOGTaskService(repo *repository.RasterCOGRepository, taskExecRepo *commonExecution.TaskExecutionRepository) *RasterCOGTaskService {
	return &RasterCOGTaskService{
		repo:         repo,
		taskExecRepo: taskExecRepo,
		bucket:       "manager",
	}
}

func (s *RasterCOGTaskService) SetExecutor(executor RasterCOGExecutor) {
	s.executor = executor
}

func (s *RasterCOGTaskService) SetCleaner(cleaner RasterCOGCleaner) {
	s.cleaner = cleaner
}

func (s *RasterCOGTaskService) SetBucket(bucket string) {
	if strings.TrimSpace(bucket) != "" {
		s.bucket = strings.TrimSpace(bucket)
	}
}

func (s *RasterCOGTaskService) Create(ctx context.Context, task *models.RasterCOGTask) error {
	if err := normalizeRasterCOGTask(task, s.bucket); err != nil {
		return err
	}
	return s.repo.CreateTask(ctx, task)
}

func (s *RasterCOGTaskService) GetByID(ctx context.Context, id uint, tenantID uint) (*models.RasterCOGTask, error) {
	return s.repo.GetTask(ctx, id, tenantID)
}

func (s *RasterCOGTaskService) List(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.RasterCOGTask, int64, error) {
	return s.repo.ListTasks(ctx, tenantID, page, pageSize)
}

func (s *RasterCOGTaskService) Update(ctx context.Context, task *models.RasterCOGTask) error {
	if err := normalizeRasterCOGTask(task, s.bucket); err != nil {
		return err
	}
	return s.repo.UpdateTask(ctx, task)
}

func (s *RasterCOGTaskService) Delete(ctx context.Context, id uint, tenantID uint) error {
	return s.repo.DeleteTask(ctx, id, tenantID)
}

func (s *RasterCOGTaskService) ListResults(ctx context.Context, filter repository.RasterCOGFilter) ([]*models.RasterCOG, int64, error) {
	return s.repo.List(ctx, filter)
}

func (s *RasterCOGTaskService) GetResult(ctx context.Context, id uint, tenantID uint) (*models.RasterCOG, error) {
	return s.repo.GetByID(ctx, id, tenantID)
}

func (s *RasterCOGTaskService) DeleteResult(ctx context.Context, id uint, tenantID uint) error {
	result, err := s.repo.GetByID(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("raster COG result not found")
	}
	if strings.TrimSpace(result.StorageRef) != "" {
		if s.cleaner == nil {
			return errors.New("raster COG result cleaner is required")
		}
		if err := s.cleaner.DeleteByStorageRef(ctx, result.StorageRef); err != nil {
			return err
		}
	}
	return s.repo.Delete(ctx, id, tenantID)
}

func (s *RasterCOGTaskService) Execute(ctx context.Context, taskID uint, tenantID uint, triggerType string, source string, parentExecutionID *string) (string, error) {
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
	currentStep := "生成栅格快显 COG"
	executionConfig := task.Config.Clone()
	if executionConfig == nil {
		executionConfig = commonModels.JSONMap{}
	}
	exec := &commonExecution.TaskExecution{
		ExecutionID:       executionID,
		TenantID:          int(tenantID),
		Module:            commonExecution.ModuleManager,
		TaskType:          commonExecution.TaskTypeRasterCOGGeneration,
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

	go s.runRasterCOGGeneration(context.Background(), task, executionID, now)
	return executionID, nil
}

func (s *RasterCOGTaskService) runRasterCOGGeneration(ctx context.Context, task *models.RasterCOGTask, executionID string, startedAt time.Time) {
	status := commonExecution.ExecutionStatusSuccess
	metadata := commonModels.JSONMap{}
	var errDetails commonModels.JSONMap

	rasterCOG, execCfg, err := s.prepareResult(ctx, task, executionID)
	var buildResult *RasterCOGExecutionResult
	if err == nil {
		if s.executor == nil {
			err = errors.New("raster COG generation executor is not configured")
		} else {
			buildResult, err = s.executor.BuildRasterCOG(ctx, RasterCOGExecutionRequest{Task: task, Config: execCfg})
		}
	}
	if err == nil && buildResult != nil {
		metadata = buildResult.Metadata.Clone()
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
	}

	completedAt := time.Now()
	if err != nil {
		status = commonExecution.ExecutionStatusFailed
		errDetails = commonModels.JSONMap{"message": err.Error()}
		if rasterCOG != nil {
			_ = s.repo.UpdateFields(ctx, rasterCOG.ID, task.TenantID, map[string]interface{}{
				"status":            models.RasterCOGStatusFailed,
				"error_message":     err.Error(),
				"last_execution_id": executionID,
			})
		}
	} else if rasterCOG != nil {
		fields := map[string]interface{}{
			"status":            models.RasterCOGStatusReady,
			"error_message":     "",
			"last_execution_id": executionID,
			"metadata":          metadata,
		}
		applyRasterCOGResultFields(fields, buildResult)
		if err := s.repo.UpdateFields(ctx, rasterCOG.ID, task.TenantID, fields); err != nil {
			status = commonExecution.ExecutionStatusFailed
			errDetails = commonModels.JSONMap{"message": fmt.Sprintf("update raster COG result: %v", err)}
			_ = s.repo.UpdateFields(ctx, rasterCOG.ID, task.TenantID, map[string]interface{}{
				"status":            models.RasterCOGStatusFailed,
				"error_message":     fmt.Sprintf("update raster COG result: %v", err),
				"last_execution_id": executionID,
			})
		}
	}

	progress := 100
	if status != commonExecution.ExecutionStatusSuccess {
		progress = 0
	}
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
		// execution 更新失败只能记录到任务最近状态；不能在后台 goroutine 中向调用方返回。
		_ = s.repo.UpdateTaskLastExecution(ctx, task.ID, task.TenantID, executionID, commonExecution.ExecutionStatusFailed, completedAt)
		return
	}
	_ = s.repo.UpdateTaskLastExecution(ctx, task.ID, task.TenantID, executionID, status, completedAt)
}

func (s *RasterCOGTaskService) prepareResult(ctx context.Context, task *models.RasterCOGTask, executionID string) (*models.RasterCOG, RasterCOGExecutionConfig, error) {
	execCfg, err := readRasterCOGExecutionConfig(task.Config, s.bucket, task.TenantID)
	if err != nil {
		return nil, execCfg, err
	}
	itemID := execCfg.Target.ItemID
	var itemIDPtr *uint
	if itemID > 0 {
		itemIDPtr = &itemID
	}
	extentJSON, extentSRID := marshalCOGExtent(execCfg.Raster.Extent, execCfg.Raster.ExtentSRID)
	metadata := commonModels.JSONMap{
		"cog": commonModels.JSONMap{
			"compression":         execCfg.COG.Compression,
			"blocksize":           execCfg.COG.BlockSize,
			"overview_resampling": execCfg.COG.OverviewResampling,
		},
		"target": commonModels.JSONMap{
			"storage_ref": execCfg.Result.StorageRef,
			"target_kind": execCfg.Result.TargetKind,
		},
	}

	existing, err := s.repo.GetCurrentByFingerprint(ctx, task.TenantID, execCfg.Target.ItemFingerprint)
	if err != nil {
		return nil, execCfg, err
	}
	if existing != nil {
		fields := map[string]interface{}{
			"item_id":           itemIDPtr,
			"locator":           execCfg.Target.Locator,
			"task_id":           &task.ID,
			"last_execution_id": executionID,
			"source_engine_id":  execCfg.Target.SourceEngineID,
			"source_profile":    execCfg.Raster.SourceProfile,
			"source_size_bytes": execCfg.Raster.SourceSizeBytes,
			"target_kind":       execCfg.Result.TargetKind,
			"storage_ref":       execCfg.Result.StorageRef,
			"file_name":         execCfg.Result.FileName,
			"width":             execCfg.Raster.Width,
			"height":            execCfg.Raster.Height,
			"band_count":        execCfg.Raster.BandCount,
			"source_srid":       execCfg.Raster.SourceSRID,
			"source_crs":        execCfg.Raster.SourceCRS,
			"extent":            extentJSON,
			"extent_srid":       extentSRID,
			"status":            models.RasterCOGStatusBuilding,
			"metadata":          metadata,
			"error_message":     "",
		}
		if err := s.repo.UpdateFields(ctx, existing.ID, task.TenantID, fields); err != nil {
			return nil, execCfg, err
		}
		refreshed, err := s.repo.GetByID(ctx, existing.ID, task.TenantID)
		if err != nil {
			return nil, execCfg, err
		}
		if refreshed == nil {
			return nil, execCfg, errors.New("raster COG result disappeared after update")
		}
		return refreshed, execCfg, nil
	}

	result := &models.RasterCOG{
		TenantID:        task.TenantID,
		ItemFingerprint: execCfg.Target.ItemFingerprint,
		ItemID:          itemIDPtr,
		Locator:         execCfg.Target.Locator,
		TaskID:          &task.ID,
		LastExecutionID: &executionID,
		SourceEngineID:  execCfg.Target.SourceEngineID,
		SourceProfile:   execCfg.Raster.SourceProfile,
		SourceSizeBytes: execCfg.Raster.SourceSizeBytes,
		TargetKind:      execCfg.Result.TargetKind,
		StorageRef:      execCfg.Result.StorageRef,
		FileName:        execCfg.Result.FileName,
		Width:           execCfg.Raster.Width,
		Height:          execCfg.Raster.Height,
		BandCount:       execCfg.Raster.BandCount,
		SourceSRID:      execCfg.Raster.SourceSRID,
		SourceCRS:       execCfg.Raster.SourceCRS,
		Extent:          extentJSON,
		ExtentSRID:      extentSRID,
		Status:          models.RasterCOGStatusBuilding,
		Metadata:        metadata,
		CreatedBy:       task.CreatedBy,
	}
	if err := s.repo.Create(ctx, result); err != nil {
		return nil, execCfg, err
	}
	return result, execCfg, nil
}

func normalizeRasterCOGTask(task *models.RasterCOGTask, bucket string) error {
	if task == nil {
		return errors.New("raster COG generation task is nil")
	}
	task.Name = strings.TrimSpace(task.Name)
	task.Description = strings.TrimSpace(task.Description)
	task.Schedule = strings.TrimSpace(task.Schedule)
	if task.Config == nil {
		task.Config = commonModels.JSONMap{}
	}
	if task.Name == "" {
		return errors.New("raster COG generation task name is required")
	}
	if len(task.Config) == 0 {
		return errors.New("raster COG generation task config is required")
	}
	if task.Schedule != "" || task.NextRunAt != nil {
		return errors.New("raster COG generation task does not support schedule")
	}
	_, err := normalizeRasterCOGTaskConfig(task.Config, bucket, task.TenantID)
	return err
}

func readRasterCOGExecutionConfig(config commonModels.JSONMap, bucket string, tenantID uint) (RasterCOGExecutionConfig, error) {
	return normalizeRasterCOGTaskConfig(config, bucket, tenantID)
}

func normalizeRasterCOGTaskConfig(config commonModels.JSONMap, bucket string, tenantID uint) (RasterCOGExecutionConfig, error) {
	if _, ok := config["artifact"]; ok {
		return RasterCOGExecutionConfig{}, errors.New("raster COG config.artifact is not supported; use config.result")
	}
	target, err := normalizeRasterCOGTarget(config)
	if err != nil {
		return RasterCOGExecutionConfig{}, err
	}
	raster := normalizeRasterCOGRaster(config)
	cog := normalizeRasterCOGOptions(config)
	result := normalizeRasterCOGTargetResult(config, target, bucket, tenantID)
	return RasterCOGExecutionConfig{Target: target, Raster: raster, COG: cog, Result: result}, nil
}

func normalizeRasterCOGTarget(config commonModels.JSONMap) (RasterCOGTargetConfig, error) {
	targetMap, ok := asJSONMap(config["target"])
	if !ok {
		return RasterCOGTargetConfig{}, errors.New("raster COG config.target is required")
	}
	target := RasterCOGTargetConfig{
		SourceEngineID:  uintFromConfig(targetMap["source_engine_id"]),
		ItemID:          uintFromConfig(targetMap["item_id"]),
		ItemFingerprint: stringFromConfig(targetMap["item_fingerprint"]),
		Locator:         stringFromConfig(targetMap["locator"]),
	}
	if target.SourceEngineID == 0 || target.Locator == "" {
		return RasterCOGTargetConfig{}, errors.New("raster COG config.target requires source_engine_id and locator")
	}
	loc, err := resourcetree.ParseURI(target.Locator)
	if err != nil {
		return RasterCOGTargetConfig{}, fmt.Errorf("raster COG config.target.locator is invalid: %w", err)
	}
	if loc.EngineID != target.SourceEngineID {
		return RasterCOGTargetConfig{}, errors.New("raster COG config.target.locator engine_id does not match source_engine_id")
	}
	if loc.ItemID != nil && target.ItemID == 0 {
		target.ItemID = *loc.ItemID
	}
	target.FullName = loc.FullName()
	if target.FullName == "" {
		return RasterCOGTargetConfig{}, errors.New("raster COG config.target.locator must point to an item path")
	}
	expectedFingerprint := commonModels.GenerateItemFingerprint(target.SourceEngineID, target.FullName)
	if target.ItemFingerprint != "" && target.ItemFingerprint != expectedFingerprint {
		return RasterCOGTargetConfig{}, errors.New("raster COG config.target.item_fingerprint does not match source locator")
	}
	target.ItemFingerprint = expectedFingerprint
	normalized := commonModels.JSONMap{
		"source_engine_id": target.SourceEngineID,
		"item_fingerprint": target.ItemFingerprint,
		"locator":          target.Locator,
	}
	if target.ItemID > 0 {
		normalized["item_id"] = target.ItemID
	}
	config["target"] = normalized
	return target, nil
}

func normalizeRasterCOGRaster(config commonModels.JSONMap) RasterCOGRasterConfig {
	rasterMap, _ := asJSONMap(config["raster"])
	raster := RasterCOGRasterConfig{
		SourceProfile:   firstNonEmptyConfig(stringFromConfig(rasterMap["source_profile"]), "unknown"),
		SourceSizeBytes: int64FromConfig(rasterMap["source_size_bytes"], 0),
		Width:           int64FromConfig(rasterMap["width"], 0),
		Height:          int64FromConfig(rasterMap["height"], 0),
		BandCount:       int64FromConfig(rasterMap["band_count"], 0),
		SourceSRID:      intFromConfig(rasterMap["source_srid"]),
		SourceCRS:       stringFromConfig(rasterMap["source_crs"]),
		ExtentSRID:      intFromConfig(rasterMap["extent_srid"]),
	}
	if extent, ok := floatSliceFromConfig(rasterMap["extent"]); ok {
		raster.Extent = extent
	}
	normalized := commonModels.JSONMap{
		"source_profile":    raster.SourceProfile,
		"source_size_bytes": raster.SourceSizeBytes,
		"width":             raster.Width,
		"height":            raster.Height,
		"band_count":        raster.BandCount,
	}
	if raster.SourceSRID > 0 {
		normalized["source_srid"] = raster.SourceSRID
	}
	if raster.SourceCRS != "" {
		normalized["source_crs"] = raster.SourceCRS
	}
	if len(raster.Extent) == 4 {
		normalized["extent"] = raster.Extent
	}
	if raster.ExtentSRID > 0 {
		normalized["extent_srid"] = raster.ExtentSRID
	}
	config["raster"] = normalized
	return raster
}

func normalizeRasterCOGOptions(config commonModels.JSONMap) RasterCOGOptionsConfig {
	cogMap, _ := asJSONMap(config["cog"])
	opts := RasterCOGOptionsConfig{
		Compression:        firstNonEmptyConfig(stringFromConfig(cogMap["compression"]), "DEFLATE"),
		BlockSize:          intFromConfig(cogMap["blocksize"]),
		OverviewResampling: firstNonEmptyConfig(stringFromConfig(cogMap["overview_resampling"]), "NEAREST"),
	}
	if opts.BlockSize <= 0 {
		opts.BlockSize = 512
	}
	config["cog"] = commonModels.JSONMap{
		"compression":         opts.Compression,
		"blocksize":           opts.BlockSize,
		"overview_resampling": opts.OverviewResampling,
	}
	return opts
}

func normalizeRasterCOGTargetResult(config commonModels.JSONMap, target RasterCOGTargetConfig, bucket string, tenantID uint) RasterCOGTargetResultConfig {
	resultMap, _ := asJSONMap(config["result"])
	fileName := stringFromConfig(resultMap["file_name"])
	if fileName == "" {
		fileName = defaultRasterCOGFileName(target.FullName)
	}
	storageRef := stringFromConfig(resultMap["storage_ref"])
	if storageRef == "" {
		objectName := fmt.Sprintf("tenant_%d/cog/%s/%s", tenantID, target.ItemFingerprint, fileName)
		storageRef = rastercogref.ObjectStorageRef(bucket, objectName)
	}
	result := RasterCOGTargetResultConfig{
		TargetKind: firstNonEmptyConfig(stringFromConfig(resultMap["target_kind"]), models.RasterCOGTargetKindMinIO),
		StorageRef: storageRef,
		FileName:   fileName,
	}
	config["result"] = commonModels.JSONMap{
		"target_kind": result.TargetKind,
		"storage_ref": result.StorageRef,
		"file_name":   result.FileName,
	}
	return result
}

func defaultRasterCOGFileName(fullName string) string {
	base := strings.TrimSpace(path.Base(strings.Trim(fullName, "/")))
	if base == "." || base == "" {
		base = "raster"
	}
	lower := strings.ToLower(base)
	for _, suffix := range []string{".tiff", ".tif"} {
		if strings.HasSuffix(lower, suffix) {
			return strings.TrimSuffix(base, base[len(base)-len(suffix):]) + ".cog.tif"
		}
	}
	return base + ".cog.tif"
}

func firstNonEmptyConfig(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func marshalCOGExtent(extent []float64, extentSRID int) ([]byte, *int) {
	if len(extent) != 4 {
		return nil, nil
	}
	data, _ := json.Marshal(extent)
	var srid *int
	if extentSRID > 0 {
		srid = &extentSRID
	}
	return data, srid
}

func applyRasterCOGResultFields(fields map[string]interface{}, result *RasterCOGExecutionResult) {
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
	if result.Width > 0 {
		fields["width"] = result.Width
	}
	if result.Height > 0 {
		fields["height"] = result.Height
	}
	if result.BandCount > 0 {
		fields["band_count"] = result.BandCount
	}
	if result.SourceSRID > 0 {
		fields["source_srid"] = result.SourceSRID
	}
	if strings.TrimSpace(result.SourceCRS) != "" {
		fields["source_crs"] = strings.TrimSpace(result.SourceCRS)
	}
	if len(result.Extent) == 4 {
		extentJSON, extentSRID := marshalCOGExtent(result.Extent, result.ExtentSRID)
		fields["extent"] = extentJSON
		fields["extent_srid"] = extentSRID
	}
}
