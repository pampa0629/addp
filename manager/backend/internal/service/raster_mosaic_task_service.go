package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

type RasterMosaicExecutor interface {
	BuildRasterMosaic(ctx context.Context, req RasterMosaicExecutionRequest) (*RasterMosaicExecutionResult, error)
}

type RasterMosaicMetaScanSubmitter interface {
	CreateManualScanRun(opts commonClient.MetaScanOptions) (*commonExecution.TaskExecution, error)
	SetTenantID(tenantID *uint)
}

type RasterMosaicExecutionRequest struct {
	Task        *models.RasterMosaicTask
	ExecutionID string
	Config      RasterMosaicExecutionConfig
}

type RasterMosaicExecutionResult struct {
	ManifestLocator string
	ManifestRef     string
	IndexRef        string
	OverviewRef     string
	LeafCount       int64
	Metadata        commonModels.JSONMap
}

type RasterMosaicProgressEvent struct {
	Phase           string
	Event           string
	Message         string
	TotalFiles      int64
	ProcessedFiles  int64
	FailedFiles     int64
	CurrentFile     string
	FileProgress    *int
	OverallProgress *int
	Metadata        commonModels.JSONMap
}

type RasterMosaicExecutionConfig struct {
	Source    RasterMosaicSourceConfig
	Placement RasterMosaicPlacementConfig
	Target    RasterMosaicTargetConfig
	COG       RasterMosaicCOGConfig
	Overview  RasterMosaicOverviewConfig
	Tiles     RasterMosaicTilesConfig
}

type RasterMosaicSourceConfig struct {
	NodeLocator     string
	SourceEngineID  uint
	Recursive       bool
	IncludePatterns []string
	ExcludePatterns []string
}

type RasterMosaicTargetConfig struct {
	StorageLocator string
	TargetEngineID uint
	DatasetName    string
}

type RasterMosaicPlacementConfig struct {
	Mode string
}

type RasterMosaicCOGConfig struct {
	Compression        string
	BlockSize          int
	OverviewResampling string
	ValidateSourceCOG  bool
	LeafConcurrency    int
	NumThreads         int
	LeafRetryAttempts  int
}

type RasterMosaicOverviewConfig struct {
	Enabled    bool
	MaxPixels  int64
	Resampling string
}

type RasterMosaicTilesConfig struct {
	Enabled bool
	MinZoom int
	MaxZoom int
	Format  string
}

type RasterMosaicTaskService struct {
	repo              *repository.RasterMosaicRepository
	taskExecRepo      *commonExecution.TaskExecutionRepository
	executor          RasterMosaicExecutor
	metaScanSubmitter RasterMosaicMetaScanSubmitter
}

var (
	ErrRasterMosaicProgressTargetMismatch = errors.New("raster mosaic progress event target mismatch")
	ErrRasterMosaicExecutionCompleted     = errors.New("raster mosaic execution is already completed")
	ErrRasterMosaicExecutionNotRunning    = errors.New("raster mosaic execution is not running")
)

func NewRasterMosaicTaskService(repo *repository.RasterMosaicRepository, taskExecRepo *commonExecution.TaskExecutionRepository) *RasterMosaicTaskService {
	return &RasterMosaicTaskService{repo: repo, taskExecRepo: taskExecRepo}
}

func (s *RasterMosaicTaskService) SetExecutor(executor RasterMosaicExecutor) {
	s.executor = executor
}

func (s *RasterMosaicTaskService) SetMetaScanSubmitter(submitter RasterMosaicMetaScanSubmitter) {
	s.metaScanSubmitter = submitter
}

func (s *RasterMosaicTaskService) Create(ctx context.Context, task *models.RasterMosaicTask) error {
	if err := normalizeRasterMosaicTask(task); err != nil {
		return err
	}
	return s.repo.CreateTask(ctx, task)
}

func (s *RasterMosaicTaskService) GetByID(ctx context.Context, id uint, tenantID uint) (*models.RasterMosaicTask, error) {
	return s.repo.GetTask(ctx, id, tenantID)
}

func (s *RasterMosaicTaskService) List(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.RasterMosaicTask, int64, error) {
	return s.repo.ListTasks(ctx, tenantID, page, pageSize)
}

func (s *RasterMosaicTaskService) Update(ctx context.Context, task *models.RasterMosaicTask) error {
	if err := normalizeRasterMosaicTask(task); err != nil {
		return err
	}
	return s.repo.UpdateTask(ctx, task)
}

func (s *RasterMosaicTaskService) Delete(ctx context.Context, id uint, tenantID uint) error {
	return s.repo.DeleteTask(ctx, id, tenantID)
}

func (s *RasterMosaicTaskService) Execute(ctx context.Context, taskID uint, tenantID uint, triggerType string, source string, parentExecutionID *string) (string, error) {
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
	currentStep := "生成栅格 mosaic"
	exec := &commonExecution.TaskExecution{
		ExecutionID:       executionID,
		TenantID:          int(tenantID),
		Module:            commonExecution.ModuleManager,
		TaskType:          commonExecution.TaskTypeRasterMosaicGeneration,
		Source:            normalizedSource,
		ParentExecutionID: parentExecutionID,
		Status:            commonExecution.ExecutionStatusPending,
		Progress:          0,
		CurrentStep:       &currentStep,
		TriggerType:       normalizedTriggerType,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	claimedTask, err := s.repo.ClaimExecution(ctx, taskID, tenantID, exec)
	if err != nil {
		if errors.Is(err, commonAPI.ErrNotFound) {
			return "", ErrTaskNotFound
		}
		if errors.Is(err, commonAPI.ErrConflict) {
			return "", ErrTaskExecutionBusy
		}
		return "", err
	}

	go s.runRasterMosaicGeneration(context.Background(), claimedTask, executionID)
	return executionID, nil
}

func (s *RasterMosaicTaskService) RecordProgressEvent(ctx context.Context, tenantID uint, executionID string, event RasterMosaicProgressEvent) error {
	if s.taskExecRepo == nil {
		return errors.New("task execution repository is required")
	}
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
	event.CurrentFile = strings.TrimSpace(event.CurrentFile)
	if event.Phase == "" {
		return errors.New("phase is required")
	}
	if event.Event == "" {
		return errors.New("event is required")
	}
	if event.TotalFiles < 0 || event.ProcessedFiles < 0 || event.FailedFiles < 0 {
		return errors.New("file counters must be greater than or equal to 0")
	}
	if event.TotalFiles > 0 && event.ProcessedFiles > event.TotalFiles {
		return errors.New("processed_files cannot be greater than total_files")
	}

	exec, err := s.taskExecRepo.GetByExecutionID(ctx, executionID, int(tenantID))
	if err != nil {
		return err
	}
	if exec.Module != commonExecution.ModuleManager || exec.TaskType != commonExecution.TaskTypeRasterMosaicGeneration {
		return ErrRasterMosaicProgressTargetMismatch
	}
	if exec.IsCompleted() {
		return ErrRasterMosaicExecutionCompleted
	}
	if exec.Status != commonExecution.ExecutionStatusRunning {
		return ErrRasterMosaicExecutionNotRunning
	}

	now := time.Now()
	nextProgress := rasterMosaicProgressPercent(event, exec.Progress)
	currentStep := rasterMosaicProgressStep(event)
	metadata := rasterMosaicProgressMetadata(exec.Metadata, event)
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
	return s.taskExecRepo.UpdateFields(ctx, executionID, int(tenantID), fields)
}

func (s *RasterMosaicTaskService) runRasterMosaicGeneration(ctx context.Context, task *models.RasterMosaicTask, executionID string) {
	startedAt := time.Now()
	if err := s.repo.StartExecution(ctx, task.ID, task.TenantID, executionID, startedAt); err != nil {
		logger.L().Warn("领取栅格 mosaic execution 失败", "execution_id", executionID, "task_id", task.ID, "error", err)
		return
	}
	s.executeRasterMosaicGeneration(ctx, task, executionID, startedAt)
}

func (s *RasterMosaicTaskService) executeRasterMosaicGeneration(
	ctx context.Context, task *models.RasterMosaicTask, executionID string, startedAt time.Time,
) {
	status := commonExecution.ExecutionStatusSuccess
	progress := 100
	metadata := commonModels.JSONMap{}
	var errDetails commonModels.JSONMap

	cfg, err := readRasterMosaicExecutionConfig(task.Config)
	var result *RasterMosaicExecutionResult
	if err == nil {
		if s.executor == nil {
			err = errors.New("raster mosaic generation executor is not configured")
		} else {
			result, err = s.executor.BuildRasterMosaic(ctx, RasterMosaicExecutionRequest{Task: task, ExecutionID: executionID, Config: cfg})
		}
	}
	if err == nil && result != nil {
		metadata = result.Metadata.Clone()
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
		metadata["manifest_locator"] = result.ManifestLocator
		metadata["manifest_ref"] = result.ManifestRef
		metadata["index_ref"] = result.IndexRef
		metadata["overview_ref"] = result.OverviewRef
		metadata["leaf_count"] = result.LeafCount
		var scanRun *commonExecution.TaskExecution
		scanRun, err = s.submitRasterMosaicMetaScan(task.TenantID, cfg)
		if err == nil && scanRun != nil {
			metadata["meta_scan"] = commonModels.JSONMap{
				"execution_id": scanRun.ExecutionID,
				"status":       scanRun.Status,
				"task_type":    scanRun.TaskType,
				"module":       scanRun.Module,
				"engine_id":    cfg.Target.TargetEngineID,
				"catalog_path": rasterMosaicDatasetCatalogPath(cfg),
			}
		}
	}
	if err != nil {
		if rasterMosaicExecutionTimedOut(err) {
			status = commonExecution.ExecutionStatusTimeout
		} else {
			status = commonExecution.ExecutionStatusFailed
		}
		progress = s.rasterMosaicExistingExecutionProgress(ctx, executionID, int(task.TenantID))
		errDetails = commonModels.JSONMap{"message": err.Error()}
	}
	metadata = s.mergeRasterMosaicExistingExecutionMetadata(ctx, executionID, int(task.TenantID), metadata)

	completedAt := time.Now()
	durationMs := completedAt.Sub(startedAt).Milliseconds()
	if err := s.repo.CompleteExecution(ctx, task.ID, task.TenantID, executionID, map[string]interface{}{
		"status":            status,
		"progress":          progress,
		"metadata":          metadata,
		"error_details":     errDetails,
		"completed_at":      completedAt,
		"execution_time_ms": durationMs,
		"updated_at":        completedAt,
	}, completedAt); err != nil {
		logger.L().Warn("提交栅格 mosaic execution 终态失败", "execution_id", executionID, "task_id", task.ID, "error", err)
	}
}

func rasterMosaicExecutionTimedOut(err error) bool {
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

func (s *RasterMosaicTaskService) rasterMosaicExistingExecutionProgress(ctx context.Context, executionID string, tenantID int) int {
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

func (s *RasterMosaicTaskService) submitRasterMosaicMetaScan(tenantID uint, cfg RasterMosaicExecutionConfig) (*commonExecution.TaskExecution, error) {
	if s == nil || s.metaScanSubmitter == nil {
		return nil, errors.New("raster mosaic meta scan submitter is not configured")
	}
	if cfg.Target.TargetEngineID == 0 {
		return nil, errors.New("raster mosaic target engine_id is required for meta scan")
	}
	catalogPath := rasterMosaicDatasetCatalogPath(cfg)
	if catalogPath == "" {
		return nil, errors.New("raster mosaic dataset catalog path is required for meta scan")
	}
	if tenantID > 0 {
		s.metaScanSubmitter.SetTenantID(&tenantID)
	}
	return s.metaScanSubmitter.CreateManualScanRun(commonClient.MetaScanOptions{
		EngineID:     cfg.Target.TargetEngineID,
		CatalogPaths: []string{catalogPath},
		ScanDepth:    commonClient.MetaScanDepthDeep,
		Force:        true,
		TriggerType:  commonExecution.TriggerTypeManual,
		Source:       commonExecution.ModuleManager,
	})
}

func rasterMosaicDatasetCatalogPath(cfg RasterMosaicExecutionConfig) string {
	loc, err := resourcetree.ParseURI(cfg.Target.StorageLocator)
	if err != nil || loc == nil {
		return ""
	}
	catalogPath := strings.Trim(loc.FullName(), "/")
	if cfg.Placement.Mode == "detached" {
		catalogPath = joinFilePath(catalogPath, strings.Trim(cfg.Target.DatasetName, "/"))
	}
	return strings.Trim(catalogPath, "/")
}

func (s *RasterMosaicTaskService) mergeRasterMosaicExistingExecutionMetadata(ctx context.Context, executionID string, tenantID int, metadata commonModels.JSONMap) commonModels.JSONMap {
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

func rasterMosaicProgressPercent(event RasterMosaicProgressEvent, current int) int {
	next := current
	if event.OverallProgress != nil {
		next = *event.OverallProgress
	} else if event.TotalFiles > 0 {
		next = int((event.ProcessedFiles*100 + event.TotalFiles/2) / event.TotalFiles)
	}
	next = clampPercent(next)
	if next < current {
		return clampPercent(current)
	}
	return next
}

func rasterMosaicProgressStep(event RasterMosaicProgressEvent) string {
	if event.Message != "" {
		return event.Message
	}
	if event.TotalFiles > 0 {
		return fmt.Sprintf("构建栅格 mosaic：%s %d/%d", event.Phase, event.ProcessedFiles, event.TotalFiles)
	}
	return fmt.Sprintf("构建栅格 mosaic：%s", event.Phase)
}

func rasterMosaicProgressMetadata(existing commonModels.JSONMap, event RasterMosaicProgressEvent) commonModels.JSONMap {
	metadata := commonModels.JSONMap{}
	if existing != nil {
		metadata = existing.Clone()
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
	}
	progress := commonModels.JSONMap{
		"phase":           event.Phase,
		"event":           event.Event,
		"message":         event.Message,
		"total_files":     event.TotalFiles,
		"processed_files": event.ProcessedFiles,
		"failed_files":    event.FailedFiles,
		"current_file":    event.CurrentFile,
	}
	if event.FileProgress != nil {
		progress["file_progress"] = clampPercent(*event.FileProgress)
	}
	if event.OverallProgress != nil {
		progress["overall_progress"] = clampPercent(*event.OverallProgress)
	}
	if event.Metadata != nil {
		progress["metadata"] = event.Metadata.Clone()
	}
	metadata["progress_event"] = progress
	return metadata
}

func clampPercent(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func normalizeRasterMosaicTask(task *models.RasterMosaicTask) error {
	if task == nil {
		return errors.New("raster mosaic generation task is nil")
	}
	task.Name = strings.TrimSpace(task.Name)
	task.Description = strings.TrimSpace(task.Description)
	task.Schedule = strings.TrimSpace(task.Schedule)
	if task.Config == nil {
		task.Config = commonModels.JSONMap{}
	}
	if task.Name == "" {
		return errors.New("raster mosaic generation task name is required")
	}
	if len(task.Config) == 0 {
		return errors.New("raster mosaic generation task config is required")
	}
	if task.Schedule != "" || task.NextRunAt != nil {
		return errors.New("raster mosaic generation task does not support schedule")
	}
	_, err := normalizeRasterMosaicTaskConfig(task.Config)
	return err
}

func readRasterMosaicExecutionConfig(config commonModels.JSONMap) (RasterMosaicExecutionConfig, error) {
	return normalizeRasterMosaicTaskConfig(config)
}

func normalizeRasterMosaicTaskConfig(config commonModels.JSONMap) (RasterMosaicExecutionConfig, error) {
	source, err := normalizeRasterMosaicSource(config)
	if err != nil {
		return RasterMosaicExecutionConfig{}, err
	}
	placement, err := normalizeRasterMosaicPlacement(config)
	if err != nil {
		return RasterMosaicExecutionConfig{}, err
	}
	target, err := normalizeRasterMosaicTarget(config, source, placement)
	if err != nil {
		return RasterMosaicExecutionConfig{}, err
	}
	cog := normalizeRasterMosaicCOG(config)
	overview := normalizeRasterMosaicOverview(config)
	tiles := normalizeRasterMosaicTiles(config)
	return RasterMosaicExecutionConfig{
		Source: source, Placement: placement, Target: target, COG: cog, Overview: overview, Tiles: tiles,
	}, nil
}

func normalizeRasterMosaicSource(config commonModels.JSONMap) (RasterMosaicSourceConfig, error) {
	sourceMap, ok := asJSONMap(config["source"])
	if !ok {
		return RasterMosaicSourceConfig{}, errors.New("raster mosaic config.source is required")
	}
	source := RasterMosaicSourceConfig{
		NodeLocator:     stringFromConfig(sourceMap["node_locator"]),
		SourceEngineID:  uintFromConfig(sourceMap["source_engine_id"]),
		Recursive:       boolFromConfig(sourceMap["recursive"], true),
		IncludePatterns: stringSliceFromMosaicConfig(sourceMap["include_patterns"]),
		ExcludePatterns: stringSliceFromMosaicConfig(sourceMap["exclude_patterns"]),
	}
	if source.NodeLocator == "" || source.SourceEngineID == 0 {
		return RasterMosaicSourceConfig{}, errors.New("raster mosaic config.source requires node_locator and source_engine_id")
	}
	loc, err := resourcetree.ParseURI(source.NodeLocator)
	if err != nil {
		return RasterMosaicSourceConfig{}, fmt.Errorf("raster mosaic config.source.node_locator is invalid: %w", err)
	}
	if loc.EngineID != source.SourceEngineID {
		return RasterMosaicSourceConfig{}, errors.New("raster mosaic config.source.node_locator engine_id does not match source_engine_id")
	}
	if len(source.IncludePatterns) == 0 {
		source.IncludePatterns = []string{"*.tif", "*.tiff"}
	}
	normalized := commonModels.JSONMap{
		"node_locator":     source.NodeLocator,
		"source_engine_id": source.SourceEngineID,
		"recursive":        source.Recursive,
		"include_patterns": source.IncludePatterns,
		"exclude_patterns": source.ExcludePatterns,
	}
	config["source"] = normalized
	return source, nil
}

func normalizeRasterMosaicPlacement(config commonModels.JSONMap) (RasterMosaicPlacementConfig, error) {
	placementMap, ok := asJSONMap(config["placement"])
	if !ok {
		return RasterMosaicPlacementConfig{}, errors.New("raster mosaic config.placement is required")
	}
	placement := RasterMosaicPlacementConfig{
		Mode: strings.ToLower(stringFromConfig(placementMap["mode"])),
	}
	switch placement.Mode {
	case "in_place":
	case "detached":
	default:
		return RasterMosaicPlacementConfig{}, errors.New("raster mosaic config.placement.mode must be in_place or detached")
	}
	config["placement"] = commonModels.JSONMap{
		"mode": placement.Mode,
	}
	return placement, nil
}

func normalizeRasterMosaicTarget(config commonModels.JSONMap, source RasterMosaicSourceConfig, placement RasterMosaicPlacementConfig) (RasterMosaicTargetConfig, error) {
	targetMap, ok := asJSONMap(config["target"])
	if !ok {
		if placement.Mode != "in_place" {
			return RasterMosaicTargetConfig{}, errors.New("raster mosaic config.target is required when placement.mode is detached")
		}
		targetMap = commonModels.JSONMap{
			"storage_locator":  source.NodeLocator,
			"target_engine_id": source.SourceEngineID,
		}
	}
	target := RasterMosaicTargetConfig{
		StorageLocator: stringFromConfig(targetMap["storage_locator"]),
		TargetEngineID: uintFromConfig(targetMap["target_engine_id"]),
		DatasetName:    stringFromConfig(targetMap["dataset_name"]),
	}
	if target.StorageLocator == "" || target.TargetEngineID == 0 {
		return RasterMosaicTargetConfig{}, errors.New("raster mosaic config.target requires storage_locator and target_engine_id")
	}
	loc, err := resourcetree.ParseURI(target.StorageLocator)
	if err != nil {
		return RasterMosaicTargetConfig{}, fmt.Errorf("raster mosaic config.target.storage_locator is invalid: %w", err)
	}
	if loc.EngineID != target.TargetEngineID {
		return RasterMosaicTargetConfig{}, errors.New("raster mosaic config.target.storage_locator engine_id does not match target_engine_id")
	}
	sameNode := rasterMosaicSameLocator(loc, mustParseRasterMosaicSourceLocator(source.NodeLocator))
	if placement.Mode == "in_place" && !sameNode {
		return RasterMosaicTargetConfig{}, errors.New("raster mosaic config.target.storage_locator must equal source.node_locator when placement.mode is in_place")
	}
	if placement.Mode == "detached" && sameNode {
		return RasterMosaicTargetConfig{}, errors.New("raster mosaic config.target.storage_locator must differ from source.node_locator when placement.mode is detached")
	}
	if target.DatasetName == "" {
		target.DatasetName = "raster_mosaic"
	}
	config["target"] = commonModels.JSONMap{
		"storage_locator":  target.StorageLocator,
		"target_engine_id": target.TargetEngineID,
		"dataset_name":     target.DatasetName,
	}
	return target, nil
}

func mustParseRasterMosaicSourceLocator(locator string) *resourcetree.ResourceLocator {
	parsed, err := resourcetree.ParseURI(locator)
	if err != nil {
		return nil
	}
	return parsed
}

func rasterMosaicSameLocator(a, b *resourcetree.ResourceLocator) bool {
	if a == nil || b == nil {
		return false
	}
	if a.EngineID != b.EngineID || a.Type != b.Type || !reflect.DeepEqual(a.Path, b.Path) {
		return false
	}
	if (a.NodeID == nil) != (b.NodeID == nil) || (a.ItemID == nil) != (b.ItemID == nil) {
		return false
	}
	if a.NodeID != nil && b.NodeID != nil && *a.NodeID != *b.NodeID {
		return false
	}
	if a.ItemID != nil && b.ItemID != nil && *a.ItemID != *b.ItemID {
		return false
	}
	return true
}

func normalizeRasterMosaicCOG(config commonModels.JSONMap) RasterMosaicCOGConfig {
	cogMap, _ := asJSONMap(config["cog"])
	cog := RasterMosaicCOGConfig{
		Compression:        firstNonEmptyConfig(stringFromConfig(cogMap["compression"]), "DEFLATE"),
		BlockSize:          intFromConfig(cogMap["blocksize"]),
		OverviewResampling: firstNonEmptyConfig(stringFromConfig(cogMap["overview_resampling"]), "NEAREST"),
		ValidateSourceCOG:  boolFromConfig(cogMap["validate_source_cog"], true),
		LeafConcurrency:    intFromConfig(cogMap["leaf_concurrency"]),
		NumThreads:         intFromConfig(cogMap["num_threads"]),
		LeafRetryAttempts:  intFromConfig(cogMap["leaf_retry_attempts"]),
	}
	if cog.BlockSize <= 0 {
		cog.BlockSize = 512
	}
	if cog.LeafConcurrency <= 0 {
		cog.LeafConcurrency = defaultRasterMosaicLeafConcurrency()
	}
	if cog.LeafConcurrency > rasterMosaicLeafConcurrencyMax {
		cog.LeafConcurrency = rasterMosaicLeafConcurrencyMax
	}
	if cog.NumThreads <= 0 {
		cog.NumThreads = defaultRasterMosaicLeafNumThreads(cog.LeafConcurrency)
	}
	if cog.LeafRetryAttempts <= 0 {
		cog.LeafRetryAttempts = 2
	}
	if cog.LeafRetryAttempts > rasterMosaicLeafRetryAttemptsMax {
		cog.LeafRetryAttempts = rasterMosaicLeafRetryAttemptsMax
	}
	config["cog"] = commonModels.JSONMap{
		"compression":         cog.Compression,
		"blocksize":           cog.BlockSize,
		"overview_resampling": cog.OverviewResampling,
		"validate_source_cog": cog.ValidateSourceCOG,
		"leaf_concurrency":    cog.LeafConcurrency,
		"num_threads":         cog.NumThreads,
		"leaf_retry_attempts": cog.LeafRetryAttempts,
	}
	return cog
}

const rasterMosaicLeafConcurrencyMax = 8
const rasterMosaicLeafRetryAttemptsMax = 5

func defaultRasterMosaicLeafConcurrency() int {
	cpuCount := runtime.NumCPU()
	switch {
	case cpuCount >= 32:
		return 6
	case cpuCount >= 16:
		return 4
	case cpuCount >= 8:
		return 2
	default:
		return 1
	}
}

func defaultRasterMosaicLeafNumThreads(leafConcurrency int) int {
	if leafConcurrency <= 0 {
		leafConcurrency = defaultRasterMosaicLeafConcurrency()
	}
	threads := runtime.NumCPU() / (leafConcurrency * 2)
	if threads < 1 {
		return 1
	}
	if threads > 4 {
		return 4
	}
	return threads
}

func normalizeRasterMosaicOverview(config commonModels.JSONMap) RasterMosaicOverviewConfig {
	overviewMap, _ := asJSONMap(config["overview"])
	overview := RasterMosaicOverviewConfig{
		Enabled:    boolFromConfig(overviewMap["enabled"], true),
		MaxPixels:  int64FromConfig(overviewMap["max_pixels"], 64000000),
		Resampling: firstNonEmptyConfig(stringFromConfig(overviewMap["resampling"]), "AVERAGE"),
	}
	if !overview.Enabled {
		overview.Enabled = true
	}
	config["overview"] = commonModels.JSONMap{
		"enabled":    overview.Enabled,
		"max_pixels": overview.MaxPixels,
		"resampling": overview.Resampling,
	}
	return overview
}

func normalizeRasterMosaicTiles(config commonModels.JSONMap) RasterMosaicTilesConfig {
	tilesMap, _ := asJSONMap(config["tiles"])
	tiles := RasterMosaicTilesConfig{
		Enabled: boolFromConfig(tilesMap["enabled"], false),
		MinZoom: intFromConfig(tilesMap["min_zoom"]),
		MaxZoom: intFromConfig(tilesMap["max_zoom"]),
		Format:  firstNonEmptyConfig(stringFromConfig(tilesMap["format"]), "webp"),
	}
	if tiles.MaxZoom < tiles.MinZoom {
		tiles.MaxZoom = tiles.MinZoom
	}
	config["tiles"] = commonModels.JSONMap{
		"enabled":  tiles.Enabled,
		"min_zoom": tiles.MinZoom,
		"max_zoom": tiles.MaxZoom,
		"format":   tiles.Format,
	}
	return tiles
}

func stringSliceFromMosaicConfig(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	case []interface{}:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				if trimmed := strings.TrimSpace(text); trimmed != "" {
					result = append(result, trimmed)
				}
			}
		}
		return result
	default:
		return nil
	}
}
