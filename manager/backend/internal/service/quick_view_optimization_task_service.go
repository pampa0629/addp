package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

type QuickViewOptimizationDBProvider interface {
	GetPostGISDB(ctx context.Context, tenantID *uint, engineID uint) (*sql.DB, error)
}

type QuickViewOptimizationTaskService struct {
	repo          *repository.QuickViewOptimizationRepository
	quickViewRepo *repository.QuickViewRepository
	taskExecRepo  *commonExecution.TaskExecutionRepository
	dbProvider    QuickViewOptimizationDBProvider
}

type quickViewOptimizationIdentity struct {
	EngineID        uint
	Schema          string
	Table           string
	ItemID          uint
	ItemFingerprint string
	Locator         string
}

type quickViewOptimizationGeometry struct {
	GeometryColumn string
	SourceSRID     int
	TargetSRID     int
}

type quickViewOptimizationOptions struct {
	TargetKind        string
	TargetSchema      string
	IncludeSourceKey  bool
	Attributes        []string
	AnalyzeAfterBuild bool
}

type quickViewOptimizationExecutionConfig struct {
	Identity quickViewOptimizationIdentity
	Geometry quickViewOptimizationGeometry
	Options  quickViewOptimizationOptions
}

type quickViewOptimizationBuildPlan struct {
	TargetTable    string
	StagingTable   string
	OldTargetTable string
	IndexName      string
	CreateSQL      string
	CreateIndexSQL string
	AnalyzeSQL     string
}

func NewQuickViewOptimizationTaskService(
	repo *repository.QuickViewOptimizationRepository,
	taskExecRepo *commonExecution.TaskExecutionRepository,
) *QuickViewOptimizationTaskService {
	return &QuickViewOptimizationTaskService{
		repo:         repo,
		taskExecRepo: taskExecRepo,
	}
}

func (s *QuickViewOptimizationTaskService) SetDBProvider(provider QuickViewOptimizationDBProvider) {
	s.dbProvider = provider
}

func (s *QuickViewOptimizationTaskService) SetQuickViewRepository(repo *repository.QuickViewRepository) {
	s.quickViewRepo = repo
}

func (s *QuickViewOptimizationTaskService) Create(ctx context.Context, task *models.QuickViewOptimizationTask) error {
	if err := normalizeQuickViewOptimizationTask(task); err != nil {
		return err
	}
	return s.repo.CreateTask(ctx, task)
}

func (s *QuickViewOptimizationTaskService) GetByID(ctx context.Context, id uint, tenantID uint) (*models.QuickViewOptimizationTask, error) {
	return s.repo.GetTask(ctx, id, tenantID)
}

func (s *QuickViewOptimizationTaskService) List(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.QuickViewOptimizationTask, int64, error) {
	return s.repo.ListTasks(ctx, tenantID, page, pageSize)
}

func (s *QuickViewOptimizationTaskService) Update(ctx context.Context, task *models.QuickViewOptimizationTask) error {
	if err := normalizeQuickViewOptimizationTask(task); err != nil {
		return err
	}
	return s.repo.UpdateTask(ctx, task)
}

func (s *QuickViewOptimizationTaskService) Delete(ctx context.Context, id uint, tenantID uint) error {
	return s.repo.DeleteTask(ctx, id, tenantID)
}

func (s *QuickViewOptimizationTaskService) ListResults(ctx context.Context, filter repository.QuickViewOptimizationFilter) ([]*models.QuickViewOptimization, int64, error) {
	return s.repo.ListResults(ctx, filter)
}

func (s *QuickViewOptimizationTaskService) GetResult(ctx context.Context, id uint, tenantID uint) (*models.QuickViewOptimization, error) {
	return s.repo.GetResult(ctx, id, tenantID)
}

func (s *QuickViewOptimizationTaskService) GetLatestReadyByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.QuickViewOptimization, error) {
	return s.repo.GetLatestReadyByFingerprint(ctx, tenantID, itemFingerprint)
}

func (s *QuickViewOptimizationTaskService) DeleteResult(ctx context.Context, id uint, tenantID uint) error {
	result, err := s.repo.GetResult(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if result == nil {
		return commonapi.ErrNotFound
	}
	if result.TargetKind != models.QuickViewOptimizationTargetKindSourceSchemaMaterializedView {
		return fmt.Errorf("unsupported quick view optimization target_kind %q", result.TargetKind)
	}
	if s.dbProvider == nil {
		return errors.New("quick view optimization db provider is required")
	}
	tid := tenantID
	db, err := s.dbProvider.GetPostGISDB(ctx, &tid, result.SourceEngineID)
	if err != nil {
		return err
	}
	dropSQL := "DROP MATERIALIZED VIEW IF EXISTS " + spatial.QualifiedPostGISTable(result.TargetSchema, result.TargetTable)
	if _, err := db.ExecContext(ctx, dropSQL); err != nil {
		s.recordQuickViewOptimizationCleanupFailure(ctx, result, err)
		return fmt.Errorf("drop quick view optimization target: %w", err)
	}
	return s.repo.DeleteResult(ctx, id, tenantID)
}

func (s *QuickViewOptimizationTaskService) DeleteResultsForSourceTable(ctx context.Context, tenantID uint, engineID uint, schema string, table string) error {
	if s.repo == nil {
		return errors.New("quick view optimization repository is required")
	}
	schema = strings.TrimSpace(schema)
	table = strings.TrimSpace(table)
	if tenantID == 0 || engineID == 0 || schema == "" || table == "" {
		return errors.New("quick view optimization source table identity is required")
	}
	results, err := s.repo.ListResultsBySourceTable(ctx, tenantID, engineID, schema, table)
	if err != nil {
		return err
	}
	for _, result := range results {
		if result == nil {
			continue
		}
		if result.Status == models.QuickViewOptimizationStatusDeleted {
			continue
		}
		if result.TargetKind != models.QuickViewOptimizationTargetKindSourceSchemaMaterializedView {
			return fmt.Errorf("unsupported quick view optimization target_kind %q", result.TargetKind)
		}
		if err := s.DeleteResult(ctx, result.ID, tenantID); err != nil {
			return err
		}
		if s.quickViewRepo != nil && strings.TrimSpace(result.ItemFingerprint) != "" {
			if err := s.quickViewRepo.DeleteByTenantAndFingerprint(ctx, tenantID, result.ItemFingerprint); err != nil {
				return err
			}
		}
	}
	if s.quickViewRepo != nil {
		sourceFingerprint := spatialItemFingerprint(engineID, schema, table)
		if sourceFingerprint != "" {
			if err := s.quickViewRepo.DeleteByTenantAndFingerprint(ctx, tenantID, sourceFingerprint); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *QuickViewOptimizationTaskService) recordQuickViewOptimizationCleanupFailure(ctx context.Context, result *models.QuickViewOptimization, cleanupErr error) {
	if result == nil || cleanupErr == nil {
		return
	}
	metadata := result.Metadata.Clone()
	if metadata == nil {
		metadata = commonModels.JSONMap{}
	}
	metadata["cleanup_error"] = cleanupErr.Error()
	metadata["cleanup_failed_at"] = time.Now().Format(time.RFC3339Nano)
	if err := s.repo.UpdateResultFields(ctx, result.ID, result.TenantID, map[string]interface{}{
		"status":        models.QuickViewOptimizationStatusFailed,
		"error_message": fmt.Sprintf("cleanup quick view optimization target failed: %v", cleanupErr),
		"metadata":      metadata,
	}); err != nil {
		logger.L().Warn("记录快显性能优化结果清理失败状态失败", "result_id", result.ID, "error", err)
	}
}

func (s *QuickViewOptimizationTaskService) Execute(ctx context.Context, taskID uint, tenantID uint, triggerType string, source string, parentExecutionID *string) (string, error) {
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
	currentStep := "构建快显性能优化目标"
	executionConfig := task.Config.Clone()
	if executionConfig == nil {
		executionConfig = commonModels.JSONMap{}
	}
	exec := &commonExecution.TaskExecution{
		ExecutionID:       executionID,
		TenantID:          int(tenantID),
		Module:            commonExecution.ModuleManager,
		TaskType:          commonExecution.TaskTypeQuickViewOptimization,
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
	if err := s.repo.UpdateTaskLastExecution(ctx, taskID, executionID, commonExecution.ExecutionStatusRunning, now); err != nil {
		return "", err
	}

	go s.runQuickViewOptimization(context.Background(), task, executionID, now)
	return executionID, nil
}

func (s *QuickViewOptimizationTaskService) runQuickViewOptimization(ctx context.Context, task *models.QuickViewOptimizationTask, executionID string, startedAt time.Time) {
	status := commonExecution.ExecutionStatusSuccess
	metadata := commonModels.JSONMap{}
	var errDetails commonModels.JSONMap

	result, execCfg, plan, err := s.prepareOptimizationResult(ctx, task, executionID)
	if err == nil {
		if s.dbProvider == nil {
			err = errors.New("quick view optimization db provider is required")
		} else {
			tid := task.TenantID
			var db *sql.DB
			db, err = s.dbProvider.GetPostGISDB(ctx, &tid, execCfg.Identity.EngineID)
			if err == nil {
				metadata, err = executeQuickViewOptimizationPlan(ctx, db, execCfg, plan)
			}
		}
	}

	completedAt := time.Now()
	if err != nil {
		status = commonExecution.ExecutionStatusFailed
		errDetails = commonModels.JSONMap{"message": err.Error()}
		if result != nil {
			fields := map[string]interface{}{
				"status":            models.QuickViewOptimizationStatusFailed,
				"error_message":     err.Error(),
				"last_execution_id": executionID,
			}
			if updateErr := s.repo.UpdateResultFields(ctx, result.ID, task.TenantID, fields); updateErr != nil {
				errDetails["quick_view_optimization_update_error"] = updateErr.Error()
			}
		}
	} else if result != nil {
		renderExtentSRID := spatial.SRIDWGS84
		fields := map[string]interface{}{
			"status":             models.QuickViewOptimizationStatusReady,
			"error_message":      "",
			"last_execution_id":  executionID,
			"metadata":           metadata,
			"render_extent_srid": renderExtentSRID,
		}
		if extent, ok := floatSliceFromConfig(metadata["render_extent"]); ok {
			extentJSON, _ := json.Marshal(extent)
			fields["render_extent"] = extentJSON
		}
		if rowCount := int64FromConfig(metadata["row_count_estimate"], 0); rowCount >= 0 {
			fields["row_count_estimate"] = rowCount
		}
		if err := s.repo.UpdateResultFields(ctx, result.ID, task.TenantID, fields); err != nil {
			status = commonExecution.ExecutionStatusFailed
			errDetails = commonModels.JSONMap{"message": fmt.Sprintf("update quick view optimization result: %v", err)}
		}
	}

	durationMs := completedAt.Sub(startedAt).Milliseconds()
	progress := 100
	if status != commonExecution.ExecutionStatusSuccess {
		progress = 0
	}
	if err := s.taskExecRepo.UpdateFields(ctx, executionID, int(task.TenantID), map[string]interface{}{
		"status":            status,
		"progress":          progress,
		"metadata":          metadata,
		"error_details":     errDetails,
		"completed_at":      completedAt,
		"execution_time_ms": durationMs,
		"updated_at":        completedAt,
	}); err != nil {
		logger.L().Warn("更新快显性能优化 execution 失败", "execution_id", executionID, "task_id", task.ID, "error", err)
	}
	if err := s.repo.UpdateTaskLastExecution(ctx, task.ID, executionID, status, completedAt); err != nil {
		logger.L().Warn("更新快显性能优化任务最近执行状态失败", "execution_id", executionID, "task_id", task.ID, "error", err)
	}
}

func (s *QuickViewOptimizationTaskService) prepareOptimizationResult(ctx context.Context, task *models.QuickViewOptimizationTask, executionID string) (*models.QuickViewOptimization, quickViewOptimizationExecutionConfig, quickViewOptimizationBuildPlan, error) {
	execCfg, err := readQuickViewOptimizationExecutionConfig(task.Config)
	if err != nil {
		return nil, execCfg, quickViewOptimizationBuildPlan{}, err
	}
	targetTable := quickViewOptimizationTargetTable(task.TenantID, execCfg.Identity.ItemFingerprint, execCfg.Geometry.GeometryColumn, execCfg.Geometry.TargetSRID)
	nowSuffix := fmt.Sprintf("%d", time.Now().UnixNano())
	plan := buildQuickViewOptimizationPlan(execCfg, targetTable, nowSuffix)

	itemID := execCfg.Identity.ItemID
	var itemIDPtr *uint
	if itemID > 0 {
		itemIDPtr = &itemID
	}
	sourceSnapshot := commonModels.JSONMap{
		"source_engine_id":       execCfg.Identity.EngineID,
		"source_schema":          execCfg.Identity.Schema,
		"source_table":           execCfg.Identity.Table,
		"source_geometry_column": execCfg.Geometry.GeometryColumn,
		"source_srid":            execCfg.Geometry.SourceSRID,
		"target_srid":            execCfg.Geometry.TargetSRID,
	}
	metadata := commonModels.JSONMap{
		"target_kind":         execCfg.Options.TargetKind,
		"target_schema":       execCfg.Options.TargetSchema,
		"target_table":        plan.TargetTable,
		"staging_table":       plan.StagingTable,
		"index_name":          plan.IndexName,
		"attributes":          execCfg.Options.Attributes,
		"include_source_key":  execCfg.Options.IncludeSourceKey,
		"analyze_after_build": execCfg.Options.AnalyzeAfterBuild,
	}

	existing, err := s.repo.GetCurrentResult(ctx, task.TenantID, execCfg.Identity.ItemFingerprint, execCfg.Geometry.GeometryColumn, execCfg.Geometry.TargetSRID)
	if err != nil {
		return nil, execCfg, plan, err
	}
	if existing != nil {
		if err := s.repo.UpdateResultFields(ctx, existing.ID, task.TenantID, map[string]interface{}{
			"item_id":                     itemIDPtr,
			"locator":                     execCfg.Identity.Locator,
			"task_id":                     &task.ID,
			"last_execution_id":           executionID,
			"source_engine_id":            execCfg.Identity.EngineID,
			"source_schema":               execCfg.Identity.Schema,
			"source_table":                execCfg.Identity.Table,
			"source_geometry_column":      execCfg.Geometry.GeometryColumn,
			"source_srid":                 execCfg.Geometry.SourceSRID,
			"target_srid":                 execCfg.Geometry.TargetSRID,
			"target_kind":                 execCfg.Options.TargetKind,
			"target_schema":               execCfg.Options.TargetSchema,
			"target_table":                plan.TargetTable,
			"target_geometry_column":      models.QuickViewOptimizationTargetGeometryColumn,
			"status":                      models.QuickViewOptimizationStatusBuilding,
			"source_fingerprint_snapshot": sourceSnapshot,
			"metadata":                    metadata,
			"error_message":               "",
		}); err != nil {
			return nil, execCfg, plan, err
		}
		existing.TargetTable = plan.TargetTable
		return existing, execCfg, plan, nil
	}

	result := &models.QuickViewOptimization{
		TenantID:                  task.TenantID,
		ItemFingerprint:           execCfg.Identity.ItemFingerprint,
		ItemID:                    itemIDPtr,
		Locator:                   execCfg.Identity.Locator,
		TaskID:                    &task.ID,
		LastExecutionID:           &executionID,
		SourceEngineID:            execCfg.Identity.EngineID,
		SourceSchema:              execCfg.Identity.Schema,
		SourceTable:               execCfg.Identity.Table,
		SourceGeometryColumn:      execCfg.Geometry.GeometryColumn,
		SourceSRID:                execCfg.Geometry.SourceSRID,
		TargetSRID:                execCfg.Geometry.TargetSRID,
		TargetKind:                execCfg.Options.TargetKind,
		TargetSchema:              execCfg.Options.TargetSchema,
		TargetTable:               plan.TargetTable,
		TargetGeometryColumn:      models.QuickViewOptimizationTargetGeometryColumn,
		Status:                    models.QuickViewOptimizationStatusBuilding,
		SourceFingerprintSnapshot: sourceSnapshot,
		Metadata:                  metadata,
		CreatedBy:                 task.CreatedBy,
	}
	if err := s.repo.CreateResult(ctx, result); err != nil {
		return nil, execCfg, plan, err
	}
	return result, execCfg, plan, nil
}

func executeQuickViewOptimizationPlan(ctx context.Context, db *sql.DB, execCfg quickViewOptimizationExecutionConfig, plan quickViewOptimizationBuildPlan) (commonModels.JSONMap, error) {
	if db == nil {
		return nil, errors.New("postgis db is required")
	}
	if err := ensureQuickViewOptimizationCreatePrivilege(ctx, db, execCfg.Options.TargetSchema); err != nil {
		return nil, err
	}
	primaryKeys, _ := queryPostGISPrimaryKeys(ctx, db, execCfg.Identity.Schema, execCfg.Identity.Table)
	plan.CreateSQL = buildQuickViewOptimizationCreateSQL(execCfg, plan.StagingTable, primaryKeys)
	sourceHasIndex, sourceIndexErr := hasValidGiSTIndex(ctx, db, execCfg.Identity.Schema, execCfg.Identity.Table, execCfg.Geometry.GeometryColumn)

	if _, err := db.ExecContext(ctx, "DROP MATERIALIZED VIEW IF EXISTS "+spatial.QualifiedPostGISTable(execCfg.Options.TargetSchema, plan.StagingTable)); err != nil {
		return nil, fmt.Errorf("drop stale staging materialized view: %w", err)
	}
	stagingCreated := false
	swapped := false
	defer func() {
		if !stagingCreated || swapped {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, cleanupErr := db.ExecContext(cleanupCtx, "DROP MATERIALIZED VIEW IF EXISTS "+spatial.QualifiedPostGISTable(execCfg.Options.TargetSchema, plan.StagingTable)); cleanupErr != nil {
			logger.L().Warn("清理失败的快显性能优化 staging 物化视图失败",
				"schema", execCfg.Options.TargetSchema,
				"staging_table", plan.StagingTable,
				"error", cleanupErr)
		}
	}()
	if _, err := db.ExecContext(ctx, plan.CreateSQL); err != nil {
		return nil, fmt.Errorf("create 3857 materialized view: %w", err)
	}
	stagingCreated = true
	if _, err := db.ExecContext(ctx, plan.CreateIndexSQL); err != nil {
		return nil, fmt.Errorf("create geom_3857 GiST index: %w", err)
	}
	analyzeExecuted := false
	if execCfg.Options.AnalyzeAfterBuild {
		if _, err := db.ExecContext(ctx, plan.AnalyzeSQL); err != nil {
			return nil, fmt.Errorf("analyze quick view optimization target: %w", err)
		}
		analyzeExecuted = true
	}
	if err := swapQuickViewOptimizationTarget(ctx, db, execCfg.Options.TargetSchema, plan.TargetTable, plan.StagingTable, plan.OldTargetTable); err != nil {
		return nil, err
	}
	swapped = true

	rowCount, _ := queryPostGISRowCountEstimate(ctx, db, execCfg.Options.TargetSchema, plan.TargetTable)
	renderExtent, _ := queryPostGISRenderExtentWGS84(ctx, db, execCfg.Options.TargetSchema, plan.TargetTable, models.QuickViewOptimizationTargetGeometryColumn)
	metadata := commonModels.JSONMap{
		"target_kind":                execCfg.Options.TargetKind,
		"target_schema":              execCfg.Options.TargetSchema,
		"target_table":               plan.TargetTable,
		"target_geometry_column":     models.QuickViewOptimizationTargetGeometryColumn,
		"staging_table":              plan.StagingTable,
		"index_name":                 plan.IndexName,
		"source_primary_keys":        primaryKeys,
		"source_identity":            quickViewOptimizationSourceIdentity(primaryKeys),
		"source_has_spatial_index":   sourceHasIndex,
		"source_spatial_index_error": "",
		"attributes":                 execCfg.Options.Attributes,
		"include_source_key":         execCfg.Options.IncludeSourceKey,
		"analyze_after_build":        execCfg.Options.AnalyzeAfterBuild,
		"analyze_executed":           analyzeExecuted,
		"row_count_estimate":         rowCount,
		"render_extent":              renderExtent,
		"render_extent_srid":         spatial.SRIDWGS84,
	}
	if sourceIndexErr != nil {
		metadata["source_spatial_index_error"] = sourceIndexErr.Error()
	}
	return metadata, nil
}

func normalizeQuickViewOptimizationTask(task *models.QuickViewOptimizationTask) error {
	if task == nil {
		return errors.New("quick view optimization task is nil")
	}
	task.Name = strings.TrimSpace(task.Name)
	task.Description = strings.TrimSpace(task.Description)
	task.Schedule = strings.TrimSpace(task.Schedule)
	if task.Config == nil {
		task.Config = commonModels.JSONMap{}
	}
	if task.Name == "" {
		return errors.New("quick view optimization task name is required")
	}
	if len(task.Config) == 0 {
		return errors.New("quick view optimization task config is required")
	}
	if task.Schedule != "" || task.NextRunAt != nil {
		return errors.New("quick view optimization task does not support schedule")
	}
	_, err := normalizeQuickViewOptimizationTaskConfig(task.Config)
	return err
}

func normalizeQuickViewOptimizationTaskConfig(config commonModels.JSONMap) (quickViewOptimizationExecutionConfig, error) {
	identity, err := normalizeQuickViewOptimizationTarget(config)
	if err != nil {
		return quickViewOptimizationExecutionConfig{}, err
	}
	geometry, err := normalizeQuickViewOptimizationGeometry(config)
	if err != nil {
		return quickViewOptimizationExecutionConfig{}, err
	}
	options, err := normalizeQuickViewOptimizationOptions(config, identity.Schema, geometry.GeometryColumn)
	if err != nil {
		return quickViewOptimizationExecutionConfig{}, err
	}
	return quickViewOptimizationExecutionConfig{Identity: identity, Geometry: geometry, Options: options}, nil
}

func readQuickViewOptimizationExecutionConfig(config commonModels.JSONMap) (quickViewOptimizationExecutionConfig, error) {
	return normalizeQuickViewOptimizationTaskConfig(config)
}

func normalizeQuickViewOptimizationTarget(config commonModels.JSONMap) (quickViewOptimizationIdentity, error) {
	target, ok := asJSONMap(config["target"])
	if !ok {
		return quickViewOptimizationIdentity{}, errors.New("quick view optimization config.target is required")
	}
	identity := quickViewOptimizationIdentity{
		EngineID:        uintFromConfig(target["source_engine_id"]),
		Schema:          stringFromConfig(target["schema"]),
		Table:           stringFromConfig(target["table"]),
		ItemID:          uintFromConfig(target["item_id"]),
		ItemFingerprint: stringFromConfig(target["item_fingerprint"]),
		Locator:         stringFromConfig(target["locator"]),
	}
	if identity.EngineID == 0 || identity.Schema == "" || identity.Table == "" {
		return quickViewOptimizationIdentity{}, errors.New("quick view optimization config.target requires source_engine_id, schema and table")
	}
	expectedFingerprint := spatialItemFingerprint(identity.EngineID, identity.Schema, identity.Table)
	if expectedFingerprint == "" {
		return quickViewOptimizationIdentity{}, errors.New("quick view optimization config.target cannot calculate item_fingerprint")
	}
	if identity.ItemFingerprint != "" && identity.ItemFingerprint != expectedFingerprint {
		return quickViewOptimizationIdentity{}, errors.New("quick view optimization config.target.item_fingerprint does not match source identity")
	}
	identity.ItemFingerprint = expectedFingerprint
	if identity.Locator == "" {
		identity.Locator = tableLocator(identity.EngineID, identity.Schema, identity.Table)
	}
	if identity.Locator == "" {
		return quickViewOptimizationIdentity{}, errors.New("quick view optimization config.target.locator is required")
	}
	normalized := commonModels.JSONMap{
		"source_engine_id": identity.EngineID,
		"schema":           identity.Schema,
		"table":            identity.Table,
		"item_fingerprint": identity.ItemFingerprint,
		"locator":          identity.Locator,
	}
	if identity.ItemID > 0 {
		normalized["item_id"] = identity.ItemID
	}
	config["target"] = normalized
	return identity, nil
}

func normalizeQuickViewOptimizationGeometry(config commonModels.JSONMap) (quickViewOptimizationGeometry, error) {
	geometry, ok := asJSONMap(config["geometry"])
	if !ok {
		return quickViewOptimizationGeometry{}, errors.New("quick view optimization config.geometry is required")
	}
	cfg := quickViewOptimizationGeometry{
		GeometryColumn: stringFromConfig(geometry["geometry_column"]),
		SourceSRID:     intFromTileCacheConfig(geometry["source_srid"], 0),
		TargetSRID:     intFromTileCacheConfig(geometry["target_srid"], spatial.SRIDWebMercator),
	}
	if cfg.GeometryColumn == "" {
		return quickViewOptimizationGeometry{}, errors.New("quick view optimization config.geometry.geometry_column is required")
	}
	if cfg.SourceSRID <= 0 {
		return quickViewOptimizationGeometry{}, errors.New("quick view optimization config.geometry.source_srid is required")
	}
	if cfg.SourceSRID == spatial.SRIDWebMercator {
		return quickViewOptimizationGeometry{}, errors.New("quick view optimization config.geometry.source_srid=3857 is already optimized by source 3857")
	}
	if cfg.TargetSRID != spatial.SRIDWebMercator {
		return quickViewOptimizationGeometry{}, errors.New("quick view optimization only supports target_srid=3857")
	}
	config["geometry"] = commonModels.JSONMap{
		"geometry_column": cfg.GeometryColumn,
		"source_srid":     cfg.SourceSRID,
		"target_srid":     cfg.TargetSRID,
	}
	return cfg, nil
}

func normalizeQuickViewOptimizationOptions(config commonModels.JSONMap, sourceSchema, sourceGeometryColumn string) (quickViewOptimizationOptions, error) {
	optimization, _ := asJSONMap(config["optimization"])
	storage, _ := asJSONMap(config["storage"])
	targetKind := stringFromConfig(optimization["target_kind"])
	if targetKind == "" {
		targetKind = models.QuickViewOptimizationTargetKindSourceSchemaMaterializedView
	}
	if targetKind != models.QuickViewOptimizationTargetKindSourceSchemaMaterializedView {
		return quickViewOptimizationOptions{}, errors.New("quick view optimization only supports source_schema_materialized_view")
	}
	targetSchema := stringFromConfig(storage["target_schema"])
	if targetSchema == "" {
		targetSchema = sourceSchema
	}
	if targetSchema != sourceSchema {
		return quickViewOptimizationOptions{}, errors.New("quick view optimization requires storage.target_schema to equal source schema")
	}
	attributes, err := stringSliceFromConfig(optimization["attributes"])
	if err != nil {
		return quickViewOptimizationOptions{}, err
	}
	if err := validateQuickViewOptimizationAttributes(attributes, sourceGeometryColumn); err != nil {
		return quickViewOptimizationOptions{}, err
	}
	opts := quickViewOptimizationOptions{
		TargetKind:        targetKind,
		TargetSchema:      targetSchema,
		IncludeSourceKey:  boolFromConfig(optimization["include_source_key"], true),
		Attributes:        attributes,
		AnalyzeAfterBuild: boolFromConfig(optimization["analyze_after_build"], true),
	}
	config["optimization"] = commonModels.JSONMap{
		"target_kind":         opts.TargetKind,
		"include_source_key":  opts.IncludeSourceKey,
		"attributes":          opts.Attributes,
		"analyze_after_build": opts.AnalyzeAfterBuild,
	}
	config["storage"] = commonModels.JSONMap{"target_schema": opts.TargetSchema}
	return opts, nil
}

func validateQuickViewOptimizationAttributes(attributes []string, sourceGeometryColumn string) error {
	sourceGeometryColumn = strings.TrimSpace(sourceGeometryColumn)
	seen := map[string]struct{}{}
	for _, attr := range attributes {
		attr = strings.TrimSpace(attr)
		if attr == "" {
			continue
		}
		normalized := strings.ToLower(attr)
		if _, exists := seen[normalized]; exists {
			return fmt.Errorf("quick view optimization config.optimization.attributes contains duplicate column %q", attr)
		}
		seen[normalized] = struct{}{}
		if strings.EqualFold(attr, "source_row_id") {
			return fmt.Errorf("quick view optimization config.optimization.attributes must not include reserved column %q", attr)
		}
		if strings.EqualFold(attr, sourceGeometryColumn) || strings.EqualFold(attr, models.QuickViewOptimizationTargetGeometryColumn) {
			return fmt.Errorf("quick view optimization config.optimization.attributes must not include geometry column %q", attr)
		}
	}
	return nil
}

func buildQuickViewOptimizationPlan(execCfg quickViewOptimizationExecutionConfig, targetTable, suffix string) quickViewOptimizationBuildPlan {
	stagingTable := targetTable + "_staging_" + suffix
	oldTable := targetTable + "_old_" + suffix
	if len(stagingTable) > 63 {
		stagingTable = targetTable[:minInt(len(targetTable), 40)] + "_stg_" + suffix[len(suffix)-minInt(len(suffix), 16):]
	}
	if len(oldTable) > 63 {
		oldTable = targetTable[:minInt(len(targetTable), 40)] + "_old_" + suffix[len(suffix)-minInt(len(suffix), 16):]
	}
	indexName := quickViewOptimizationIndexName(stagingTable)
	createSQL := buildQuickViewOptimizationCreateSQL(execCfg, stagingTable, nil)
	return quickViewOptimizationBuildPlan{
		TargetTable:    targetTable,
		StagingTable:   stagingTable,
		OldTargetTable: oldTable,
		IndexName:      indexName,
		CreateSQL:      createSQL,
		CreateIndexSQL: spatial.BuildPostGISCreateGISTIndexSQL(execCfg.Options.TargetSchema, stagingTable, indexName, models.QuickViewOptimizationTargetGeometryColumn, false),
		AnalyzeSQL:     spatial.BuildPostGISAnalyzeSQL(execCfg.Options.TargetSchema, stagingTable),
	}
}

func buildQuickViewOptimizationCreateSQL(execCfg quickViewOptimizationExecutionConfig, stagingTable string, sourceKeyColumns []string) string {
	selectExpressions := quickViewOptimizationSelectExpressions(execCfg.Options.IncludeSourceKey, sourceKeyColumns, execCfg.Options.Attributes)
	selectExpressions = append(selectExpressions, fmt.Sprintf(
		"ST_Transform(%s, 3857) AS %s",
		spatial.QuotePostGISIdentifier(execCfg.Geometry.GeometryColumn),
		spatial.QuotePostGISIdentifier(models.QuickViewOptimizationTargetGeometryColumn),
	))
	return fmt.Sprintf(`
		CREATE MATERIALIZED VIEW %s AS
		SELECT
			%s
		FROM %s
		WHERE %s IS NOT NULL
	`,
		spatial.QualifiedPostGISTable(execCfg.Options.TargetSchema, stagingTable),
		strings.Join(selectExpressions, ",\n\t\t\t"),
		spatial.QualifiedPostGISTable(execCfg.Identity.Schema, execCfg.Identity.Table),
		spatial.QuotePostGISIdentifier(execCfg.Geometry.GeometryColumn),
	)
}

func quickViewOptimizationSelectExpressions(includeSourceKey bool, sourceKeyColumns []string, attributes []string) []string {
	parts := make([]string, 0, len(attributes)+1)
	if includeSourceKey {
		parts = append(parts, quickViewOptimizationSourceRowIDExpression(sourceKeyColumns))
	}
	for _, attr := range attributes {
		attr = strings.TrimSpace(attr)
		if attr == "" {
			continue
		}
		parts = append(parts, spatial.QuotePostGISIdentifier(attr))
	}
	return parts
}

func quickViewOptimizationSourceRowIDExpression(sourceKeyColumns []string) string {
	normalized := make([]string, 0, len(sourceKeyColumns))
	for _, column := range sourceKeyColumns {
		column = strings.TrimSpace(column)
		if column != "" {
			normalized = append(normalized, column)
		}
	}
	switch len(normalized) {
	case 0:
		return "(row_number() OVER ())::text AS source_row_id"
	case 1:
		return fmt.Sprintf("(%s)::text AS source_row_id", spatial.QuotePostGISIdentifier(normalized[0]))
	default:
		args := make([]string, 0, len(normalized)*2)
		for _, column := range normalized {
			args = append(args, quotePostGISLiteral(column), spatial.QuotePostGISIdentifier(column))
		}
		return fmt.Sprintf("jsonb_build_object(%s)::text AS source_row_id", strings.Join(args, ", "))
	}
}

func quickViewOptimizationSourceIdentity(sourceKeyColumns []string) string {
	if len(sourceKeyColumns) == 0 {
		return "reduced"
	}
	return "primary_key"
}

func quickViewOptimizationTargetTable(tenantID uint, itemFingerprint, geometryColumn string, targetSRID int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s:%d", tenantID, itemFingerprint, geometryColumn, targetSRID)))
	return "addp_qvo_" + hex.EncodeToString(sum[:])[:24]
}

func quickViewOptimizationIndexName(table string) string {
	base := "idx_" + table + "_" + models.QuickViewOptimizationTargetGeometryColumn + "_gist"
	if len(base) <= 63 {
		return base
	}
	sum := sha256.Sum256([]byte(base))
	return "idx_qvo_" + hex.EncodeToString(sum[:])[:24] + "_gist"
}

func ensureQuickViewOptimizationCreatePrivilege(ctx context.Context, db *sql.DB, schema string) error {
	var ok bool
	if err := db.QueryRowContext(ctx, "SELECT has_schema_privilege(current_user, $1, 'CREATE')", schema).Scan(&ok); err != nil {
		return fmt.Errorf("check source schema create privilege: %w", err)
	}
	if !ok {
		return fmt.Errorf("missing CREATE privilege on source schema %q for quick view optimization target", schema)
	}
	return nil
}

func swapQuickViewOptimizationTarget(ctx context.Context, db *sql.DB, schema, targetTable, stagingTable, oldTable string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	exists, err := materializedViewExists(ctx, db, schema, targetTable)
	if err != nil {
		return err
	}
	if exists {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			"ALTER MATERIALIZED VIEW %s RENAME TO %s",
			spatial.QualifiedPostGISTable(schema, targetTable),
			spatial.QuotePostGISIdentifier(oldTable),
		)); err != nil {
			return fmt.Errorf("rename existing quick view optimization target: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		"ALTER MATERIALIZED VIEW %s RENAME TO %s",
		spatial.QualifiedPostGISTable(schema, stagingTable),
		spatial.QuotePostGISIdentifier(targetTable),
	)); err != nil {
		return fmt.Errorf("rename staging quick view optimization target: %w", err)
	}
	if exists {
		if _, err := tx.ExecContext(ctx, "DROP MATERIALIZED VIEW IF EXISTS "+spatial.QualifiedPostGISTable(schema, oldTable)); err != nil {
			return fmt.Errorf("drop old quick view optimization target: %w", err)
		}
	}
	return tx.Commit()
}

func queryPostGISPrimaryKeys(ctx context.Context, db *sql.DB, schema, table string) ([]string, error) {
	const query = `
		SELECT a.attname
		FROM pg_index i
		JOIN unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
		WHERE i.indrelid = to_regclass(quote_ident($1) || '.' || quote_ident($2))
		  AND i.indisprimary
		ORDER BY k.ord
	`
	rows, err := db.QueryContext(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return nil, err
		}
		if strings.TrimSpace(column) != "" {
			columns = append(columns, column)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}

func materializedViewExists(ctx context.Context, db *sql.DB, schema, view string) (bool, error) {
	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM pg_matviews
			WHERE schemaname = $1 AND matviewname = $2
		)
	`
	var exists bool
	if err := db.QueryRowContext(ctx, query, schema, view).Scan(&exists); err != nil {
		return false, fmt.Errorf("query materialized view existence failed: %w", err)
	}
	return exists, nil
}

func queryPostGISRowCountEstimate(ctx context.Context, db *sql.DB, schema, table string) (int64, error) {
	const query = `
		SELECT COALESCE(c.reltuples::bigint, 0)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2
	`
	var count int64
	if err := db.QueryRowContext(ctx, query, schema, table).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func queryPostGISRenderExtentWGS84(ctx context.Context, db *sql.DB, schema, table, geomColumn string) ([]float64, error) {
	const query = `
		WITH e AS (
			SELECT ST_Transform(ST_SetSRID(ST_EstimatedExtent($1, $2, $3)::geometry, 3857), 4326) AS geom
		)
		SELECT ST_XMin(geom), ST_YMin(geom), ST_XMax(geom), ST_YMax(geom)
		FROM e
		WHERE geom IS NOT NULL
	`
	var minX, minY, maxX, maxY sql.NullFloat64
	if err := db.QueryRowContext(ctx, query, schema, table, geomColumn).Scan(&minX, &minY, &maxX, &maxY); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if !minX.Valid || !minY.Valid || !maxX.Valid || !maxY.Valid {
		return nil, nil
	}
	return []float64{minX.Float64, minY.Float64, maxX.Float64, maxY.Float64}, nil
}

func stringSliceFromConfig(value interface{}) ([]string, error) {
	if value == nil {
		return []string{}, nil
	}
	switch v := value.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out, nil
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, errors.New("quick view optimization config.optimization.attributes must be string array")
			}
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	default:
		return nil, errors.New("quick view optimization config.optimization.attributes must be string array")
	}
}

func quotePostGISLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func int64FromConfig(value interface{}, defaultValue int64) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case uint:
		return int64(v)
	case float64:
		return int64(v)
	}
	return defaultValue
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
