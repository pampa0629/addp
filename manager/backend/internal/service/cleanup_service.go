package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/addp/common/cleanup"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/exportartifact"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/minio/minio-go/v7"
	"gorm.io/gorm"
)

type CleanupService struct {
	db                  *gorm.DB
	search              *HybridSearchService
	runtime             *cleanup.Runner
	artifacts           *CleanupArtifactService
	metaClient          *commonClient.MetaClient
	systemClient        *commonClient.SystemServiceClient
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
	IndexDocuments           int      `json:"index_documents"`
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
	db *gorm.DB,
	search *HybridSearchService,
	metaClient *commonClient.MetaClient,
	systemClient *commonClient.SystemServiceClient,
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
	s := &CleanupService{
		db: db, search: search,
		metaClient:          metaClient,
		systemClient:        systemClient,
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
	effects := CleanupArtifactEffects{Bucket: s.minioBucket}
	if rasterCOGSvc != nil {
		effects.Objects = rasterCOGSvc.cleaner
	}
	if model3DTilesSvc != nil {
		effects.Directories = model3DTilesSvc.cleaner
	}
	if optimizationSvc != nil {
		effects.Tables = optimizationSvc.dbProvider
	}
	if tileCacheSvc != nil {
		effects.TileInvalidator = tileCacheSvc.tileCacheRuntimeInvalidator
	}
	s.artifacts = NewCleanupArtifactService(db, effects)
	s.runtime = &cleanup.Runner{DB: db, Owner: events.ModuleManager, System: systemClient, Log: s.log, Handle: s.runCleanupExecution}
	return s
}

func (s *CleanupService) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if err := s.runtime.Start(ctx); err != nil {
		return err
	}
	if s.exportRepo != nil && s.minioClient != nil && s.minioBucket != "" {
		go s.runExportSessionCleanup(ctx)
		s.log.Info("Manager 导出暂存清理已启动")
	}
	if s.model3DGLBSvc != nil {
		go s.runModel3DOutputCleanup(ctx)
	}
	return nil
}

func (s *CleanupService) Stop() {
	if s == nil || s.stopCh == nil {
		return
	}
	s.runtime.Stop()
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

func (s *CleanupService) ScanReclaimCandidates(ctx context.Context, tenantID uint, scope map[string]interface{}) (*ManagerCleanupStats, error) {
	plan, err := s.buildCleanupPlan(ctx, tenantID, scope)
	if err != nil {
		return nil, err
	}
	return plan.statistics(), nil
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

func managerScanSummary(stats *ManagerCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	scanned := stats.IndexDocuments + stats.PreviewStates + stats.TileCaches + stats.Embeddings + stats.VectorMaterializedViews + stats.ManagedArtifacts + stats.TaskDefinitions
	return events.CleanupResultSummary{
		ScannedItems: scanned,
		SkippedItems: stats.SkippedExternalTargets,
		ErrorCount:   len(stats.Errors),
		RiskLevel:    riskLevelForCount(scanned),
	}
}

func managerExecuteSummary(stats *ManagerCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	affected := stats.IndexDocuments + stats.PreviewStates + stats.TileCaches + stats.Embeddings + stats.VectorMaterializedViews + stats.ManagedArtifacts + stats.DisabledTaskDefinitions + stats.DeletedTaskDefinitions
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
	// Derived task identity comes only from normalized Owner bindings. Embedding
	// has its own registered config contract and is the sole config adapter.
	if definition.TaskType != commonExecution.TaskTypeEmbedding {
		return targets
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
