package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	commonScheduler "github.com/addp/common/scheduler"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/mvt"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/tilecache"
	"github.com/google/uuid"
)

const tileCacheTaskSchedulePollInterval = time.Minute

type TileCacheGenerator interface {
	GenerateMixed(ctx context.Context, cfg mvt.QuickViewConfig, progressTracker *mvt.ProgressTracker) (*mvt.GenerateResult, error)
}

type TileCacheCleaner interface {
	DeleteByStorageRef(ctx context.Context, storageRef string) error
}

type TileCacheTaskService struct {
	tileCacheRepo      *repository.TileCacheRepository
	taskExecRepo       *commonExecution.TaskExecutionRepository
	quickViewSvc       *QuickViewService
	tileGenerator      TileCacheGenerator
	tileCacheCleaner   TileCacheCleaner
	defaultConcurrency int
}

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

func (s *TileCacheTaskService) SetTileCacheCleaner(cleaner TileCacheCleaner) {
	s.tileCacheCleaner = cleaner
}

func (s *TileCacheTaskService) Create(ctx context.Context, task *models.TileCacheTask) error {
	if err := normalizeTileCacheTask(task); err != nil {
		return err
	}
	return s.tileCacheRepo.CreateTask(ctx, task)
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

func (s *TileCacheTaskService) Execute(ctx context.Context, taskID uint, tenantID uint, triggerType string, source string, parentExecutionID *string) (string, error) {
	task, err := s.tileCacheRepo.GetTask(ctx, taskID, tenantID)
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
	executionConfig := task.Config.Clone()
	if executionConfig == nil {
		executionConfig = commonModels.JSONMap{}
	}
	exec := &commonExecution.TaskExecution{
		ExecutionID:       executionID,
		TenantID:          int(tenantID),
		Module:            commonExecution.ModuleManager,
		TaskType:          commonExecution.TaskTypeTileCacheGeneration,
		Source:            normalizedSource,
		SourceTaskID:      commonExecution.NewSourceTaskIDFromUint(taskID),
		SourceTaskName:    &task.Name,
		ParentExecutionID: parentExecutionID,
		Status:            commonExecution.ExecutionStatusRunning,
		TriggerType:       normalizedTriggerType,
		ExecutionConfig:   executionConfig,
		StartedAt:         &now,
	}
	if err := s.taskExecRepo.Create(ctx, exec); err != nil {
		return "", err
	}
	if err := s.tileCacheRepo.UpdateTaskLastExecution(ctx, taskID, executionID, commonExecution.ExecutionStatusRunning, now); err != nil {
		return "", err
	}

	go s.runTileCacheGeneration(context.Background(), task, executionID, now)
	return executionID, nil
}

func (s *TileCacheTaskService) runTileCacheGeneration(ctx context.Context, task *models.TileCacheTask, executionID string, startedAt time.Time) {
	status := commonExecution.ExecutionStatusSuccess
	var errDetails commonModels.JSONMap
	metadata := commonModels.JSONMap{}
	completedAt := time.Now()
	resultReadyPersisted := false
	preserveReadyTileCache := false

	tileCache, _, cfg, preserveReadyTileCache, err := s.prepareExecutionTileCache(ctx, task, executionID)
	if err == nil && s.tileGenerator == nil {
		err = errors.New("tile cache generation executor is not connected")
	}
	if err == nil {
		result, genErr := s.tileGenerator.GenerateMixed(ctx, cfg, nil)
		if genErr != nil {
			err = genErr
		} else {
			completedAt = time.Now()
			status = commonExecution.ExecutionStatusSuccess
			metadata = commonModels.JSONMap{
				"tile_cache_id":      tileCache.ID,
				"actual_max_zoom":    result.ActualMaxZoom,
				"total_tiles":        result.TotalTiles,
				"cached_tiles":       result.CachedTiles,
				"generation_seconds": result.GenerationSec,
				"stop_reason":        result.StopReason,
			}
			updates := map[string]interface{}{
				"status":            models.TileCacheStatusReady,
				"storage_ref":       cfg.StorageRef,
				"last_execution_id": executionID,
				"min_zoom":          cfg.MinZoom,
				"max_zoom":          result.ActualMaxZoom,
				"error_message":     "",
				"config_hash":       cfg.ConfigHash,
			}
			if len(cfg.Extent) == 4 {
				extentJSON, _ := json.Marshal(cfg.Extent)
				updates["extent"] = extentJSON
				if cfg.ExtentSRID > 0 {
					updates["extent_srid"] = cfg.ExtentSRID
				}
			}
			if updateErr := s.tileCacheRepo.UpdateTileCacheFields(ctx, tileCache.ID, task.TenantID, updates); updateErr != nil {
				err = fmt.Errorf("update tile cache result state: %w", updateErr)
			} else {
				resultReadyPersisted = true
			}
		}
	}

	if err != nil {
		status = commonExecution.ExecutionStatusFailed
		errDetails = commonModels.JSONMap{"message": err.Error()}
		if tileCache != nil && !resultReadyPersisted && !preserveReadyTileCache {
			if updateErr := s.tileCacheRepo.UpdateTileCacheFields(ctx, tileCache.ID, task.TenantID, map[string]interface{}{
				"status":            models.TileCacheStatusFailed,
				"error_message":     err.Error(),
				"last_execution_id": executionID,
			}); updateErr != nil {
				errDetails["tile_cache_update_error"] = updateErr.Error()
			}
		}
	}

	completedAt = time.Now()
	durationMs := completedAt.Sub(startedAt).Milliseconds()
	if err := s.taskExecRepo.UpdateFields(ctx, executionID, int(task.TenantID), map[string]interface{}{
		"status":            status,
		"progress":          100,
		"metadata":          metadata,
		"error_details":     errDetails,
		"completed_at":      completedAt,
		"execution_time_ms": durationMs,
		"updated_at":        completedAt,
	}); err != nil {
		logger.L().Warn("更新瓦片缓存 execution 失败", "execution_id", executionID, "task_id", task.ID, "error", err)
	}
	if err := s.tileCacheRepo.UpdateTaskLastExecution(ctx, task.ID, executionID, status, completedAt); err != nil {
		logger.L().Warn("更新瓦片缓存任务最近执行状态失败", "execution_id", executionID, "task_id", task.ID, "error", err)
	}
}

func (s *TileCacheTaskService) prepareExecutionTileCache(ctx context.Context, task *models.TileCacheTask, executionID string) (*models.TileCache, commonModels.JSONMap, mvt.QuickViewConfig, bool, error) {
	execCfg := task.Config.Clone()
	if execCfg == nil {
		execCfg = commonModels.JSONMap{}
	}
	tile, _ := asJSONMap(execCfg["tile"])
	options, _ := asJSONMap(execCfg["options"])

	format := strings.ToLower(stringFromConfig(tile["format"]))
	if format == "" {
		format = "mvt"
	}
	if format != "mvt" {
		return nil, execCfg, mvt.QuickViewConfig{}, false, fmt.Errorf("unsupported tile format %q", format)
	}

	identity, err := readTileCacheTaskTargetIdentity(execCfg)
	if err != nil {
		return nil, execCfg, mvt.QuickViewConfig{}, false, err
	}
	engineID := identity.EngineID
	schema := identity.Schema
	table := identity.Table

	itemID := identity.ItemID
	var itemIDPtr *uint
	if itemID > 0 {
		itemIDPtr = &itemID
	}
	locator := identity.Locator
	minZoom := intFromTileCacheConfig(tile["min_zoom"], 0)
	maxZoom := intFromTileCacheConfig(tile["max_zoom"], 18)
	if maxZoom <= 0 || maxZoom < minZoom {
		return nil, execCfg, mvt.QuickViewConfig{}, false, errors.New("tile cache task config.tile.max_zoom is invalid")
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
	needsSpatialMeta := geomColumn == "" || primaryKey == "" || len(extent) == 0 || sourceSRID == 0
	if s.quickViewSvc != nil {
		var err error
		spatialMeta, err = s.quickViewSvc.GetSpatialMetadataFromMeta(ctx, task.TenantID, engineID, schema, table)
		if err != nil {
			return nil, execCfg, mvt.QuickViewConfig{}, false, err
		}
		if needsSpatialMeta && geomColumn == "" {
			geomColumn = spatialMeta.GeomColumn
		}
		if needsSpatialMeta && primaryKey == "" {
			primaryKey = spatialMeta.PrimaryKey
		}
		if needsSpatialMeta && len(extent) == 0 {
			extent = spatialMeta.Extent
			extentSRID = spatialMeta.ExtentSRID
		}
		if needsSpatialMeta && sourceSRID == 0 {
			sourceSRID = spatialMeta.SRID
		}
	}
	if geomColumn == "" {
		return nil, execCfg, mvt.QuickViewConfig{}, false, ErrQuickViewGeometryColumnNotFound
	}

	fingerprint := identity.ItemFingerprint
	cfgHash := tileCacheConfigHash(tileCacheConfigHashInput{
		TileMatrixSet:  stringFromConfig(tile["tile_matrix_set"]),
		MinZoom:        minZoom,
		MaxZoom:        maxZoom,
		SourceSRID:     sourceSRID,
		TargetSRID:     intFromTileCacheConfig(tile["target_srid"], 3857),
		ExtentSRID:     extentSRID,
		Extent:         extent,
		GeometryColumn: geomColumn,
		PrimaryKey:     primaryKey,
	})
	tileCache := &models.TileCache{
		TenantID:        task.TenantID,
		ItemFingerprint: fingerprint,
		ItemID:          itemIDPtr,
		Locator:         locator,
		TaskID:          &task.ID,
		LastExecutionID: &executionID,
		TileFormat:      format,
		Status:          models.TileCacheStatusGenerating,
		ConfigHash:      cfgHash,
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
	existingTileCache, err := s.tileCacheRepo.GetTileCacheByFingerprintAndConfig(ctx, task.TenantID, fingerprint, format, cfgHash)
	if err != nil {
		return nil, execCfg, mvt.QuickViewConfig{}, false, err
	}
	preserveReadyTileCache := existingTileCache != nil && existingTileCache.Status == models.TileCacheStatusReady
	if existingTileCache != nil {
		tileCache = existingTileCache
		if !preserveReadyTileCache {
			initialUpdates["status"] = models.TileCacheStatusGenerating
			initialUpdates["storage_ref"] = ""
			initialUpdates["error_message"] = ""
		}
		if err := s.tileCacheRepo.UpdateTileCacheFields(ctx, tileCache.ID, task.TenantID, initialUpdates); err != nil {
			return nil, execCfg, mvt.QuickViewConfig{}, false, err
		}
	} else {
		if err := s.tileCacheRepo.CreateTileCache(ctx, tileCache); err != nil {
			return nil, execCfg, mvt.QuickViewConfig{}, false, err
		}
	}

	storageRef := buildTileCacheStorageRef(task.TenantID, fingerprint, cfgHash)
	cfg := mvt.QuickViewConfig{
		EngineID:    engineID,
		TenantID:    task.TenantID,
		Schema:      schema,
		Table:       table,
		GeomColumn:  geomColumn,
		SRID:        sourceSRID,
		ExtentSRID:  extentSRID,
		PrimaryKey:  primaryKey,
		Extent:      extent,
		MinZoom:     minZoom,
		MaxZoom:     maxZoom,
		Concurrency: intFromTileCacheConfig(execCfg["concurrency"], s.defaultConcurrency),
		Fingerprint: fingerprint,
		StorageRef:  storageRef,
		ConfigHash:  cfgHash,
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = s.defaultConcurrency
	}
	return tileCache, execCfg, cfg, preserveReadyTileCache, nil
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

type tileCacheTaskTargetIdentity struct {
	EngineID        uint
	Schema          string
	Table           string
	ItemID          uint
	ItemFingerprint string
	Locator         string
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
	}
	if identity.EngineID == 0 || identity.Schema == "" || identity.Table == "" {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target requires source_engine_id, schema and table")
	}
	expectedFingerprint := spatialItemFingerprint(identity.EngineID, identity.Schema, identity.Table)
	if expectedFingerprint == "" {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target cannot calculate item_fingerprint")
	}
	if identity.ItemFingerprint != "" && identity.ItemFingerprint != expectedFingerprint {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.item_fingerprint does not match source identity")
	}
	identity.ItemFingerprint = expectedFingerprint
	if identity.Locator == "" {
		identity.Locator = tableLocator(identity.EngineID, identity.Schema, identity.Table)
	}
	if identity.Locator == "" {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.locator is required")
	}

	normalizedTarget := commonModels.JSONMap{
		"source_engine_id": identity.EngineID,
		"schema":           identity.Schema,
		"table":            identity.Table,
		"item_fingerprint": identity.ItemFingerprint,
		"locator":          identity.Locator,
	}
	if identity.ItemID > 0 {
		normalizedTarget["item_id"] = identity.ItemID
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
	}
	if identity.EngineID == 0 || identity.Schema == "" || identity.Table == "" {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target requires source_engine_id, schema and table")
	}
	if identity.ItemFingerprint == "" {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.item_fingerprint is required")
	}
	expectedFingerprint := spatialItemFingerprint(identity.EngineID, identity.Schema, identity.Table)
	if identity.ItemFingerprint != expectedFingerprint {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.item_fingerprint does not match source identity")
	}
	if identity.Locator == "" {
		return tileCacheTaskTargetIdentity{}, errors.New("tile cache task config.target.locator is required")
	}
	return identity, nil
}

func buildTileCacheStorageRef(tenantID uint, fingerprint string, configHash string) string {
	return tilecache.ObjectPrefixStorageRef(tenantID, fingerprint, configHash)
}

type tileCacheConfigHashInput struct {
	TileMatrixSet  string    `json:"tile_matrix_set,omitempty"`
	MinZoom        int       `json:"min_zoom"`
	MaxZoom        int       `json:"max_zoom"`
	SourceSRID     int       `json:"source_srid"`
	TargetSRID     int       `json:"target_srid"`
	ExtentSRID     int       `json:"extent_srid,omitempty"`
	Extent         []float64 `json:"extent,omitempty"`
	GeometryColumn string    `json:"geometry_column"`
	PrimaryKey     string    `json:"primary_key,omitempty"`
}

func tileCacheConfigHash(input tileCacheConfigHashInput) string {
	input.TileMatrixSet = strings.TrimSpace(input.TileMatrixSet)
	input.GeometryColumn = strings.TrimSpace(input.GeometryColumn)
	input.PrimaryKey = strings.TrimSpace(input.PrimaryKey)
	if input.TileMatrixSet == "" {
		input.TileMatrixSet = "WebMercatorQuad"
	}
	if input.TargetSRID == 0 {
		input.TargetSRID = 3857
	}
	data, _ := json.Marshal(input)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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
