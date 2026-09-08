package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	engineselection "github.com/addp/common/engine/selection"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/exportartifact"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"
)

type CleanupService struct {
	redis               *redis.Client
	metaClient          *commonClient.MetaClient
	systemClient        *commonClient.SystemServiceClient
	taskExecRepo        *commonExecution.TaskExecutionRepository
	previewStateRepo    *repository.PreviewStateRepository
	tileCacheSvc        *TileCacheTaskService
	embeddingRepo       *repository.EmbeddingRepository
	optimizationSvc     *VectorMaterializedViewTaskService
	taskDefinitionRepo  *repository.CleanupTaskDefinitionRepository
	managedArtifactRepo *repository.CleanupManagedArtifactRepository
	rasterCOGSvc        *RasterCOGTaskService
	model3DTilesSvc     *Model3DTilesTaskService
	model3DGLBSvc       *Model3DGLBTaskService
	gaussianSplatSvc    *GaussianSplatKSplatTaskService
	pointCloudCOPCSvc   *PointCloudCOPCTaskService
	pptxPDFSvc          *PPTXPDFTaskService
	exportRepo          *repository.ExportSessionRepository
	minioClient         *minio.Client
	minioBucket         string
	exportCleanup       ExportCleanupOptions
	log                 *slog.Logger
	stopCh              chan struct{}
}

type ExportCleanupOptions = exportartifact.CleanupOptions

type ManagerCleanupStats struct {
	PreviewStates            int      `json:"preview_states"`
	TileCaches               int      `json:"vector_tile_caches"`
	Embeddings               int      `json:"embeddings"`
	VectorMaterializedViews  int      `json:"vector_materialized_view_generations"`
	ManagedArtifacts         int      `json:"managed_quick_view_artifacts"`
	ExportSessions           int      `json:"export_sessions,omitempty"`
	DeletedPhysicalArtifacts int      `json:"deleted_physical_artifacts,omitempty"`
	FreedBytes               int64    `json:"freed_bytes,omitempty"`
	MarkedMissingSource      int      `json:"marked_missing_source,omitempty"`
	SkippedExternalTargets   int      `json:"skipped_external_targets,omitempty"`
	AbandonedExternal        int      `json:"abandoned_external,omitempty"`
	TaskDefinitions          int      `json:"task_definitions,omitempty"`
	DisabledTaskDefinitions  int      `json:"disabled_task_definitions,omitempty"`
	DeletedTaskDefinitions   int      `json:"deleted_task_definitions,omitempty"`
	Errors                   []string `json:"errors,omitempty"`
}

func NewCleanupService(
	redisClient *redis.Client,
	metaClient *commonClient.MetaClient,
	systemClient *commonClient.SystemServiceClient,
	taskExecRepo *commonExecution.TaskExecutionRepository,
	previewStateRepo *repository.PreviewStateRepository,
	tileCacheSvc *TileCacheTaskService,
	embeddingRepo *repository.EmbeddingRepository,
	optimizationSvc *VectorMaterializedViewTaskService,
	taskDefinitionRepo *repository.CleanupTaskDefinitionRepository,
	managedArtifactRepo *repository.CleanupManagedArtifactRepository,
	rasterCOGSvc *RasterCOGTaskService,
	model3DTilesSvc *Model3DTilesTaskService,
	model3DGLBSvc *Model3DGLBTaskService,
	gaussianSplatSvc *GaussianSplatKSplatTaskService,
	pointCloudCOPCSvc *PointCloudCOPCTaskService,
	pptxPDFSvc *PPTXPDFTaskService,
	exportRepo *repository.ExportSessionRepository,
	minioClient *minio.Client,
	minioBucket string,
	exportCleanup ExportCleanupOptions,
) *CleanupService {
	exportCleanup = exportartifact.NormalizeCleanupOptions(exportCleanup)
	return &CleanupService{
		redis:               redisClient,
		metaClient:          metaClient,
		systemClient:        systemClient,
		taskExecRepo:        taskExecRepo,
		previewStateRepo:    previewStateRepo,
		tileCacheSvc:        tileCacheSvc,
		embeddingRepo:       embeddingRepo,
		optimizationSvc:     optimizationSvc,
		taskDefinitionRepo:  taskDefinitionRepo,
		managedArtifactRepo: managedArtifactRepo,
		rasterCOGSvc:        rasterCOGSvc,
		model3DTilesSvc:     model3DTilesSvc,
		model3DGLBSvc:       model3DGLBSvc,
		gaussianSplatSvc:    gaussianSplatSvc,
		pointCloudCOPCSvc:   pointCloudCOPCSvc,
		pptxPDFSvc:          pptxPDFSvc,
		exportRepo:          exportRepo,
		minioClient:         minioClient,
		minioBucket:         strings.Trim(minioBucket, "/"),
		exportCleanup:       exportCleanup,
		log:                 logger.With("component", "manager_cleanup_service"),
		stopCh:              make(chan struct{}),
	}
}

func (s *CleanupService) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if s.redis != nil {
		go s.consumeCleanupRequests(ctx)
		s.log.Info("Manager 资源回收事件订阅已启动")
	}
	if s.exportRepo != nil && s.minioClient != nil && s.minioBucket != "" {
		go s.runExportSessionCleanup(ctx)
		s.log.Info("Manager 导出暂存清理已启动")
	}
	return nil
}

func (s *CleanupService) Stop() {
	if s == nil || s.stopCh == nil {
		return
	}
	close(s.stopCh)
}

func (s *CleanupService) runExportSessionCleanup(ctx context.Context) {
	s.cleanupExportSessionsOnce(ctx)
	ticker := time.NewTicker(s.exportCleanup.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanupExportSessionsOnce(ctx)
		}
	}
}

func (s *CleanupService) cleanupExportSessionsOnce(ctx context.Context) {
	if s == nil || s.exportRepo == nil || s.minioClient == nil || s.minioBucket == "" {
		return
	}
	result, err := exportartifact.CleanupExpiredOnce(ctx, s.exportRepo, s.minioClient, s.minioBucket, s.exportCleanup, time.Now())
	if err != nil {
		s.log.Warn("清理导出暂存失败", "error", err)
		return
	}
	if result.MarkedExpired > 0 || result.DeletedSessions > 0 {
		s.log.Info("已清理导出暂存", "marked_expired", result.MarkedExpired, "sessions", result.DeletedSessions, "objects", result.DeletedObjects)
	}
}

func (s *CleanupService) consumeCleanupRequests(ctx context.Context) {
	groupName := "manager-cleanup-consumer"
	consumerName := "manager-backend"
	_ = s.redis.XGroupCreateMkStream(ctx, events.EventCleanupRequest, groupName, "$").Err()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		default:
			streams, err := s.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group:    groupName,
				Consumer: consumerName,
				Streams:  []string{events.EventCleanupRequest, ">"},
				Count:    1,
				Block:    5 * time.Second,
			}).Result()
			if err != nil {
				if err != redis.Nil {
					s.log.Error("读取资源回收请求失败", "error", err)
				}
				continue
			}
			for _, stream := range streams {
				for _, message := range stream.Messages {
					s.handleCleanupRequest(ctx, message)
					_ = s.redis.XAck(ctx, events.EventCleanupRequest, groupName, message.ID).Err()
				}
			}
		}
	}
}

func (s *CleanupService) handleCleanupRequest(ctx context.Context, message redis.XMessage) {
	event, err := events.ParseCleanupRequest(message.Values)
	if err != nil {
		s.log.Error("解析资源回收请求失败", "error", err, "message_id", message.ID)
		return
	}
	if !events.CleanupExpectedForModule(event.ExpectedModules, events.ModuleManager) {
		return
	}

	result := events.CleanupResultData{
		Module:      events.ModuleManager,
		Action:      event.Action,
		TenantID:    event.TenantID,
		TaskID:      event.TaskID,
		CleanupMode: event.CleanupMode,
		TriggerType: event.TriggerType,
		Timestamp:   time.Now(),
	}

	exec, startedAt, execErr := s.createExecutorExecution(ctx, event)
	if execErr != nil {
		s.log.Error("创建 Manager 资源回收执行记录失败", "error", execErr, "task_id", event.TaskID)
	}
	defer func() {
		if exec != nil {
			s.finishExecutorExecution(ctx, exec.ExecutionID, event.TenantID, startedAt, result)
		}
		s.writeResult(ctx, event.TaskID, result)
	}()

	switch event.Action {
	case events.CleanupActionScan:
		stats, err := s.ScanReclaimCandidates(ctx, event.TenantID, event.Context)
		if err != nil {
			result.Status = events.CleanupResultFailed
			result.Errors = []string{err.Error()}
			result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
			return
		}
		result.Status = events.CleanupResultSuccess
		result.Statistics = managerCleanupStatsToMap(stats)
		result.Summary = managerScanSummary(stats)
		if event.CauseEvent == events.CleanupCauseEngineDeleting {
			impact, err := s.managerEngineDeletionImpact(ctx, event.TenantID, event.Context)
			if err != nil {
				result.Status = events.CleanupResultFailed
				result.Errors = []string{err.Error()}
				result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
				return
			}
			result.Impact = &impact
		}
	case events.CleanupActionExecute:
		executeContext := cloneCleanupContext(event.Context)
		executeContext["requested_by"] = event.RequestedBy
		stats, err := s.ExecuteCleanup(ctx, event.TenantID, event.CleanupMode, executeContext)
		if err != nil {
			result.Status = events.CleanupResultFailed
			result.Errors = []string{err.Error()}
			result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
			return
		}
		if len(stats.Errors) > 0 {
			result.Status = events.CleanupResultPartialSuccess
			result.Errors = stats.Errors
		} else {
			result.Status = events.CleanupResultSuccess
		}
		result.Statistics = managerCleanupStatsToMap(stats)
		result.Summary = managerExecuteSummary(stats)
	default:
		result.Status = events.CleanupResultFailed
		result.Errors = []string{"unknown resource reclaim action: " + event.Action}
		result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
	}
}

func (s *CleanupService) ScanReclaimCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (*ManagerCleanupStats, error) {
	stats := &ManagerCleanupStats{}
	if tenantID == 0 {
		return stats, errors.New("manager resource reclaim requires tenant_id")
	}
	if s.previewStateRepo != nil {
		items, err := s.previewStateRepo.ListPreviewStates(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		stats.PreviewStates = len(s.filterMissingPreviewStates(ctx, tenantID, items, cleanupContext))
	}
	if s.tileCacheSvc != nil && s.tileCacheSvc.tileCacheRepo != nil {
		items, err := s.tileCacheSvc.tileCacheRepo.ListAllTileCaches(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		stats.TileCaches = len(s.filterMissingTileCaches(ctx, tenantID, items, cleanupContext))
	}
	if s.embeddingRepo != nil {
		items, err := s.embeddingRepo.ListAllEmbeddings(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		stats.Embeddings = len(s.filterMissingEmbeddings(ctx, tenantID, items, cleanupContext))
	}
	if s.optimizationSvc != nil && s.optimizationSvc.repo != nil {
		items, err := s.optimizationSvc.repo.ListAllResults(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		stats.VectorMaterializedViews = len(s.filterMissingVectorMaterializedViews(ctx, tenantID, items, cleanupContext))
	}
	managedArtifacts, err := s.managedArtifactCleanupCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	stats.ManagedArtifacts = len(managedArtifacts)
	for _, artifact := range managedArtifacts {
		if artifact.SizeBytes > 0 {
			stats.FreedBytes += artifact.SizeBytes
		}
	}
	taskCandidates, err := s.taskDefinitionCleanupCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	stats.TaskDefinitions = len(taskCandidates)
	return stats, nil
}

func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}) (*ManagerCleanupStats, error) {
	stats := &ManagerCleanupStats{}
	if tenantID == 0 {
		return stats, errors.New("manager resource reclaim requires tenant_id")
	}
	switch cleanupMode {
	case events.CleanupModeLogical, events.CleanupModePhysical:
	default:
		return stats, fmt.Errorf("unsupported cleanup_mode: %s", cleanupMode)
	}
	if uintFromCleanupContext(cleanupContext, "engine_id") > 0 {
		impact, err := s.managerEngineDeletionImpact(ctx, tenantID, cleanupContext)
		if err != nil {
			return nil, err
		}
		if impact.Summary.Running > 0 {
			stats.Errors = append(stats.Errors, "manager resources are still running")
			return stats, nil
		}
	}

	if s.previewStateRepo != nil {
		items, err := s.previewStateRepo.ListPreviewStates(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		for _, item := range s.filterMissingPreviewStates(ctx, tenantID, items, cleanupContext) {
			if err := s.previewStateRepo.DeleteByTenantAndFingerprint(ctx, tenantID, item.ItemFingerprint); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("delete preview_state %d: %v", item.ID, err))
				continue
			}
			stats.PreviewStates++
			stats.MarkedMissingSource++
		}
	}

	if s.tileCacheSvc != nil && s.tileCacheSvc.tileCacheRepo != nil {
		items, err := s.tileCacheSvc.tileCacheRepo.ListAllTileCaches(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		for _, item := range s.filterMissingTileCaches(ctx, tenantID, items, cleanupContext) {
			if cleanupMode == events.CleanupModePhysical {
				if err := s.tileCacheSvc.DeleteTileCache(ctx, item.ID, tenantID); err != nil {
					stats.Errors = append(stats.Errors, fmt.Sprintf("delete vector_tile_cache %d: %v", item.ID, err))
					continue
				}
				stats.DeletedPhysicalArtifacts++
			} else if err := s.tileCacheSvc.tileCacheRepo.UpdateTileCacheFields(ctx, item.ID, tenantID, map[string]interface{}{
				"status":        models.TileCacheStatusDeleted,
				"error_message": "resource reclaim logical cleanup: missing source",
			}); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("mark vector_tile_cache %d: %v", item.ID, err))
				continue
			}
			stats.TileCaches++
			stats.MarkedMissingSource++
		}
	}

	if s.embeddingRepo != nil {
		items, err := s.embeddingRepo.ListAllEmbeddings(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		for _, item := range s.filterMissingEmbeddings(ctx, tenantID, items, cleanupContext) {
			if cleanupMode == events.CleanupModePhysical {
				if err := s.embeddingRepo.DeleteEmbedding(ctx, tenantID, item.ID); err != nil {
					stats.Errors = append(stats.Errors, fmt.Sprintf("delete embedding %d: %v", item.ID, err))
					continue
				}
				stats.DeletedPhysicalArtifacts++
			} else if err := s.embeddingRepo.MarkEmbeddingMissingSource(ctx, tenantID, item.ID, "resource reclaim logical cleanup: missing source"); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("mark embedding %d: %v", item.ID, err))
				continue
			}
			stats.Embeddings++
			stats.MarkedMissingSource++
		}
	}

	if s.optimizationSvc != nil && s.optimizationSvc.repo != nil {
		items, err := s.optimizationSvc.repo.ListAllResults(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		for _, item := range s.filterMissingVectorMaterializedViews(ctx, tenantID, items, cleanupContext) {
			if item.TargetKind != models.VectorMaterializedViewTargetKindSourceSchemaMaterializedView {
				stats.SkippedExternalTargets++
				continue
			}
			if cleanupMode == events.CleanupModePhysical {
				if externalArtifactPolicy(cleanupContext) == commonModels.ExternalArtifactPolicyAbandon {
					if err := s.abandonVectorMaterializedView(ctx, item, cleanupContext); err != nil {
						stats.Errors = append(stats.Errors, fmt.Sprintf("abandon vector_materialized_view_generation %d: %v", item.ID, err))
						continue
					}
					stats.AbandonedExternal++
					stats.VectorMaterializedViews++
					stats.MarkedMissingSource++
					continue
				}
				if err := s.optimizationSvc.DeleteResult(ctx, item.ID, tenantID); err != nil {
					stats.Errors = append(stats.Errors, fmt.Sprintf("delete vector_materialized_view_generation %d: %v", item.ID, err))
					continue
				}
				stats.DeletedPhysicalArtifacts++
			} else if err := s.optimizationSvc.repo.MarkResultStale(ctx, item.ID, tenantID, "resource reclaim logical cleanup: missing source"); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("mark vector_materialized_view_generation %d: %v", item.ID, err))
				continue
			}
			stats.VectorMaterializedViews++
			stats.MarkedMissingSource++
		}
	}
	managedArtifacts, err := s.managedArtifactCleanupCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	for _, artifact := range managedArtifacts {
		if cleanupMode == events.CleanupModePhysical {
			if err := s.deleteManagedArtifact(ctx, artifact); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("delete %s artifact %d: %v", artifact.TaskType, artifact.ID, err))
				continue
			}
			stats.DeletedPhysicalArtifacts++
			if artifact.SizeBytes > 0 {
				stats.FreedBytes += artifact.SizeBytes
			}
		} else if err := s.managedArtifactRepo.MarkMissingSource(ctx, artifact); err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("mark %s artifact %d: %v", artifact.TaskType, artifact.ID, err))
			continue
		}
		stats.ManagedArtifacts++
		stats.MarkedMissingSource++
	}
	if err := s.cleanupTaskDefinitions(ctx, tenantID, cleanupMode, cleanupContext, stats); err != nil {
		return nil, err
	}
	return stats, nil
}

func (s *CleanupService) cleanupTaskDefinitions(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}, stats *ManagerCleanupStats) error {
	if stats == nil || s.taskDefinitionRepo == nil {
		return nil
	}
	candidates, err := s.taskDefinitionCleanupCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return err
	}
	taskCleanupMode := cleanupMode
	if uintFromCleanupContext(cleanupContext, "engine_id") > 0 {
		taskCleanupMode = events.CleanupModeLogical
	}
	for _, task := range candidates {
		if taskCleanupMode == events.CleanupModePhysical {
			if err := s.taskDefinitionRepo.HardDelete(ctx, task); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("delete %s task %d: %v", task.TaskType, task.ID, err))
				continue
			}
			stats.DeletedTaskDefinitions++
			continue
		}
		if !task.Enabled {
			continue
		}
		if err := s.taskDefinitionRepo.Disable(ctx, task, task.CleanupReason); err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("disable %s task %d: %v", task.TaskType, task.ID, err))
			continue
		}
		stats.DisabledTaskDefinitions++
	}
	return nil
}

func (s *CleanupService) managerEngineDeletionImpact(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (events.CleanupImpactData, error) {
	if uintFromCleanupContext(cleanupContext, "engine_id") == 0 {
		return events.CleanupImpactData{}, errors.New("manager engine deletion assessment requires engine_id")
	}
	items := make([]events.CleanupImpactItem, 0)
	appendImpact := func(stableRef, disposition string) {
		items = append(items, events.CleanupImpactItem{StableRef: stableRef, Disposition: disposition})
	}
	if s.previewStateRepo != nil {
		values, err := s.previewStateRepo.ListPreviewStates(ctx, tenantID)
		if err != nil {
			return events.CleanupImpactData{}, err
		}
		for _, item := range s.filterMissingPreviewStates(ctx, tenantID, values, cleanupContext) {
			appendImpact(fmt.Sprintf("manager_preview_state:%d", item.ID), events.CleanupImpactWillDelete)
		}
	}
	if s.tileCacheSvc != nil && s.tileCacheSvc.tileCacheRepo != nil {
		values, err := s.tileCacheSvc.tileCacheRepo.ListAllTileCaches(ctx, tenantID)
		if err != nil {
			return events.CleanupImpactData{}, err
		}
		for _, item := range s.filterMissingTileCaches(ctx, tenantID, values, cleanupContext) {
			stableRef := fmt.Sprintf("manager_vector_tile_cache:%d", item.ID)
			appendImpact(stableRef, events.CleanupImpactWillDelete)
			if item.Status == models.TileCacheStatusGenerating {
				appendImpact(stableRef, events.CleanupImpactRunning)
			}
		}
	}
	if s.embeddingRepo != nil {
		values, err := s.embeddingRepo.ListAllEmbeddings(ctx, tenantID)
		if err != nil {
			return events.CleanupImpactData{}, err
		}
		for _, item := range s.filterMissingEmbeddings(ctx, tenantID, values, cleanupContext) {
			appendImpact(fmt.Sprintf("manager_embedding:%d", item.ID), events.CleanupImpactWillDelete)
		}
	}
	if s.optimizationSvc != nil && s.optimizationSvc.repo != nil {
		values, err := s.optimizationSvc.repo.ListAllResults(ctx, tenantID)
		if err != nil {
			return events.CleanupImpactData{}, err
		}
		for _, item := range s.filterMissingVectorMaterializedViews(ctx, tenantID, values, cleanupContext) {
			stableRef := fmt.Sprintf("manager_vector_materialized_view:%d", item.ID)
			appendImpact(stableRef, events.CleanupImpactExternalArtifact)
			if item.Status == models.VectorMaterializedViewStatusBuilding {
				appendImpact(stableRef, events.CleanupImpactRunning)
			}
		}
	}
	artifacts, err := s.managedArtifactCleanupCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return events.CleanupImpactData{}, err
	}
	for _, artifact := range artifacts {
		stableRef := fmt.Sprintf("manager_%s_artifact:%d", artifact.TaskType, artifact.ID)
		appendImpact(stableRef, events.CleanupImpactWillDelete)
		if cleanupArtifactRunning(artifact.Status) {
			appendImpact(stableRef, events.CleanupImpactRunning)
		}
	}
	tasks, err := s.taskDefinitionCleanupCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return events.CleanupImpactData{}, err
	}
	for _, task := range tasks {
		stableRef := fmt.Sprintf("manager_%s_task:%d", task.TaskType, task.ID)
		appendImpact(stableRef, events.CleanupImpactWillDisable)
		if cleanupExecutionRunning(task.LastExecutionStatus) {
			appendImpact(stableRef, events.CleanupImpactRunning)
		}
	}
	return events.BuildCleanupImpactData(items, "/manager")
}

func cleanupExecutionRunning(status *string) bool {
	if status == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(*status)) {
	case commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning:
		return true
	default:
		return false
	}
}

func cleanupArtifactRunning(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "building", "generating", commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning:
		return true
	default:
		return false
	}
}

func (s *CleanupService) filterMissingPreviewStates(ctx context.Context, tenantID uint, items []*models.PreviewState, cleanupContext map[string]interface{}) []*models.PreviewState {
	out := make([]*models.PreviewState, 0)
	for _, item := range items {
		if item == nil {
			continue
		}
		if !s.matchesCleanupContext(item.Locator, item.ItemFingerprint, 0, cleanupContext) {
			continue
		}
		if s.sourceExistsForCleanup(ctx, tenantID, item.Locator, 0, cleanupContext) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (s *CleanupService) filterMissingTileCaches(ctx context.Context, tenantID uint, items []*models.TileCache, cleanupContext map[string]interface{}) []*models.TileCache {
	out := make([]*models.TileCache, 0)
	for _, item := range items {
		if item == nil {
			continue
		}
		itemID := uintPtrValue(item.ItemID)
		if !s.matchesCleanupContext(item.Locator, item.ItemFingerprint, itemID, cleanupContext) {
			continue
		}
		if s.sourceExistsForCleanup(ctx, tenantID, item.Locator, itemID, cleanupContext) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (s *CleanupService) filterMissingEmbeddings(ctx context.Context, tenantID uint, items []*models.Embedding, cleanupContext map[string]interface{}) []*models.Embedding {
	out := make([]*models.Embedding, 0)
	for _, item := range items {
		if item == nil {
			continue
		}
		if !s.matchesCleanupContext(item.Locator, item.ItemFingerprint, item.ItemID, cleanupContext) {
			continue
		}
		if s.sourceExistsForCleanup(ctx, tenantID, item.Locator, item.ItemID, cleanupContext) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (s *CleanupService) filterMissingVectorMaterializedViews(ctx context.Context, tenantID uint, items []*models.VectorMaterializedView, cleanupContext map[string]interface{}) []*models.VectorMaterializedView {
	out := make([]*models.VectorMaterializedView, 0)
	for _, item := range items {
		if item == nil {
			continue
		}
		if item.Status == models.VectorMaterializedViewStatusAbandonedExternal {
			continue
		}
		itemID := uintPtrValue(item.ItemID)
		if !s.matchesCleanupContext(item.Locator, item.ItemFingerprint, itemID, cleanupContext) {
			continue
		}
		if uintFromCleanupContext(cleanupContext, "engine_id") != item.SourceEngineID && s.sourceExistsForCleanup(ctx, tenantID, item.Locator, itemID, cleanupContext) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (s *CleanupService) taskDefinitionCleanupCandidates(
	ctx context.Context,
	tenantID uint,
	cleanupContext map[string]interface{},
) ([]repository.CleanupTaskDefinition, error) {
	if s.taskDefinitionRepo == nil {
		return nil, nil
	}
	definitions, err := s.taskDefinitionRepo.List(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	validEngineIDs, err := s.validCleanupEngineIDs(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	candidates := make([]repository.CleanupTaskDefinition, 0)
	for _, definition := range definitions {
		for _, target := range cleanupTaskTargetsFromDefinition(definition) {
			if !s.matchesCleanupTaskTarget(target, cleanupContext) {
				continue
			}
			reason := s.taskTargetMissingReason(ctx, tenantID, target, cleanupContext, validEngineIDs)
			if reason == "" {
				continue
			}
			definition.CleanupReason = reason
			candidates = append(candidates, definition)
			break
		}
	}
	return candidates, nil
}

func (s *CleanupService) managedArtifactCleanupCandidates(
	ctx context.Context,
	tenantID uint,
	cleanupContext map[string]interface{},
) ([]repository.CleanupManagedArtifact, error) {
	if s.managedArtifactRepo == nil {
		return nil, nil
	}
	artifacts, err := s.managedArtifactRepo.List(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	validEngineIDs, err := s.validCleanupEngineIDs(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	candidates := make([]repository.CleanupManagedArtifact, 0)
	for _, artifact := range artifacts {
		target := cleanupTaskTarget{
			EngineID: artifact.SourceEngineID, ItemID: artifact.ItemID,
			ItemFingerprint: artifact.ItemFingerprint, Locator: artifact.Locator, VerifySource: true,
		}
		if !s.matchesCleanupTaskTarget(target, cleanupContext) {
			continue
		}
		if s.taskTargetSourceExists(ctx, tenantID, target, cleanupContext, validEngineIDs) {
			continue
		}
		candidates = append(candidates, artifact)
	}
	return candidates, nil
}

func (s *CleanupService) deleteManagedArtifact(ctx context.Context, artifact repository.CleanupManagedArtifact) error {
	switch artifact.TaskType {
	case commonExecution.TaskTypeRasterCOGGeneration:
		if s.rasterCOGSvc == nil {
			return errors.New("raster COG cleanup service is not configured")
		}
		return s.rasterCOGSvc.DeleteResult(ctx, artifact.ID, artifact.TenantID)
	case commonExecution.TaskTypeModel3DTilesGeneration:
		if s.model3DTilesSvc == nil {
			return errors.New("model 3D Tiles cleanup service is not configured")
		}
		return s.model3DTilesSvc.DeleteResult(ctx, artifact.ID, artifact.TenantID)
	case commonExecution.TaskTypeModel3DGLBGeneration:
		if s.model3DGLBSvc == nil {
			return errors.New("model GLB cleanup service is not configured")
		}
		return s.model3DGLBSvc.DeleteResult(ctx, artifact.ID, artifact.TenantID)
	case commonExecution.TaskTypeGaussianSplatKSplatGeneration:
		if s.gaussianSplatSvc == nil {
			return errors.New("KSplat cleanup service is not configured")
		}
		return s.gaussianSplatSvc.DeleteResult(ctx, artifact.ID, artifact.TenantID)
	case commonExecution.TaskTypePointCloudCOPCGeneration:
		if s.pointCloudCOPCSvc == nil {
			return errors.New("COPC cleanup service is not configured")
		}
		return s.pointCloudCOPCSvc.DeleteResult(ctx, artifact.ID, artifact.TenantID)
	case commonExecution.TaskTypePPTXPDFGeneration:
		if s.pptxPDFSvc == nil {
			return errors.New("PPTX PDF cleanup service is not configured")
		}
		return s.pptxPDFSvc.DeleteResult(ctx, artifact.ID, artifact.TenantID)
	default:
		return fmt.Errorf("unsupported Manager cleanup artifact type %q", artifact.TaskType)
	}
}

func (s *CleanupService) validCleanupEngineIDs(
	ctx context.Context,
	tenantID uint,
	cleanupContext map[string]interface{},
) (map[uint]struct{}, error) {
	if uintFromCleanupContext(cleanupContext, "engine_id") > 0 || s.systemClient == nil {
		return nil, nil
	}
	engines, err := s.systemClient.WithTenantID(tenantID).ListEngines(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tenant engines for Manager cleanup: %w", err)
	}
	valid := make(map[uint]struct{}, len(engines))
	for index := range engines {
		engine := &engines[index]
		if engineselection.IsSelectionOption(engine) && engineselection.HasStorageCapability(engine) {
			valid[engine.ID] = struct{}{}
		}
	}
	return valid, nil
}

func (s *CleanupService) matchesCleanupContext(locator string, itemFingerprint string, itemID uint, cleanupContext map[string]interface{}) bool {
	if len(cleanupContext) == 0 {
		return true
	}
	if engineID := uintFromCleanupContext(cleanupContext, "engine_id"); engineID > 0 {
		loc, err := resourcetree.ParseURI(locator)
		if err != nil || loc.EngineID != engineID {
			return false
		}
	}
	if contextItemID := uintFromCleanupContext(cleanupContext, "item_id"); contextItemID > 0 && itemID != contextItemID {
		return false
	}
	if fingerprint := strings.TrimSpace(stringFromCleanupContext(cleanupContext, "item_fingerprint")); fingerprint != "" && strings.TrimSpace(itemFingerprint) != fingerprint {
		return false
	}
	return true
}

func (s *CleanupService) matchesCleanupTaskTarget(target cleanupTaskTarget, cleanupContext map[string]interface{}) bool {
	if len(cleanupContext) == 0 {
		return true
	}
	if engineID := uintFromCleanupContext(cleanupContext, "engine_id"); engineID > 0 && target.EngineID != engineID {
		return false
	}
	if contextItemID := uintFromCleanupContext(cleanupContext, "item_id"); contextItemID > 0 && target.ItemID != contextItemID {
		return false
	}
	if fingerprint := strings.TrimSpace(stringFromCleanupContext(cleanupContext, "item_fingerprint")); fingerprint != "" && strings.TrimSpace(target.ItemFingerprint) != fingerprint {
		return false
	}
	return true
}

func (s *CleanupService) taskTargetSourceExists(
	ctx context.Context,
	tenantID uint,
	target cleanupTaskTarget,
	cleanupContext map[string]interface{},
	validEngineIDs map[uint]struct{},
) bool {
	return s.taskTargetMissingReason(ctx, tenantID, target, cleanupContext, validEngineIDs) == ""
}

func (s *CleanupService) taskTargetMissingReason(
	ctx context.Context,
	tenantID uint,
	target cleanupTaskTarget,
	cleanupContext map[string]interface{},
	validEngineIDs map[uint]struct{},
) string {
	if target.IsEmpty() {
		return ""
	}
	if engineID := uintFromCleanupContext(cleanupContext, "engine_id"); engineID > 0 && target.EngineID == engineID {
		return "missing_engine"
	}
	if validEngineIDs != nil && target.EngineID > 0 {
		if _, exists := validEngineIDs[target.EngineID]; !exists {
			return "missing_engine"
		}
	}
	if !target.VerifySource {
		return ""
	}
	if !s.sourceExists(ctx, tenantID, target.Locator, target.ItemID) {
		return "missing_source"
	}
	return ""
}

func (s *CleanupService) sourceExists(ctx context.Context, tenantID uint, locator string, itemID uint) bool {
	if s.metaClient == nil {
		return true
	}
	client := s.metaClient.WithTenantID(tenantID)
	if itemID > 0 {
		if _, err := client.GetItemByID(itemID); err == nil {
			return true
		}
	}
	loc, err := resourcetree.ParseURI(strings.TrimSpace(locator))
	if err != nil {
		return false
	}
	if loc.ItemID != nil {
		_, err := client.GetItemByID(*loc.ItemID)
		return err == nil
	}
	if loc.NodeID != nil {
		_, err := client.GetNodeByID(*loc.NodeID)
		return err == nil
	}
	if len(loc.Path) > 0 {
		_, err := client.GetItemByCatalogPath(loc.EngineID, loc.FullName())
		return err == nil
	}
	return false
}

func (s *CleanupService) sourceExistsForCleanup(
	ctx context.Context,
	tenantID uint,
	locator string,
	itemID uint,
	cleanupContext map[string]interface{},
) bool {
	engineID := uintFromCleanupContext(cleanupContext, "engine_id")
	if engineID > 0 {
		if loc, err := resourcetree.ParseURI(strings.TrimSpace(locator)); err == nil && loc.EngineID == engineID {
			return false
		}
	}
	return s.sourceExists(ctx, tenantID, locator, itemID)
}

func cloneCleanupContext(source map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(source)+1)
	for key, value := range source {
		result[key] = value
	}
	return result
}

func externalArtifactPolicy(cleanupContext map[string]interface{}) string {
	return strings.TrimSpace(stringFromCleanupContext(cleanupContext, "external_artifact_policy"))
}

func (s *CleanupService) abandonVectorMaterializedView(ctx context.Context, item *models.VectorMaterializedView, cleanupContext map[string]interface{}) error {
	if item == nil || s.optimizationSvc == nil || s.optimizationSvc.repo == nil {
		return errors.New("vector materialized view result is required")
	}
	metadata := item.Metadata.Clone()
	if metadata == nil {
		metadata = commonModels.JSONMap{}
	}
	now := time.Now().Format(time.RFC3339Nano)
	metadata["abandoned_external_at"] = now
	metadata["abandoned_external_by"] = uintFromCleanupContext(cleanupContext, "requested_by")
	metadata["external_artifact_policy"] = commonModels.ExternalArtifactPolicyAbandon
	if strings.TrimSpace(item.ErrorMessage) != "" {
		metadata["last_cleanup_error"] = item.ErrorMessage
	}
	message := strings.TrimSpace(item.ErrorMessage)
	if message == "" {
		message = "external artifact explicitly abandoned during engine deletion"
	}
	return s.optimizationSvc.repo.MarkResultAbandonedExternal(ctx, item.ID, item.TenantID, message, metadata)
}

func (s *CleanupService) createExecutorExecution(ctx context.Context, event events.CleanupRequestEvent) (*commonExecution.TaskExecution, time.Time, error) {
	if s.taskExecRepo == nil || event.ParentExecutionID == "" {
		return nil, time.Time{}, nil
	}
	startedAt := time.Now()
	currentStep := fmt.Sprintf("Manager 资源回收 %s", event.Action)
	triggerType, err := commonExecution.NormalizeTriggerType(event.TriggerType)
	if err != nil {
		triggerType = commonExecution.TriggerTypeManual
	}
	exec := &commonExecution.TaskExecution{
		TenantID:          int(event.TenantID),
		ExecutionID:       uuid.NewString(),
		Module:            commonExecution.ModuleManager,
		TaskType:          commonExecution.TaskTypeCleanupExecutor,
		Source:            commonExecution.ModuleSystem,
		ParentExecutionID: &event.ParentExecutionID,
		Status:            commonExecution.ExecutionStatusRunning,
		Progress:          0,
		CurrentStep:       &currentStep,
		TriggerType:       triggerType,
		TriggeredBy:       intPtr(int(event.RequestedBy)),
		ExecutionConfig: commonModels.JSONMap{
			"task_id":       event.TaskID,
			"action":        event.Action,
			"cleanup_mode":  event.CleanupMode,
			"based_on_scan": event.BasedOnScan,
			"cause_event":   event.CauseEvent,
			"context":       event.Context,
		},
		StartedAt: &startedAt,
		CreatedAt: startedAt,
		UpdatedAt: startedAt,
	}
	if err := s.taskExecRepo.Create(ctx, exec); err != nil {
		return nil, startedAt, err
	}
	return exec, startedAt, nil
}

func (s *CleanupService) finishExecutorExecution(ctx context.Context, executionID string, tenantID uint, startedAt time.Time, result events.CleanupResultData) {
	if s.taskExecRepo == nil || executionID == "" {
		return
	}
	now := time.Now()
	status := commonExecution.StatusFromCleanupResult(result.Status)
	var errDetails commonModels.JSONMap
	if len(result.Errors) > 0 {
		errDetails = commonModels.JSONMap{"errors": result.Errors}
	}
	if err := s.taskExecRepo.UpdateFields(ctx, executionID, int(tenantID), map[string]interface{}{
		"status":            status,
		"progress":          100,
		"metadata":          commonModels.JSONMap{"cleanup_result": result, "summary": result.Summary},
		"error_details":     errDetails,
		"completed_at":      now,
		"execution_time_ms": now.Sub(startedAt).Milliseconds(),
		"updated_at":        now,
	}); err != nil {
		s.log.Warn("更新 Manager 资源回收执行记录失败", "execution_id", executionID, "error", err)
	}
}

func (s *CleanupService) writeResult(ctx context.Context, taskID string, result events.CleanupResultData) {
	if s.redis == nil || taskID == "" {
		return
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		s.log.Error("序列化 Manager 资源回收结果失败", "error", err, "task_id", taskID)
		return
	}
	key := fmt.Sprintf("cleanup:results:%s", taskID)
	if err := s.redis.HSet(ctx, key, events.ModuleManager, string(resultJSON)).Err(); err != nil {
		s.log.Error("写入 Manager 资源回收结果失败", "error", err, "task_id", taskID)
	}
}

func managerScanSummary(stats *ManagerCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	scanned := stats.PreviewStates + stats.TileCaches + stats.Embeddings + stats.VectorMaterializedViews + stats.ManagedArtifacts + stats.TaskDefinitions
	return events.CleanupResultSummary{
		ScannedItems: scanned,
		FreedBytes:   stats.FreedBytes,
		SkippedItems: stats.SkippedExternalTargets,
		ErrorCount:   len(stats.Errors),
		RiskLevel:    riskLevelForCount(scanned),
	}
}

func managerExecuteSummary(stats *ManagerCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	affected := stats.PreviewStates + stats.TileCaches + stats.Embeddings + stats.VectorMaterializedViews + stats.ManagedArtifacts + stats.DisabledTaskDefinitions + stats.DeletedTaskDefinitions
	return events.CleanupResultSummary{
		AffectedRecords:          affected,
		DeletedPhysicalArtifacts: stats.DeletedPhysicalArtifacts,
		FreedBytes:               stats.FreedBytes,
		MarkedMissingSource:      stats.MarkedMissingSource,
		DisabledTaskDefinitions:  stats.DisabledTaskDefinitions,
		DeletedTaskDefinitions:   stats.DeletedTaskDefinitions,
		SkippedItems:             stats.SkippedExternalTargets,
		ErrorCount:               len(stats.Errors),
		RiskLevel:                riskLevelForCount(affected),
	}
}

func riskLevelForCount(count int) string {
	if count > 1000 {
		return "high"
	}
	if count > 100 {
		return "medium"
	}
	return "low"
}

func managerCleanupStatsToMap(stats *ManagerCleanupStats) map[string]interface{} {
	data, _ := json.Marshal(stats)
	var result map[string]interface{}
	_ = json.Unmarshal(data, &result)
	return result
}

type cleanupTaskTarget struct {
	EngineID        uint
	ItemID          uint
	ItemFingerprint string
	Locator         string
	VerifySource    bool
}

func (t cleanupTaskTarget) IsEmpty() bool {
	return t.EngineID == 0 && t.ItemID == 0 && strings.TrimSpace(t.ItemFingerprint) == "" && strings.TrimSpace(t.Locator) == ""
}

func cleanupTaskTargetsFromDefinition(definition repository.CleanupTaskDefinition) []cleanupTaskTarget {
	targets := make([]cleanupTaskTarget, 0, 3)
	if len(definition.ResourceBindings) > 0 {
		for _, binding := range definition.ResourceBindings {
			itemID := uint(0)
			if binding.ItemID != nil {
				itemID = *binding.ItemID
			}
			targets = append(targets, cleanupTaskTarget{
				EngineID: binding.EngineID, ItemID: itemID,
				ItemFingerprint: strings.TrimSpace(binding.ItemFingerprint),
				Locator:         strings.TrimSpace(binding.Locator), VerifySource: binding.Role == models.TaskResourceRoleSource,
			})
		}
		return targets
	}
	if definition.SourceEngineID > 0 || definition.ItemID > 0 || strings.TrimSpace(definition.ItemFingerprint) != "" || strings.TrimSpace(definition.Locator) != "" {
		targets = append(targets, cleanupTaskTarget{
			EngineID: definition.SourceEngineID, ItemID: definition.ItemID,
			ItemFingerprint: strings.TrimSpace(definition.ItemFingerprint),
			Locator:         strings.TrimSpace(definition.Locator), VerifySource: true,
		})
	}
	for _, section := range []string{"source", "target"} {
		values, ok := asJSONMap(definition.Config[section])
		if !ok {
			continue
		}
		engineID := firstPositiveUintFromConfig(values, "source_engine_id", "target_engine_id", "engine_id")
		locator := firstNonEmptyStringFromConfig(values, "locator", "item_locator", "node_locator", "storage_locator")
		if engineID == 0 && locator != "" {
			if parsed, err := resourcetree.ParseURI(locator); err == nil {
				engineID = parsed.EngineID
			}
		}
		verifySource := stringFromConfig(values["locator"]) != "" ||
			stringFromConfig(values["item_locator"]) != "" ||
			stringFromConfig(values["node_locator"]) != "" ||
			uintFromConfig(values["item_id"]) > 0 ||
			strings.TrimSpace(stringFromConfig(values["item_fingerprint"])) != ""
		target := cleanupTaskTarget{
			EngineID: engineID, ItemID: uintFromConfig(values["item_id"]),
			ItemFingerprint: strings.TrimSpace(stringFromConfig(values["item_fingerprint"])),
			Locator:         locator, VerifySource: verifySource,
		}
		if !target.IsEmpty() {
			targets = append(targets, target)
		}
	}
	return targets
}

func firstPositiveUintFromConfig(values commonModels.JSONMap, keys ...string) uint {
	for _, key := range keys {
		if value := uintFromConfig(values[key]); value > 0 {
			return value
		}
	}
	return 0

}

func firstNonEmptyStringFromConfig(values commonModels.JSONMap, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(stringFromConfig(values[key])); value != "" {
			return value
		}
	}
	return ""
}

func uintFromCleanupContext(values map[string]interface{}, key string) uint {
	switch value := values[key].(type) {
	case uint:
		return value
	case int:
		if value > 0 {
			return uint(value)
		}
	case int64:
		if value > 0 {
			return uint(value)
		}
	case float64:
		if value > 0 {
			return uint(value)
		}
	case string:
		var parsed uint64
		if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil {
			return uint(parsed)
		}
	}
	return 0
}

func stringFromCleanupContext(values map[string]interface{}, key string) string {
	if value, ok := values[key].(string); ok {
		return value
	}
	return ""
}

func uintPtrValue(value *uint) uint {
	if value == nil {
		return 0
	}
	return *value
}
