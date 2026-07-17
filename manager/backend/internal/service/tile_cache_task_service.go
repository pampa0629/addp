package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	commonScheduler "github.com/addp/common/scheduler"
	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/mvt"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/tilecache"
	"github.com/google/uuid"
)

const tileCacheTaskSchedulePollInterval = time.Minute
const tileCacheProgressUpdateMinInterval = 2 * time.Second

type TileCacheGenerator interface {
	GenerateMixed(ctx context.Context, cfg mvt.QuickViewConfig, progressTracker mvt.ProgressSink) (*mvt.GenerateResult, error)
}

type WorkflowTileCacheGenerator interface {
	GenerateVectorTileCache(ctx context.Context, req WorkflowTileCacheRequest) (*mvt.GenerateResult, commonModels.JSONMap, error)
}

type RealtimeTileTargetResolver interface {
	ResolveRealtimeTileTarget(ctx context.Context, tenantID *uint, resourceID uint, schema, table, geomCol string, sourceSRID int) (*RealtimeTileTarget, error)
}

type TileCacheCleaner interface {
	DeleteByStorageRef(ctx context.Context, storageRef string) error
}

type TileCacheRuntimeCacheInvalidator interface {
	InvalidateTileCacheRuntimeCache(ctx context.Context, tenantID uint, tileCacheID uint) error
}

type TileCacheTaskService struct {
	tileCacheRepo               *repository.TileCacheRepository
	taskExecRepo                *commonExecution.TaskExecutionRepository
	quickViewSvc                *QuickViewService
	tileGenerator               TileCacheGenerator
	workflowTileGenerator       WorkflowTileCacheGenerator
	tileTargetResolver          RealtimeTileTargetResolver
	tileCacheCleaner            TileCacheCleaner
	tileCacheRuntimeInvalidator TileCacheRuntimeCacheInvalidator
	defaultConcurrency          int
}

type tileCacheExecutionProgressSink struct {
	repo          *commonExecution.TaskExecutionRepository
	executionID   string
	tenantID      uint
	startedAt     time.Time
	mu            sync.Mutex
	lastUpdatedAt time.Time
	lastProgress  int
	lastMetadata  commonModels.JSONMap
}

type WorkflowTileCacheRequest struct {
	Task         *models.TileCacheTask
	ExecutionID  string
	Identity     tileCacheTaskTargetIdentity
	StorageRef   string
	Tile         commonModels.JSONMap
	Options      commonModels.JSONMap
	ProgressSink mvt.ProgressSink
}

type TileCacheProgressEvent struct {
	Phase              string
	Event              string
	Message            string
	CurrentZoom        int
	MaxZoom            int
	TilesProcessed     int
	TilesTotalEstimate int
	ProgressPercent    *float64
	OverallProgress    *float64
	ElapsedSeconds     *float64
	RemainingSeconds   *float64
	Metadata           commonModels.JSONMap
}

var (
	ErrTileCacheProgressTargetMismatch = errors.New("vector tile cache progress event target mismatch")
	ErrTileCacheExecutionCompleted     = errors.New("vector tile cache execution is already completed")
)

func NewTileCacheTaskService(
	tileCacheRepo *repository.TileCacheRepository,
	taskExecRepo *commonExecution.TaskExecutionRepository,
) *TileCacheTaskService {
	return &TileCacheTaskService{
		tileCacheRepo:      tileCacheRepo,
		taskExecRepo:       taskExecRepo,
		defaultConcurrency: 4,
	}
}

func (s *TileCacheTaskService) SetQuickViewService(quickViewSvc *QuickViewService) {
	s.quickViewSvc = quickViewSvc
}

func (s *TileCacheTaskService) SetTileGenerator(generator TileCacheGenerator, defaultConcurrency int) {
	s.tileGenerator = generator
	if defaultConcurrency > 0 {
		s.defaultConcurrency = defaultConcurrency
	}
}

func (s *TileCacheTaskService) SetWorkflowTileGenerator(generator WorkflowTileCacheGenerator) {
	s.workflowTileGenerator = generator
}

func (s *TileCacheTaskService) SetRealtimeTileTargetResolver(resolver RealtimeTileTargetResolver) {
	s.tileTargetResolver = resolver
}

func (s *TileCacheTaskService) SetTileCacheCleaner(cleaner TileCacheCleaner) {
	s.tileCacheCleaner = cleaner
}

func (s *TileCacheTaskService) SetTileCacheRuntimeCacheInvalidator(invalidator TileCacheRuntimeCacheInvalidator) {
	s.tileCacheRuntimeInvalidator = invalidator
}

func (s *TileCacheTaskService) newTileCacheExecutionProgressSink(executionID string, tenantID uint, startedAt time.Time) *tileCacheExecutionProgressSink {
	return &tileCacheExecutionProgressSink{
		repo:          s.taskExecRepo,
		executionID:   executionID,
		tenantID:      tenantID,
		startedAt:     startedAt,
		lastUpdatedAt: startedAt,
	}
}

func (s *tileCacheExecutionProgressSink) UpdateProgress(ctx context.Context, progress *mvt.QuickViewProgress) error {
	if s == nil || s.repo == nil || progress == nil {
		return nil
	}
	nextProgress := progressPercentInt(progress)
	now := time.Now()
	s.mu.Lock()
	if nextProgress < s.lastProgress {
		nextProgress = s.lastProgress
	}
	shouldUpdate := nextProgress != s.lastProgress ||
		now.Sub(s.lastUpdatedAt) >= tileCacheProgressUpdateMinInterval ||
		progress.Status != "running"
	if !shouldUpdate {
		s.mu.Unlock()
		return nil
	}
	s.lastProgress = nextProgress
	s.lastUpdatedAt = now
	s.mu.Unlock()

	currentStep := tileCacheProgressStep(progress)
	elapsedMs := now.Sub(s.startedAt).Milliseconds()
	metadata := commonModels.JSONMap{
		"tiles_processed":         progress.TilesProcessed,
		"tiles_total_estimate":    progress.TilesTotalEstimate,
		"progress_percent":        progress.ProgressPercent,
		"current_zoom":            progress.CurrentZoom,
		"max_zoom":                progress.MaxZoom,
		"elapsed_seconds":         progress.ElapsedSeconds,
		"estimated_remaining_sec": progress.EstimatedRemainingSec,
	}
	if progress.ErrorMessage != "" {
		metadata["progress_error"] = progress.ErrorMessage
	}
	s.mu.Lock()
	s.lastMetadata = metadata.Clone()
	s.mu.Unlock()
	return s.repo.UpdateFields(ctx, s.executionID, int(s.tenantID), map[string]interface{}{
		"progress":          nextProgress,
		"current_step":      currentStep,
		"metadata":          metadata,
		"execution_time_ms": elapsedMs,
		"updated_at":        now,
	})
}

func (s *tileCacheExecutionProgressSink) LastProgress() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastProgress
}

func (s *tileCacheExecutionProgressSink) LastMetadata() commonModels.JSONMap {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastMetadata == nil {
		return nil
	}
	return s.lastMetadata.Clone()
}

func mergeTileCacheProgressMetadata(metadata commonModels.JSONMap, progressMetadata commonModels.JSONMap) {
	if metadata == nil || progressMetadata == nil {
		return
	}
	for key, value := range progressMetadata {
		if _, exists := metadata[key]; !exists {
			metadata[key] = value
		}
	}
}

func progressPercentInt(progress *mvt.QuickViewProgress) int {
	if progress == nil {
		return 0
	}
	percent := progress.ProgressPercent
	if percent <= 0 && progress.TilesTotalEstimate > 0 {
		percent = float64(progress.TilesProcessed) / float64(progress.TilesTotalEstimate) * 100
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return int(math.Round(percent))
}

func tileCacheProgressStep(progress *mvt.QuickViewProgress) string {
	if progress == nil {
		return "生成瓦片缓存"
	}
	if progress.TilesTotalEstimate > 0 {
		return fmt.Sprintf("生成瓦片缓存 z%d/%d：%d/%d", progress.CurrentZoom, progress.MaxZoom, progress.TilesProcessed, progress.TilesTotalEstimate)
	}
	if progress.MaxZoom > 0 {
		return fmt.Sprintf("生成瓦片缓存 z%d/%d", progress.CurrentZoom, progress.MaxZoom)
	}
	return "生成瓦片缓存"
}

func (s *TileCacheTaskService) RecordProgressEvent(ctx context.Context, tenantID uint, executionID string, event TileCacheProgressEvent) error {
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
	if event.Phase == "" {
		return errors.New("phase is required")
	}
	if event.Event == "" {
		return errors.New("event is required")
	}
	if event.TilesProcessed < 0 || event.TilesTotalEstimate < 0 {
		return errors.New("tile counters must be greater than or equal to 0")
	}
	if event.TilesTotalEstimate > 0 && event.TilesProcessed > event.TilesTotalEstimate {
		return errors.New("tiles_processed cannot be greater than tiles_total_estimate")
	}

	exec, err := s.taskExecRepo.GetByExecutionID(ctx, executionID, int(tenantID))
	if err != nil {
		return err
	}
	if exec.Module != commonExecution.ModuleManager || exec.TaskType != commonExecution.TaskTypeVectorTileCacheGeneration {
		return ErrTileCacheProgressTargetMismatch
	}
	if exec.IsCompleted() {
		return ErrTileCacheExecutionCompleted
	}

	now := time.Now()
	nextProgress := tileCacheEventProgressPercent(event, exec.Progress)
	progress := &mvt.QuickViewProgress{
		Status:                commonExecution.ExecutionStatusRunning,
		CurrentZoom:           event.CurrentZoom,
		MaxZoom:               event.MaxZoom,
		TilesProcessed:        event.TilesProcessed,
		TilesTotalEstimate:    event.TilesTotalEstimate,
		ProgressPercent:       float64(nextProgress),
		ElapsedSeconds:        optionalFloat64(event.ElapsedSeconds),
		EstimatedRemainingSec: optionalFloat64(event.RemainingSeconds),
	}
	currentStep := event.Message
	if currentStep == "" {
		currentStep = tileCacheProgressStep(progress)
	}
	metadata := tileCacheProgressEventMetadata(exec.Metadata, event, nextProgress)
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

func tileCacheEventProgressPercent(event TileCacheProgressEvent, current int) int {
	next := current
	switch {
	case event.OverallProgress != nil:
		next = int(math.Round(*event.OverallProgress))
	case event.ProgressPercent != nil:
		next = int(math.Round(*event.ProgressPercent))
	case event.TilesTotalEstimate > 0:
		next = int(math.Round(float64(event.TilesProcessed) / float64(event.TilesTotalEstimate) * 100))
	}
	next = clampPercent(next)
	if next < current {
		return clampPercent(current)
	}
	return next
}

func tileCacheProgressEventMetadata(existing commonModels.JSONMap, event TileCacheProgressEvent, nextProgress int) commonModels.JSONMap {
	metadata := commonModels.JSONMap{}
	if existing != nil {
		metadata = existing.Clone()
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
	}
	progress := commonModels.JSONMap{
		"phase":                event.Phase,
		"event":                event.Event,
		"message":              event.Message,
		"current_zoom":         event.CurrentZoom,
		"max_zoom":             event.MaxZoom,
		"tiles_processed":      event.TilesProcessed,
		"tiles_total_estimate": event.TilesTotalEstimate,
		"overall_progress":     nextProgress,
		"progress_percent":     nextProgress,
	}
	if event.ProgressPercent != nil {
		progress["progress_percent"] = *event.ProgressPercent
	}
	if event.OverallProgress != nil {
		progress["overall_progress"] = *event.OverallProgress
	}
	if event.ElapsedSeconds != nil {
		progress["elapsed_seconds"] = *event.ElapsedSeconds
	}
	if event.RemainingSeconds != nil {
		progress["estimated_remaining_seconds"] = *event.RemainingSeconds
	}
	if event.Metadata != nil {
		progress["metadata"] = event.Metadata.Clone()
	}
	metadata["progress_event"] = progress
	metadata["tiles_processed"] = event.TilesProcessed
	metadata["tiles_total_estimate"] = event.TilesTotalEstimate
	metadata["progress_percent"] = nextProgress
	metadata["current_zoom"] = event.CurrentZoom
	metadata["max_zoom"] = event.MaxZoom
	return metadata
}

func optionalFloat64(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func (s *TileCacheTaskService) Create(ctx context.Context, task *models.TileCacheTask) error {
	if err := normalizeTileCacheTask(task); err != nil {
		return err
	}
	identity, format, err := tileCacheTaskSemanticIdentity(task.Config)
	if err != nil {
		return err
	}
	existing, err := s.tileCacheRepo.GetTaskByTargetFingerprintAndFormat(ctx, task.TenantID, identity.ItemFingerprint, format, 0)
	if err != nil {
		return err
	}
	if existing != nil {
		return s.reuseExistingTask(ctx, task, existing)
	}
	if err := s.tileCacheRepo.CreateTask(ctx, task); err != nil {
		if strings.Contains(err.Error(), "idx_vector_tile_cache_tasks_source_format_unique") {
			existing, lookupErr := s.tileCacheRepo.GetTaskByTargetFingerprintAndFormat(ctx, task.TenantID, identity.ItemFingerprint, format, 0)
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

func (s *TileCacheTaskService) reuseExistingTask(ctx context.Context, task, existing *models.TileCacheTask) error {
	existing.Name = task.Name
	existing.Description = task.Description
	existing.Enabled = task.Enabled
	existing.Schedule = task.Schedule
	existing.NextRunAt = task.NextRunAt
	existing.Config = task.Config.Clone()
	if existing.CreatedBy == nil {
		existing.CreatedBy = task.CreatedBy
	}
	if err := s.tileCacheRepo.UpdateTask(ctx, existing); err != nil {
		return err
	}
	*task = *existing
	return nil
}

func (s *TileCacheTaskService) GetByID(ctx context.Context, id uint, tenantID uint) (*models.TileCacheTask, error) {
	return s.tileCacheRepo.GetTask(ctx, id, tenantID)
}

func (s *TileCacheTaskService) List(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.TileCacheTask, int64, error) {
	return s.tileCacheRepo.ListTasks(ctx, tenantID, page, pageSize)
}

func (s *TileCacheTaskService) Update(ctx context.Context, task *models.TileCacheTask) error {
	if err := normalizeTileCacheTask(task); err != nil {
		return err
	}
	if err := s.ensureUniqueTileCacheTaskTarget(ctx, task, task.ID); err != nil {
		return err
	}
	return s.tileCacheRepo.UpdateTask(ctx, task)
}

func (s *TileCacheTaskService) Delete(ctx context.Context, id uint, tenantID uint) error {
	return s.tileCacheRepo.DeleteTask(ctx, id, tenantID)
}

func (s *TileCacheTaskService) ListTileCache(ctx context.Context, filter repository.TileCacheFilter) ([]*models.TileCache, int64, error) {
	return s.tileCacheRepo.ListTileCache(ctx, filter)
}

func (s *TileCacheTaskService) GetTileCache(ctx context.Context, id uint, tenantID uint) (*models.TileCache, error) {
	return s.tileCacheRepo.GetTileCache(ctx, id, tenantID)
}

func (s *TileCacheTaskService) DeleteTileCache(ctx context.Context, id uint, tenantID uint) error {
	result, err := s.tileCacheRepo.GetTileCache(ctx, id, tenantID)
	if err != nil || result == nil {
		return err
	}
	if s.tileCacheRuntimeInvalidator != nil {
		if err := s.tileCacheRuntimeInvalidator.InvalidateTileCacheRuntimeCache(ctx, tenantID, result.ID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(result.StorageRef) != "" {
		if s.tileCacheCleaner == nil {
			return errors.New("tile cache cleaner is required")
		}
		if err := s.tileCacheCleaner.DeleteByStorageRef(ctx, result.StorageRef); err != nil {
			return err
		}
	}
	if err := s.tileCacheRepo.DeleteTileCache(ctx, id, tenantID); err != nil {
		return err
	}
	return nil
}

func (s *TileCacheTaskService) Execute(ctx context.Context, taskID uint, tenantID uint, triggerType string, source string, parentExecutionID *string, confirmExistingResult bool) (string, error) {
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
	exec := &commonExecution.TaskExecution{
		ExecutionID:       executionID,
		TenantID:          int(tenantID),
		Module:            commonExecution.ModuleManager,
		TaskType:          commonExecution.TaskTypeVectorTileCacheGeneration,
		Source:            normalizedSource,
		ParentExecutionID: parentExecutionID,
		Status:            commonExecution.ExecutionStatusPending,
		TriggerType:       normalizedTriggerType,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	claimedTask, err := s.tileCacheRepo.ClaimExecution(ctx, taskID, tenantID, exec, confirmExistingResult)
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

	go s.runTileCacheGeneration(context.Background(), claimedTask, executionID)
	return executionID, nil
}

func (s *TileCacheTaskService) runTileCacheGeneration(ctx context.Context, task *models.TileCacheTask, executionID string) {
	startedAt := time.Now()
	if err := s.tileCacheRepo.StartExecution(ctx, task.ID, task.TenantID, executionID, startedAt); err != nil {
		logger.L().Warn("领取瓦片缓存 execution 失败", "execution_id", executionID, "task_id", task.ID, "error", err)
		return
	}
	status := commonExecution.ExecutionStatusSuccess
	var errDetails commonModels.JSONMap
	metadata := commonModels.JSONMap{}
	completedAt := time.Now()
	resultReadyPersisted := false
	preserveReadyTileCache := false
	var tileCacheFields map[string]interface{}

	tileCache, execCfg, cfg, workflowReq, preserveReadyTileCache, err := s.prepareExecutionTileCache(ctx, task, executionID)
	if err == nil && workflowReq == nil && s.tileGenerator == nil {
		err = errors.New("tile cache generation executor is not connected")
	}
	if err == nil && workflowReq != nil && s.workflowTileGenerator == nil {
		err = errors.New("workflow tile cache generation executor is not connected")
	}
	progressSink := s.newTileCacheExecutionProgressSink(executionID, task.TenantID, startedAt)
	tileGenerationTargetMetadata, _ := asJSONMap(execCfg["tile_generation_target"])
	optimizationMetadata, _ := asJSONMap(execCfg["optimization"])
	if err == nil {
		var result *mvt.GenerateResult
		var workflowMetadata commonModels.JSONMap
		var genErr error
		if workflowReq != nil {
			workflowReq.ProgressSink = progressSink
			result, workflowMetadata, genErr = s.workflowTileGenerator.GenerateVectorTileCache(ctx, *workflowReq)
			if workflowMetadata != nil {
				execCfg["workflow_runtime"] = workflowMetadata
			}
			cfg = mvt.QuickViewConfig{
				EngineID:    workflowReq.Identity.EngineID,
				TenantID:    task.TenantID,
				Schema:      workflowReq.Identity.SourceKind,
				Table:       workflowReq.Identity.FullName,
				GeomColumn:  stringFromConfig(workflowReq.Options["geometry_column"]),
				SRID:        intFromTileCacheConfig(workflowReq.Tile["source_srid"], 0),
				ExtentSRID:  intFromTileCacheConfig(workflowReq.Tile["extent_srid"], 0),
				Extent:      []float64{},
				MinZoom:     intFromTileCacheConfig(workflowReq.Tile["min_zoom"], 0),
				MaxZoom:     intFromTileCacheConfig(workflowReq.Tile["max_zoom"], 0),
				Fingerprint: workflowReq.Identity.ItemFingerprint,
				StorageRef:  workflowReq.StorageRef,
			}
			if extent, ok := floatSliceFromConfig(workflowReq.Tile["extent"]); ok {
				cfg.Extent = extent
			}
		} else {
			result, genErr = s.tileGenerator.GenerateMixed(ctx, cfg, progressSink)
		}
		if genErr != nil {
			err = genErr
		} else if result == nil {
			err = errors.New("tile cache generation returned empty result")
		} else {
			completedAt = time.Now()
			metadata = buildTileCacheGenerationMetadata(
				tileCache.ID,
				cfg,
				result,
				tileGenerationTargetMetadata,
				optimizationMetadata,
				execCfg,
			)
			if workflowMetadata != nil {
				metadata["workflow_runtime"] = workflowMetadata
			}
			if result.CachedTiles <= 0 {
				err = errors.New("tile cache generation produced no non-empty tiles")
			} else {
				status = commonExecution.ExecutionStatusSuccess
			}
		}
		if err == nil {
			updates := map[string]interface{}{
				"status":            models.TileCacheStatusReady,
				"storage_ref":       cfg.StorageRef,
				"last_execution_id": executionID,
				"min_zoom":          cfg.MinZoom,
				"max_zoom":          result.ActualMaxZoom,
				"error_message":     "",
			}
			resultExtent := cfg.Extent
			resultExtentSRID := cfg.ExtentSRID
			if len(result.ExtentWGS84) == 4 {
				resultExtent = result.ExtentWGS84
				resultExtentSRID = spatial.SRIDWGS84
			}
			if len(resultExtent) == 4 {
				extentJSON, _ := json.Marshal(resultExtent)
				updates["extent"] = extentJSON
				if resultExtentSRID > 0 {
					updates["extent_srid"] = resultExtentSRID
				}
			}
			tileCacheFields = updates
			resultReadyPersisted = true
		}
	}

	if err != nil {
		status = commonExecution.ExecutionStatusFailed
		errDetails = commonModels.JSONMap{"message": err.Error()}
		if tileCache != nil && !resultReadyPersisted && !preserveReadyTileCache {
			tileCacheFields = map[string]interface{}{
				"status":            models.TileCacheStatusFailed,
				"error_message":     err.Error(),
				"last_execution_id": executionID,
			}
		}
	}

	completedAt = time.Now()
	durationMs := completedAt.Sub(startedAt).Milliseconds()
	finalProgress := progressSink.LastProgress()
	if status == commonExecution.ExecutionStatusSuccess {
		finalProgress = 100
	}
	mergeTileCacheProgressMetadata(metadata, progressSink.LastMetadata())
	tileCacheID := uint(0)
	if tileCache != nil {
		tileCacheID = tileCache.ID
	}
	if err := s.tileCacheRepo.CompleteExecution(ctx, task.ID, task.TenantID, executionID, tileCacheID, tileCacheFields, map[string]interface{}{
		"status":            status,
		"progress":          finalProgress,
		"metadata":          metadata,
		"error_details":     errDetails,
		"completed_at":      completedAt,
		"execution_time_ms": durationMs,
		"updated_at":        completedAt,
	}, completedAt); err != nil {
		logger.L().Warn("提交瓦片缓存 execution 终态失败", "execution_id", executionID, "task_id", task.ID, "error", err)
	}
}

func buildTileCacheGenerationMetadata(
	tileCacheID uint,
	cfg mvt.QuickViewConfig,
	result *mvt.GenerateResult,
	tileGenerationTargetMetadata commonModels.JSONMap,
	optimizationMetadata commonModels.JSONMap,
	execCfg commonModels.JSONMap,
) commonModels.JSONMap {
	tileConfig, _ := asJSONMap(execCfg["tile"])
	sourceSRID := intFromTileCacheConfig(tileConfig["source_srid"], cfg.SRID)
	tileTargetSRID := intFromTileCacheConfig(tileConfig["target_srid"], spatial.SRIDWebMercator)

	metadata := commonModels.JSONMap{
		"vector_tile_cache_id":    tileCacheID,
		"actual_max_zoom":         result.ActualMaxZoom,
		"min_zoom":                cfg.MinZoom,
		"max_zoom":                cfg.MaxZoom,
		"source_srid":             sourceSRID,
		"target_srid":             tileTargetSRID,
		"extent_srid":             cfg.ExtentSRID,
		"extent":                  cfg.Extent,
		"geometry_column":         cfg.GeomColumn,
		"total_tiles":             result.TotalTiles,
		"cached_tiles":            result.CachedTiles,
		"tiles_total_estimate":    result.TilesTotalEstimate,
		"tiles_processed":         result.TilesProcessed,
		"generated_tiles":         result.GeneratedTiles,
		"empty_tiles":             result.EmptyTiles,
		"skipped_tiles":           result.SkippedTiles,
		"oversized_skipped_tiles": result.OversizedTiles,
		"failed_tiles":            result.FailedTiles,
		"total_size_bytes":        result.TotalSizeBytes,
		"max_tile_size_bytes":     result.MaxTileSizeBytes,
		"min_tile_size_bytes":     result.MinTileSizeBytes,
		"generation_seconds":      result.GenerationSec,
		"stop_reason":             result.StopReason,
		"refresh_actions": commonModels.JSONMap{
			"previous_runtime_cache_invalidated": boolFromJSONMap(execCfg, "previous_runtime_cache_invalidated"),
			"previous_storage_ref_deleted":       boolFromJSONMap(execCfg, "previous_storage_ref_deleted"),
		},
	}
	if len(result.ExtentWGS84) == 4 {
		metadata["tile_range_extent_wgs84"] = result.ExtentWGS84
	}
	if len(result.ZoomLevels) > 0 {
		metadata["zoom_levels"] = result.ZoomLevels
	}
	if tileGenerationTargetMetadata != nil {
		metadata["tile_generation_target"] = tileGenerationTargetMetadata
	}
	if optimizationMetadata != nil {
		metadata["optimization"] = optimizationMetadata
	}
	return metadata
}

func boolFromJSONMap(values commonModels.JSONMap, key string) bool {
	if values == nil {
		return false
	}
	value, ok := values[key].(bool)
	return ok && value
}

func (s *TileCacheTaskService) prepareExecutionTileCache(ctx context.Context, task *models.TileCacheTask, executionID string) (*models.TileCache, commonModels.JSONMap, mvt.QuickViewConfig, *WorkflowTileCacheRequest, bool, error) {
	execCfg := task.Config.Clone()
	if execCfg == nil {
		execCfg = commonModels.JSONMap{}
	}
	tile, _ := asJSONMap(execCfg["tile"])
	options, _ := asJSONMap(execCfg["options"])
	if tile == nil {
		tile = commonModels.JSONMap{}
		execCfg["tile"] = tile
	}
	if options == nil {
		options = commonModels.JSONMap{}
		execCfg["options"] = options
	}

	format := strings.ToLower(stringFromConfig(tile["format"]))
	if format == "" {
		format = "mvt"
	}
	if format != "mvt" {
		return nil, execCfg, mvt.QuickViewConfig{}, nil, false, fmt.Errorf("unsupported tile format %q", format)
	}

	identity, err := readTileCacheTaskTargetIdentity(execCfg)
	if err != nil {
		return nil, execCfg, mvt.QuickViewConfig{}, nil, false, err
	}
	engineID := identity.EngineID
	schema := identity.Schema
	table := identity.Table
	tableSource := identity.SourceKind == string(resourcetree.TypeTable)

	itemID := identity.ItemID
	var itemIDPtr *uint
	if itemID > 0 {
		itemIDPtr = &itemID
	}
	locator := identity.Locator
	minZoom := intFromTileCacheConfig(tile["min_zoom"], 0)
	maxZoom := intFromTileCacheConfig(tile["max_zoom"], 18)
	if maxZoom <= 0 || maxZoom < minZoom {
		return nil, execCfg, mvt.QuickViewConfig{}, nil, false, errors.New("tile cache task config.tile.max_zoom is invalid")
	}

	geomColumn := stringFromConfig(options["geometry_column"])
	sourceSRID := intFromTileCacheConfig(firstNonNil(tile["source_srid"], options["source_srid"]), 0)
	primaryKey := stringFromConfig(options["primary_key"])
	var extent []float64
	extentSRID := intFromTileCacheConfig(tile["extent_srid"], 0)
	if rawExtent, ok := floatSliceFromConfig(tile["extent"]); ok {
		extent = rawExtent
	}

	var spatialMeta *SpatialMetadataResult
	needsSpatialMeta := tableSource && (geomColumn == "" || primaryKey == "" || len(extent) == 0 || sourceSRID == 0)
	if needsSpatialMeta && s.quickViewSvc != nil {
		var err error
		spatialMeta, err = s.quickViewSvc.GetSpatialMetadataFromMeta(ctx, task.TenantID, engineID, schema, table)
		if err != nil {
			return nil, execCfg, mvt.QuickViewConfig{}, nil, false, err
		}
		if geomColumn == "" {
			geomColumn = spatialMeta.GeomColumn
		}
		if primaryKey == "" {
			primaryKey = spatialMeta.PrimaryKey
		}
		if len(extent) == 0 {
			extent = spatialMeta.Extent
			extentSRID = spatialMeta.ExtentSRID
		}
		if sourceSRID == 0 {
			sourceSRID = spatialMeta.SRID
		}
	}
	if geomColumn == "" {
		return nil, execCfg, mvt.QuickViewConfig{}, nil, false, ErrQuickViewGeometryColumnNotFound
	}
	if sourceSRID <= 0 {
		return nil, execCfg, mvt.QuickViewConfig{}, nil, false, errors.New("tile cache task source_srid is required")
	}
	if !tableSource && len(extent) != 4 {
		return nil, execCfg, mvt.QuickViewConfig{}, nil, false, errors.New("tile cache task tile.extent is required for non-table source")
	}

	fingerprint := identity.ItemFingerprint
	tileCache := &models.TileCache{
		TenantID:        task.TenantID,
		ItemFingerprint: fingerprint,
		ItemID:          itemIDPtr,
		Locator:         locator,
		TaskID:          &task.ID,
		LastExecutionID: &executionID,
		TileFormat:      format,
		Status:          models.TileCacheStatusGenerating,
		CreatedBy:       task.CreatedBy,
	}
	initialUpdates := map[string]interface{}{
		"item_id":           itemIDPtr,
		"locator":           locator,
		"task_id":           &task.ID,
		"last_execution_id": executionID,
		"created_by":        task.CreatedBy,
	}
	if len(extent) == 4 {
		extentJSON, _ := json.Marshal(extent)
		tileCache.Extent = extentJSON
		initialUpdates["extent"] = extentJSON
		if extentSRID > 0 {
			tileCache.ExtentSRID = &extentSRID
			initialUpdates["extent_srid"] = extentSRID
		}
	}
	tileCache.MinZoom = &minZoom
	tileCache.MaxZoom = &maxZoom
	initialUpdates["min_zoom"] = minZoom
	initialUpdates["max_zoom"] = maxZoom
	existingTileCache, err := s.tileCacheRepo.GetTileCacheByFingerprintAndFormat(ctx, task.TenantID, fingerprint, format)
	if err != nil {
		return nil, execCfg, mvt.QuickViewConfig{}, nil, false, err
	}
	if existingTileCache != nil {
		if s.tileCacheRuntimeInvalidator != nil {
			if err := s.tileCacheRuntimeInvalidator.InvalidateTileCacheRuntimeCache(ctx, task.TenantID, existingTileCache.ID); err != nil {
				return nil, execCfg, mvt.QuickViewConfig{}, nil, false, fmt.Errorf("invalidate existing tile cache runtime cache: %w", err)
			}
			execCfg["previous_runtime_cache_invalidated"] = true
		}
		if strings.TrimSpace(existingTileCache.StorageRef) != "" {
			if s.tileCacheCleaner == nil {
				return nil, execCfg, mvt.QuickViewConfig{}, nil, false, errors.New("tile cache cleaner is required to refresh existing tile cache")
			}
			if err := s.tileCacheCleaner.DeleteByStorageRef(ctx, existingTileCache.StorageRef); err != nil {
				return nil, execCfg, mvt.QuickViewConfig{}, nil, false, fmt.Errorf("delete existing tile cache objects: %w", err)
			}
			execCfg["previous_storage_ref_deleted"] = true
		}
		tileCache = existingTileCache
		initialUpdates["status"] = models.TileCacheStatusGenerating
		initialUpdates["storage_ref"] = ""
		initialUpdates["error_message"] = ""
		if err := s.tileCacheRepo.UpdateTileCacheFields(ctx, tileCache.ID, task.TenantID, initialUpdates); err != nil {
			return nil, execCfg, mvt.QuickViewConfig{}, nil, false, err
		}
	} else {
		if err := s.tileCacheRepo.CreateTileCache(ctx, tileCache); err != nil {
			return nil, execCfg, mvt.QuickViewConfig{}, nil, false, err
		}
	}

	storageRef := buildTileCacheStorageRef(task.TenantID, fingerprint)
	if identity.SourceKind != string(resourcetree.TypeTable) {
		execCfg["tile_generation_target"] = commonModels.JSONMap{
			"source_kind":              identity.SourceKind,
			"full_name":                identity.FullName,
			"geom_column":              geomColumn,
			"srid":                     sourceSRID,
			"target_kind":              "workflow_vector_tile_cache",
			"optimization_recommended": false,
		}
		options["geometry_column"] = geomColumn
		if primaryKey != "" {
			options["primary_key"] = primaryKey
		}
		if sourceSRID > 0 {
			tile["source_srid"] = sourceSRID
		}
		if len(extent) == 4 {
			tile["extent"] = extent
		}
		if extentSRID > 0 {
			tile["extent_srid"] = extentSRID
		}
		execCfg["tile"] = tile
		execCfg["options"] = options
		workflowReq := &WorkflowTileCacheRequest{
			Task:        task,
			ExecutionID: executionID,
			Identity:    identity,
			StorageRef:  storageRef,
			Tile:        tile.Clone(),
			Options:     options.Clone(),
		}
		return tileCache, execCfg, mvt.QuickViewConfig{}, workflowReq, false, nil
	}

	var tenantIDPtr *uint
	if task.TenantID > 0 {
		tenantID := task.TenantID
		tenantIDPtr = &tenantID
	}
	generationTarget, err := s.resolveTileGenerationTarget(ctx, tenantIDPtr, engineID, schema, table, geomColumn, sourceSRID, primaryKey)
	if err != nil {
		return nil, execCfg, mvt.QuickViewConfig{}, nil, false, err
	}
	cfg := mvt.QuickViewConfig{
		EngineID:           engineID,
		TenantID:           task.TenantID,
		Schema:             generationTarget.Schema,
		Table:              generationTarget.Table,
		GeomColumn:         generationTarget.GeomColumn,
		SRID:               generationTarget.SRID,
		ExtentSRID:         extentSRID,
		PrimaryKey:         generationTarget.PrimaryKey,
		Extent:             extent,
		MinZoom:            minZoom,
		MaxZoom:            maxZoom,
		Concurrency:        intFromTileCacheConfig(execCfg["concurrency"], s.defaultConcurrency),
		Fingerprint:        fingerprint,
		StorageRef:         storageRef,
		OptimizationConfig: tileCacheOptimizationConfig(execCfg),
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = s.defaultConcurrency
	}
	execCfg["tile_generation_target"] = commonModels.JSONMap{
		"schema":                      generationTarget.Schema,
		"table":                       generationTarget.Table,
		"geom_column":                 generationTarget.GeomColumn,
		"srid":                        generationTarget.SRID,
		"target_kind":                 generationTarget.TargetKind,
		"optimization_recommended":    generationTarget.OptimizationRecommended,
		"optimization_recommendation": generationTarget.OptimizationRecommendation,
	}
	return tileCache, execCfg, cfg, nil, false, nil
}

func tileCacheOptimizationConfig(config commonModels.JSONMap) *commonModels.OptimizationConfig {
	optimization := commonModels.DefaultOptimizationConfig()
	raw, ok := asJSONMap(config["optimization"])
	if !ok {
		config["optimization"] = optimizationJSONMap(optimization)
		return &optimization
	}
	applyTileCacheOptimizationConfig(&optimization, raw)
	config["optimization"] = optimizationJSONMap(optimization)
	return &optimization
}

func applyTileCacheOptimizationConfig(optimization *commonModels.OptimizationConfig, raw commonModels.JSONMap) {
	if optimization == nil || raw == nil {
		return
	}
	if version := stringFromConfig(raw["version"]); version != "" {
		optimization.Version = version
	}
	if attr, ok := asJSONMap(raw["attribute_pruning"]); ok {
		if enabled, exists := attr["enabled"].(bool); exists {
			optimization.AttributePruning.Enabled = enabled
		}
		if threshold := intFromTileCacheConfig(attr["zoom_threshold"], 0); threshold > 0 {
			optimization.AttributePruning.ZoomThreshold = threshold
		}
	}
	if thresholds, ok := asJSONMap(raw["tile_size_thresholds"]); ok {
		if maxSizeMB := floatFromTileCacheConfig(thresholds["max_size_mb"], 0); maxSizeMB > 0 {
			optimization.TileSizeThresholds.MaxSizeMB = maxSizeMB
		}
	}
	if extent, ok := asJSONMap(raw["extent_optimization"]); ok {
		if maxZoomExtent := intFromTileCacheConfig(extent["max_zoom_extent"], 0); maxZoomExtent > 0 {
			optimization.ExtentOptimization.MaxZoomExtent = maxZoomExtent
		}
		if baseExtent := intFromTileCacheConfig(extent["base_extent"], 0); baseExtent > 0 {
			optimization.ExtentOptimization.BaseExtent = baseExtent
		}
		if minExtent := intFromTileCacheConfig(extent["min_extent"], 0); minExtent > 0 {
			optimization.ExtentOptimization.MinExtent = minExtent
		}
	}
}

func optimizationJSONMap(optimization commonModels.OptimizationConfig) commonModels.JSONMap {
	return commonModels.JSONMap{
		"version": optimization.Version,
		"attribute_pruning": commonModels.JSONMap{
			"enabled":        optimization.AttributePruning.Enabled,
			"zoom_threshold": optimization.AttributePruning.ZoomThreshold,
		},
		"tile_size_thresholds": commonModels.JSONMap{
			"max_size_mb": optimization.TileSizeThresholds.MaxSizeMB,
		},
		"extent_optimization": commonModels.JSONMap{
			"max_zoom_extent": optimization.ExtentOptimization.MaxZoomExtent,
			"base_extent":     optimization.ExtentOptimization.BaseExtent,
			"min_extent":      optimization.ExtentOptimization.MinExtent,
		},
	}
}

type tileGenerationTarget struct {
	Schema                       string
	Table                        string
	GeomColumn                   string
	SRID                         int
	PrimaryKey                   string
	VectorMaterializedViewTarget bool
	TargetKind                   string
	OptimizationRecommended      bool
	OptimizationRecommendation   string
}

func (s *TileCacheTaskService) resolveTileGenerationTarget(
	ctx context.Context,
	tenantID *uint,
	engineID uint,
	schema, table, geomColumn string,
	sourceSRID int,
	primaryKey string,
) (tileGenerationTarget, error) {
	if sourceSRID <= 0 {
		return tileGenerationTarget{}, errors.New("tile cache task source_srid is required")
	}
	if s.tileTargetResolver == nil {
		if sourceSRID == spatial.SRIDWebMercator {
			return tileGenerationTarget{
				Schema:     schema,
				Table:      table,
				GeomColumn: geomColumn,
				SRID:       sourceSRID,
				PrimaryKey: primaryKey,
				TargetKind: RealtimeTileTargetKindSourceTable,
			}, nil
		}
		return tileGenerationTarget{
			Schema:                     schema,
			Table:                      table,
			GeomColumn:                 geomColumn,
			SRID:                       sourceSRID,
			PrimaryKey:                 primaryKey,
			TargetKind:                 RealtimeTileTargetKindSourceTable,
			OptimizationRecommended:    true,
			OptimizationRecommendation: "vector_materialized_view_generation is recommended before generating cache for non-3857 spatial data",
		}, nil
	}
	target, err := s.tileTargetResolver.ResolveRealtimeTileTarget(ctx, tenantID, engineID, schema, table, geomColumn, sourceSRID)
	if err != nil {
		return tileGenerationTarget{}, err
	}
	if target == nil {
		recommendOptimization := sourceSRID != spatial.SRIDWebMercator
		recommendation := ""
		if recommendOptimization {
			recommendation = "vector_materialized_view_generation is recommended before generating cache for non-3857 spatial data"
		}
		return tileGenerationTarget{
			Schema:                     schema,
			Table:                      table,
			GeomColumn:                 geomColumn,
			SRID:                       sourceSRID,
			PrimaryKey:                 primaryKey,
			TargetKind:                 RealtimeTileTargetKindSourceTable,
			OptimizationRecommended:    recommendOptimization,
			OptimizationRecommendation: recommendation,
		}, nil
	}
	targetPrimaryKey := primaryKey
	if !target.VectorMaterializedViewTarget {
		targetPrimaryKey = primaryKey
	} else {
		targetPrimaryKey = ""
	}
	return tileGenerationTarget{
		Schema:                       target.Schema,
		Table:                        target.Table,
		GeomColumn:                   target.GeomColumn,
		SRID:                         target.SRID,
		PrimaryKey:                   targetPrimaryKey,
		VectorMaterializedViewTarget: target.VectorMaterializedViewTarget,
		TargetKind:                   target.TargetKind,
		OptimizationRecommended:      target.OptimizationRecommended,
		OptimizationRecommendation:   target.OptimizationRecommendation,
	}, nil
}

func normalizeTileCacheTask(task *models.TileCacheTask) error {
	if task == nil {
		return errors.New("tile cache task is nil")
	}
	task.Name = strings.TrimSpace(task.Name)
	task.Description = strings.TrimSpace(task.Description)
	task.Schedule = strings.TrimSpace(task.Schedule)
	if task.Config == nil {
		task.Config = commonModels.JSONMap{}
	}
	if task.Name == "" {
		return errors.New("tile cache task name is required")
	}
	if len(task.Config) == 0 {
		return errors.New("tile cache task config is required")
	}
	if _, ok := task.Config["preparation"]; ok {
		return errors.New("tile cache task config.preparation has been removed; create a vector_materialized_view_generation task instead")
	}
	if _, err := normalizeTileCacheTaskTarget(task.Config); err != nil {
		return err
	}
	if task.Schedule == "" {
		task.NextRunAt = nil
		return nil
	}
	builder := commonScheduler.NewExpressionBuilder()
	if err := builder.Validate(task.Schedule); err != nil {
		return fmt.Errorf("invalid tile cache task schedule: %w", err)
	}
	nextRunAt, err := builder.NextRunTime(task.Schedule, tileCacheScheduleNow())
	if err != nil {
		return fmt.Errorf("calculate tile cache task next_run_at: %w", err)
	}
	task.NextRunAt = &nextRunAt
	return nil
}

func (s *TileCacheTaskService) ensureUniqueTileCacheTaskTarget(ctx context.Context, task *models.TileCacheTask, excludeTaskID uint) error {
	if s == nil || s.tileCacheRepo == nil || task == nil {
		return nil
	}
	identity, err := readTileCacheTaskTargetIdentity(task.Config)
	if err != nil {
		return err
	}
	tile, _ := asJSONMap(task.Config["tile"])
	format := strings.ToLower(stringFromConfig(tile["format"]))
	if format == "" {
		format = "mvt"
	}
	existing, err := s.tileCacheRepo.GetTaskByTargetFingerprintAndFormat(ctx, task.TenantID, identity.ItemFingerprint, format, excludeTaskID)
	if err != nil {
		return err
	}
	if existing != nil {
		return fmt.Errorf("该数据项已存在 %s 瓦片缓存任务：%s", format, existing.Name)
	}
	return nil
}

func tileCacheTaskSemanticIdentity(config commonModels.JSONMap) (tileCacheTaskTargetIdentity, string, error) {
	identity, err := readTileCacheTaskTargetIdentity(config)
	if err != nil {
		return tileCacheTaskTargetIdentity{}, "", err
	}
	tile, _ := asJSONMap(config["tile"])
	format := strings.ToLower(stringFromConfig(tile["format"]))
	if format == "" {
		format = "mvt"
	}
	return identity, format, nil
}

type tileCacheTaskTargetIdentity struct {
	EngineID        uint
	Schema          string
	Table           string
	ItemID          uint
	ItemFingerprint string
	Locator         string
	SourceKind      string
	FullName        string
}

func normalizeTileCacheTaskTarget(config commonModels.JSONMap) (tileCacheTaskTargetIdentity, error) {
	target, ok := asJSONMap(config["target"])
	if !ok {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target is required")
	}
	identity := tileCacheTaskTargetIdentity{
		EngineID:        uintFromConfig(target["source_engine_id"]),
		Schema:          stringFromConfig(target["schema"]),
		Table:           stringFromConfig(target["table"]),
		ItemID:          uintFromConfig(target["item_id"]),
		ItemFingerprint: stringFromConfig(target["item_fingerprint"]),
		Locator:         stringFromConfig(target["locator"]),
		SourceKind:      stringFromConfig(target["source_kind"]),
		FullName:        stringFromConfig(target["full_name"]),
	}
	if identity.EngineID == 0 || identity.Locator == "" {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target requires source_engine_id and locator")
	}
	locator, err := resourcetree.ParseURI(identity.Locator)
	if err != nil {
		return tileCacheTaskTargetIdentity{}, fmt.Errorf("tile cache task config.target.locator is invalid: %w", err)
	}
	if locator.EngineID != identity.EngineID {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.locator engine_id does not match source_engine_id")
	}
	if locator.ItemID != nil && identity.ItemID == 0 {
		identity.ItemID = *locator.ItemID
	}
	identity.SourceKind = string(locator.Type)
	identity.FullName = strings.TrimSpace(locator.FullName())
	if identity.FullName == "" {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.locator must point to an item path")
	}
	if locator.Type == resourcetree.TypeTable {
		if len(locator.Path) < 2 {
			return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.locator table path is invalid")
		}
		schema := locator.Path[len(locator.Path)-2]
		tableName := locator.Path[len(locator.Path)-1]
		if identity.Schema != "" && identity.Schema != schema {
			return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.schema does not match locator")
		}
		if identity.Table != "" && identity.Table != tableName {
			return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.table does not match locator")
		}
		identity.Schema = schema
		identity.Table = tableName
	}
	expectedFingerprint := commonModels.GenerateItemFingerprint(identity.EngineID, identity.FullName)
	if identity.ItemFingerprint != "" && identity.ItemFingerprint != expectedFingerprint {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.item_fingerprint does not match source locator")
	}
	identity.ItemFingerprint = expectedFingerprint

	normalizedTarget := commonModels.JSONMap{
		"source_engine_id": identity.EngineID,
		"item_fingerprint": identity.ItemFingerprint,
		"locator":          identity.Locator,
		"source_kind":      identity.SourceKind,
		"full_name":        identity.FullName,
	}
	if identity.ItemID > 0 {
		normalizedTarget["item_id"] = identity.ItemID
	}
	if identity.Schema != "" {
		normalizedTarget["schema"] = identity.Schema
	}
	if identity.Table != "" {
		normalizedTarget["table"] = identity.Table
	}
	config["target"] = normalizedTarget
	return identity, nil
}

func readTileCacheTaskTargetIdentity(config commonModels.JSONMap) (tileCacheTaskTargetIdentity, error) {
	target, ok := asJSONMap(config["target"])
	if !ok {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target is required")
	}
	identity := tileCacheTaskTargetIdentity{
		EngineID:        uintFromConfig(target["source_engine_id"]),
		Schema:          stringFromConfig(target["schema"]),
		Table:           stringFromConfig(target["table"]),
		ItemID:          uintFromConfig(target["item_id"]),
		ItemFingerprint: stringFromConfig(target["item_fingerprint"]),
		Locator:         stringFromConfig(target["locator"]),
		SourceKind:      stringFromConfig(target["source_kind"]),
		FullName:        stringFromConfig(target["full_name"]),
	}
	if identity.EngineID == 0 || identity.Locator == "" {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target requires source_engine_id and locator")
	}
	if identity.ItemFingerprint == "" {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.item_fingerprint is required")
	}
	locator, err := resourcetree.ParseURI(identity.Locator)
	if err != nil {
		return tileCacheTaskTargetIdentity{}, fmt.Errorf("tile cache task config.target.locator is invalid: %w", err)
	}
	if locator.EngineID != identity.EngineID {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.locator engine_id does not match source_engine_id")
	}
	if locator.ItemID != nil && identity.ItemID == 0 {
		identity.ItemID = *locator.ItemID
	}
	identity.SourceKind = string(locator.Type)
	identity.FullName = strings.TrimSpace(locator.FullName())
	if identity.FullName == "" {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.locator must point to an item path")
	}
	if locator.Type == resourcetree.TypeTable {
		if len(locator.Path) < 2 {
			return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.locator table path is invalid")
		}
		schema := locator.Path[len(locator.Path)-2]
		tableName := locator.Path[len(locator.Path)-1]
		if identity.Schema != "" && identity.Schema != schema {
			return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.schema does not match locator")
		}
		if identity.Table != "" && identity.Table != tableName {
			return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.table does not match locator")
		}
		identity.Schema = schema
		identity.Table = tableName
	}
	expectedFingerprint := commonModels.GenerateItemFingerprint(identity.EngineID, identity.FullName)
	if identity.ItemFingerprint != expectedFingerprint {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.item_fingerprint does not match source locator")
	}
	return identity, nil
}

func buildTileCacheStorageRef(tenantID uint, fingerprint string) string {
	return tilecache.ObjectPrefixStorageRef(tenantID, fingerprint)
}

func firstNonNil(values ...interface{}) interface{} {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func intFromTileCacheConfig(value interface{}, defaultValue int) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint:
		return int(v)
	case float64:
		return int(v)
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &parsed); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func floatFromTileCacheConfig(value interface{}, defaultValue float64) float64 {
	switch v := value.(type) {
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case uint:
		return float64(v)
	case float32:
		return float64(v)
	case float64:
		return v
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%f", &parsed); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func floatSliceFromConfig(value interface{}) ([]float64, bool) {
	switch v := value.(type) {
	case []float64:
		return v, len(v) == 4
	case []interface{}:
		out := make([]float64, 0, len(v))
		for _, item := range v {
			switch n := item.(type) {
			case float64:
				out = append(out, n)
			case int:
				out = append(out, float64(n))
			default:
				return nil, false
			}
		}
		return out, len(out) == 4
	default:
		return nil, false
	}
}

func tileCacheScheduleNow() time.Time {
	return time.Now()
}
