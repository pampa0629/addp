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
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
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
	redis            *redis.Client
	metaClient       *commonClient.MetaClient
	taskExecRepo     *commonExecution.TaskExecutionRepository
	previewStateRepo *repository.PreviewStateRepository
	tileCacheSvc     *TileCacheTaskService
	embeddingRepo    *repository.EmbeddingRepository
	optimizationSvc  *VectorMaterializedViewTaskService
	exportRepo       *repository.ExportSessionRepository
	minioClient      *minio.Client
	minioBucket      string
	exportCleanup    ExportCleanupOptions
	log              *slog.Logger
	stopCh           chan struct{}
}

type ExportCleanupOptions struct {
	SuccessRetention time.Duration
	FailedRetention  time.Duration
	MaxRunningAge    time.Duration
	Interval         time.Duration
}

type ManagerCleanupStats struct {
	PreviewStates            int      `json:"preview_states"`
	TileCaches               int      `json:"vector_tile_caches"`
	Embeddings               int      `json:"embeddings"`
	VectorMaterializedViews  int      `json:"vector_materialized_view_generations"`
	ExportSessions           int      `json:"export_sessions,omitempty"`
	DeletedPhysicalArtifacts int      `json:"deleted_physical_artifacts,omitempty"`
	MarkedMissingSource      int      `json:"marked_missing_source,omitempty"`
	SkippedExternalTargets   int      `json:"skipped_external_targets,omitempty"`
	DisabledTaskDefinitions  int      `json:"disabled_task_definitions,omitempty"`
	Errors                   []string `json:"errors,omitempty"`
}

func NewCleanupService(
	redisClient *redis.Client,
	metaClient *commonClient.MetaClient,
	taskExecRepo *commonExecution.TaskExecutionRepository,
	previewStateRepo *repository.PreviewStateRepository,
	tileCacheSvc *TileCacheTaskService,
	embeddingRepo *repository.EmbeddingRepository,
	optimizationSvc *VectorMaterializedViewTaskService,
	exportRepo *repository.ExportSessionRepository,
	minioClient *minio.Client,
	minioBucket string,
	exportCleanup ExportCleanupOptions,
) *CleanupService {
	exportCleanup = normalizeExportCleanupOptions(exportCleanup)
	return &CleanupService{
		redis:            redisClient,
		metaClient:       metaClient,
		taskExecRepo:     taskExecRepo,
		previewStateRepo: previewStateRepo,
		tileCacheSvc:     tileCacheSvc,
		embeddingRepo:    embeddingRepo,
		optimizationSvc:  optimizationSvc,
		exportRepo:       exportRepo,
		minioClient:      minioClient,
		minioBucket:      strings.Trim(minioBucket, "/"),
		exportCleanup:    exportCleanup,
		log:              logger.With("component", "manager_cleanup_service"),
		stopCh:           make(chan struct{}),
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

func normalizeExportCleanupOptions(opts ExportCleanupOptions) ExportCleanupOptions {
	if opts.SuccessRetention <= 0 {
		opts.SuccessRetention = 24 * time.Hour
	}
	if opts.FailedRetention <= 0 {
		opts.FailedRetention = 6 * time.Hour
	}
	if opts.MaxRunningAge <= 0 {
		opts.MaxRunningAge = 6 * time.Hour
	}
	if opts.Interval <= 0 {
		opts.Interval = 30 * time.Minute
	}
	return opts
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
	now := time.Now()
	expiredRunningBefore := now.Add(-s.exportCleanup.MaxRunningAge)
	if affected, err := s.exportRepo.MarkRunningExpired(ctx, expiredRunningBefore); err != nil {
		s.log.Warn("标记超时导出会话失败", "error", err)
	} else if affected > 0 {
		s.log.Info("已标记超时导出会话", "count", affected)
	}

	successBefore := now.Add(-s.exportCleanup.SuccessRetention)
	failedBefore := now.Add(-s.exportCleanup.FailedRetention)
	for {
		sessions, err := s.exportRepo.ListExpiredFinalSessions(ctx, successBefore, failedBefore, 100)
		if err != nil {
			s.log.Warn("查询过期导出会话失败", "error", err)
			return
		}
		if len(sessions) == 0 {
			return
		}
		for _, session := range sessions {
			if session == nil {
				continue
			}
			if err := s.cleanupExportSession(ctx, session); err != nil {
				s.log.Warn("清理导出会话失败", "session_id", session.ID, "error", err)
				continue
			}
		}
		if len(sessions) < 100 {
			return
		}
	}
}

func (s *CleanupService) cleanupExportSession(ctx context.Context, session *models.ExportSession) error {
	if session == nil {
		return nil
	}
	prefix, err := s.exportSessionCleanupPrefix(session)
	if err != nil {
		return err
	}
	if prefix != "" {
		deleted, err := s.deleteMinIOPrefix(ctx, prefix)
		if err != nil {
			return err
		}
		if deleted > 0 {
			s.log.Info("已清理导出暂存对象", "session_id", session.ID, "deleted", deleted)
		}
	}
	return s.exportRepo.Delete(ctx, session.ID, session.TenantID)
}

func (s *CleanupService) exportSessionCleanupPrefix(session *models.ExportSession) (string, error) {
	parent := strings.TrimSpace(session.TargetParentLocator)
	if parent != "" {
		loc, err := parseManagerInfraLocator(parent)
		if err != nil {
			return "", err
		}
		if loc.bucket != s.minioBucket {
			return "", fmt.Errorf("export session bucket mismatch")
		}
		return strings.Trim(loc.objectPath, "/") + "/", nil
	}
	locator := strings.TrimSpace(session.TargetLocator)
	if locator == "" {
		return "", nil
	}
	loc, err := parseManagerInfraLocator(locator)
	if err != nil {
		return "", err
	}
	if loc.bucket != s.minioBucket {
		return "", fmt.Errorf("export session bucket mismatch")
	}
	path := strings.Trim(loc.objectPath, "/")
	if path == "" {
		return "", nil
	}
	if idx := strings.LastIndex(path, "/"); idx > 0 {
		return path[:idx+1], nil
	}
	return "", nil
}

func (s *CleanupService) deleteMinIOPrefix(ctx context.Context, prefix string) (int, error) {
	prefix = strings.TrimPrefix(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return 0, nil
	}
	deleted := 0
	objectCh := s.minioClient.ListObjects(ctx, s.minioBucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})
	for object := range objectCh {
		if object.Err != nil {
			return deleted, object.Err
		}
		if strings.TrimSpace(object.Key) == "" {
			continue
		}
		if err := s.minioClient.RemoveObject(ctx, s.minioBucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
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
	case events.CleanupActionExecute:
		stats, err := s.ExecuteCleanup(ctx, event.TenantID, event.CleanupMode, event.Context)
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
	taskCandidates, err := s.countTaskDefinitionCleanupCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	stats.DisabledTaskDefinitions = taskCandidates
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
	if err := s.cleanupTaskDefinitions(ctx, tenantID, cleanupMode, cleanupContext, stats); err != nil {
		return nil, err
	}
	return stats, nil
}

func (s *CleanupService) countTaskDefinitionCleanupCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (int, error) {
	total := 0
	if s.embeddingRepo != nil {
		tasks, err := s.embeddingRepo.ListAllEmbeddingTasks(ctx, tenantID)
		if err != nil {
			return 0, err
		}
		total += len(s.filterEmbeddingTasksForCleanup(ctx, tenantID, tasks, cleanupContext))
	}
	if s.tileCacheSvc != nil && s.tileCacheSvc.tileCacheRepo != nil {
		tasks, err := s.tileCacheSvc.tileCacheRepo.ListAllTasks(ctx, tenantID)
		if err != nil {
			return 0, err
		}
		total += len(s.filterTileCacheTasksForCleanup(ctx, tenantID, tasks, cleanupContext))
	}
	if s.optimizationSvc != nil && s.optimizationSvc.repo != nil {
		tasks, err := s.optimizationSvc.repo.ListAllTasks(ctx, tenantID)
		if err != nil {
			return 0, err
		}
		total += len(s.filterVectorMaterializedViewTasksForCleanup(ctx, tenantID, tasks, cleanupContext))
	}
	return total, nil
}

func (s *CleanupService) cleanupTaskDefinitions(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}, stats *ManagerCleanupStats) error {
	if stats == nil {
		return nil
	}
	if s.embeddingRepo != nil {
		tasks, err := s.embeddingRepo.ListAllEmbeddingTasks(ctx, tenantID)
		if err != nil {
			return err
		}
		for _, task := range s.filterEmbeddingTasksForCleanup(ctx, tenantID, tasks, cleanupContext) {
			if cleanupMode == events.CleanupModePhysical {
				if err := s.embeddingRepo.DeleteEmbeddingTask(ctx, task.ID, tenantID); err != nil {
					stats.Errors = append(stats.Errors, fmt.Sprintf("delete embedding_task %d: %v", task.ID, err))
					continue
				}
			} else if err := s.embeddingRepo.DisableEmbeddingTaskForCleanup(ctx, tenantID, task.ID, cleanupTaskDefinitionReason(cleanupContext)); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("disable embedding_task %d: %v", task.ID, err))
				continue
			}
			stats.DisabledTaskDefinitions++
		}
	}
	if s.tileCacheSvc != nil && s.tileCacheSvc.tileCacheRepo != nil {
		tasks, err := s.tileCacheSvc.tileCacheRepo.ListAllTasks(ctx, tenantID)
		if err != nil {
			return err
		}
		for _, task := range s.filterTileCacheTasksForCleanup(ctx, tenantID, tasks, cleanupContext) {
			if cleanupMode == events.CleanupModePhysical {
				if err := s.tileCacheSvc.tileCacheRepo.DeleteTask(ctx, task.ID, tenantID); err != nil {
					stats.Errors = append(stats.Errors, fmt.Sprintf("delete vector_tile_cache_task %d: %v", task.ID, err))
					continue
				}
			} else if err := s.tileCacheSvc.tileCacheRepo.DisableTaskForCleanup(ctx, tenantID, task.ID, cleanupTaskDefinitionReason(cleanupContext)); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("disable vector_tile_cache_task %d: %v", task.ID, err))
				continue
			}
			stats.DisabledTaskDefinitions++
		}
	}
	if s.optimizationSvc != nil && s.optimizationSvc.repo != nil {
		tasks, err := s.optimizationSvc.repo.ListAllTasks(ctx, tenantID)
		if err != nil {
			return err
		}
		for _, task := range s.filterVectorMaterializedViewTasksForCleanup(ctx, tenantID, tasks, cleanupContext) {
			if cleanupMode == events.CleanupModePhysical {
				if err := s.optimizationSvc.repo.DeleteTask(ctx, task.ID, tenantID); err != nil {
					stats.Errors = append(stats.Errors, fmt.Sprintf("delete vector_materialized_view_generation_task %d: %v", task.ID, err))
					continue
				}
			} else if err := s.optimizationSvc.repo.DisableTaskForCleanup(ctx, tenantID, task.ID, cleanupTaskDefinitionReason(cleanupContext)); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("disable vector_materialized_view_generation_task %d: %v", task.ID, err))
				continue
			}
			stats.DisabledTaskDefinitions++
		}
	}
	return nil
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
		if s.sourceExists(ctx, tenantID, item.Locator, 0) {
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
		if s.sourceExists(ctx, tenantID, item.Locator, itemID) {
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
		if s.sourceExists(ctx, tenantID, item.Locator, item.ItemID) {
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
		itemID := uintPtrValue(item.ItemID)
		if !s.matchesCleanupContext(item.Locator, item.ItemFingerprint, itemID, cleanupContext) {
			continue
		}
		if s.sourceExists(ctx, tenantID, item.Locator, itemID) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (s *CleanupService) filterEmbeddingTasksForCleanup(ctx context.Context, tenantID uint, tasks []*models.EmbeddingTask, cleanupContext map[string]interface{}) []*models.EmbeddingTask {
	out := make([]*models.EmbeddingTask, 0)
	for _, task := range tasks {
		if task == nil || !task.Enabled {
			continue
		}
		target := cleanupTaskTargetFromConfig(task.Config)
		if !s.matchesCleanupTaskTarget(target, cleanupContext) {
			continue
		}
		if s.taskTargetSourceExists(ctx, tenantID, target, cleanupContext) {
			continue
		}
		out = append(out, task)
	}
	return out
}

func (s *CleanupService) filterTileCacheTasksForCleanup(ctx context.Context, tenantID uint, tasks []*models.TileCacheTask, cleanupContext map[string]interface{}) []*models.TileCacheTask {
	out := make([]*models.TileCacheTask, 0)
	for _, task := range tasks {
		if task == nil || !task.Enabled {
			continue
		}
		target := cleanupTaskTargetFromConfig(task.Config)
		if !s.matchesCleanupTaskTarget(target, cleanupContext) {
			continue
		}
		if s.taskTargetSourceExists(ctx, tenantID, target, cleanupContext) {
			continue
		}
		out = append(out, task)
	}
	return out
}

func (s *CleanupService) filterVectorMaterializedViewTasksForCleanup(ctx context.Context, tenantID uint, tasks []*models.VectorMaterializedViewTask, cleanupContext map[string]interface{}) []*models.VectorMaterializedViewTask {
	out := make([]*models.VectorMaterializedViewTask, 0)
	for _, task := range tasks {
		if task == nil || !task.Enabled {
			continue
		}
		target := cleanupTaskTargetFromConfig(task.Config)
		if !s.matchesCleanupTaskTarget(target, cleanupContext) {
			continue
		}
		if s.taskTargetSourceExists(ctx, tenantID, target, cleanupContext) {
			continue
		}
		out = append(out, task)
	}
	return out
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

func (s *CleanupService) taskTargetSourceExists(ctx context.Context, tenantID uint, target cleanupTaskTarget, cleanupContext map[string]interface{}) bool {
	if target.IsEmpty() {
		return true
	}
	if engineID := uintFromCleanupContext(cleanupContext, "engine_id"); engineID > 0 && target.EngineID == engineID {
		return false
	}
	return s.sourceExists(ctx, tenantID, target.Locator, target.ItemID)
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
	status := commonExecution.ExecutionStatusSuccess
	if result.Status == events.CleanupResultFailed {
		status = commonExecution.ExecutionStatusFailed
	}
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
	scanned := stats.PreviewStates + stats.TileCaches + stats.Embeddings + stats.VectorMaterializedViews + stats.DisabledTaskDefinitions
	return events.CleanupResultSummary{
		ScannedItems:            scanned,
		DisabledTaskDefinitions: stats.DisabledTaskDefinitions,
		SkippedItems:            stats.SkippedExternalTargets,
		ErrorCount:              len(stats.Errors),
		RiskLevel:               riskLevelForCount(scanned),
	}
}

func managerExecuteSummary(stats *ManagerCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	affected := stats.PreviewStates + stats.TileCaches + stats.Embeddings + stats.VectorMaterializedViews + stats.DisabledTaskDefinitions
	return events.CleanupResultSummary{
		AffectedRecords:          affected,
		DeletedPhysicalArtifacts: stats.DeletedPhysicalArtifacts,
		MarkedMissingSource:      stats.MarkedMissingSource,
		DisabledTaskDefinitions:  stats.DisabledTaskDefinitions,
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
}

func (t cleanupTaskTarget) IsEmpty() bool {
	return t.EngineID == 0 && t.ItemID == 0 && strings.TrimSpace(t.ItemFingerprint) == "" && strings.TrimSpace(t.Locator) == ""
}

func cleanupTaskTargetFromConfig(config commonModels.JSONMap) cleanupTaskTarget {
	targetMap, ok := asJSONMap(config["target"])
	if !ok {
		return cleanupTaskTarget{}
	}
	engineID := uintFromConfig(targetMap["engine_id"])
	if engineID == 0 {
		engineID = uintFromConfig(targetMap["source_engine_id"])
	}
	return cleanupTaskTarget{
		EngineID:        engineID,
		ItemID:          uintFromConfig(targetMap["item_id"]),
		ItemFingerprint: strings.TrimSpace(stringFromConfig(targetMap["item_fingerprint"])),
		Locator:         strings.TrimSpace(stringFromConfig(targetMap["locator"])),
	}
}

func cleanupTaskDefinitionReason(cleanupContext map[string]interface{}) string {
	if uintFromCleanupContext(cleanupContext, "engine_id") > 0 {
		return "missing_engine"
	}
	return "missing_source"
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
