package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/extractor"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scantask"
	"github.com/addp/meta/internal/search"
	"gorm.io/gorm"
)

// ScanService 统一扫描服务
type ScanService struct {
	db                       *gorm.DB
	repo                     *metaRepo.ScanRepository  // 数据访问层
	dbScanService            *DatabaseScanService      // 数据库扫描服务
	nosqlScanService         *NoSQLScanService         // NoSQL 数据库扫描服务
	objectCatalogScanService *ObjectCatalogScanService // 对象 catalog 扫描服务
	fileCatalogScanService   *FileCatalogScanService   // 文件 catalog 扫描服务
	metadataQueryService     *MetadataQueryService     // 元数据查询服务（独立）
	engineService            *EngineService
	config                   *config.Config
	log                      *slog.Logger
	indexer                  *search.Indexer
	indexerService           *IndexerService              // 索引服务（独立）
	spatialService           *SpatialMetadataService      // 空间元数据服务（独立）
	scanEventPublisher       *events.ScanEventPublisher   // 扫描事件发布器
	metadataExtractor        *extractor.MetadataExtractor // 元数据提取器
	dedupService             *ScanDedupService            // 扫描去重服务（可选）
	immediateRecorder        *scantask.ImmediateExecutionRecorder
}

// ScanProgressReporter 用于在长时间扫描任务中更新进度
type ScanProgressReporter interface {
	SetTotal(total int)
	Advance(label string, completed, total int, meta map[string]interface{})
	Message(message string)
}

func NewScanService(db *gorm.DB, engineService *EngineService) *ScanService {
	if engineService == nil {
		engineService = NewEngineService(db, "", "", nil) // nil Redis client for fallback
	}

	repo := metaRepo.NewScanRepository(db)
	log := logger.With("component", "scan_service")

	// 创建独立的服务（消除循环依赖）
	indexerService := NewIndexerService(nil, log)         // indexer 稍后通过 SetIndexer 注入
	spatialService := NewSpatialMetadataService(nil, log) // config 稍后通过 SetConfig 注入
	metadataExtractor := extractor.NewMetadataExtractor(db)

	s := &ScanService{
		db:                db,
		repo:              repo,
		engineService:     engineService,
		log:               log,
		metadataExtractor: metadataExtractor,
		indexerService:    indexerService,
		spatialService:    spatialService,
		immediateRecorder: scantask.NewImmediateExecutionRecorder(db),
	}

	// 创建 DatabaseScanService（使用独立服务，无循环依赖）
	s.dbScanService = NewDatabaseScanService(db, log, nil, repo, spatialService, indexerService)

	// 创建 NoSQLScanService（使用独立服务，无循环依赖）
	s.nosqlScanService = NewNoSQLScanService(db, log, nil, repo, indexerService)

	// 创建 ObjectCatalogScanService（使用独立服务，无循环依赖）
	s.objectCatalogScanService = NewObjectCatalogScanService(db, log, repo, metadataExtractor, indexerService)

	// 创建 FileCatalogScanService
	s.fileCatalogScanService = NewFileCatalogScanService(db, log, repo, indexerService)

	// 创建 MetadataQueryService（提供元数据查询接口）
	s.metadataQueryService = NewMetadataQueryService(db, spatialService, engineService, log)

	return s
}

func (s *ScanService) CountItems(tenantID uint) (int64, error) {
	var itemCount int64
	if err := s.db.Table("metadata.meta_item").Where("tenant_id = ?", tenantID).Count(&itemCount).Error; err != nil {
		return 0, err
	}
	return itemCount, nil
}

// SetIndexer 注入搜索索引器
func (s *ScanService) SetIndexer(indexer *search.Indexer) {
	s.indexer = indexer
	// 同时注入到独立服务
	if s.indexerService != nil {
		s.indexerService.indexer = indexer
	}
	if s.dbScanService != nil {
		s.dbScanService.indexer = indexer
	}
}

// SetConfig 注入配置
func (s *ScanService) SetConfig(cfg *config.Config) {
	s.config = cfg
	// 同时注入到空间元数据服务
	if s.spatialService != nil {
		s.spatialService.config = cfg
	}
}

// SetScanEventPublisher 注入扫描事件发布器
func (s *ScanService) SetScanEventPublisher(publisher *events.ScanEventPublisher) {
	s.scanEventPublisher = publisher
}

// SetDedupService 注入扫描去重服务
func (s *ScanService) SetDedupService(dedupService *ScanDedupService) {
	s.dedupService = dedupService

	// 启动时清理所有残留的扫描锁（防止上次服务异常退出时的锁未清理）
	if dedupService != nil {
		s.cleanupStaleScanLocks()
	}
}

// cleanupStaleScanLocks 清理所有残留的扫描锁
// 在服务启动时调用，清理上次服务异常退出时未清理的锁
func (s *ScanService) cleanupStaleScanLocks() {
	if s.dedupService == nil {
		return
	}

	ctx := context.Background()

	// 1. 查询所有状态为"running"的节点
	var staleNodes []models.MetaNode
	if err := s.db.Where("scan_status = ?", "running").Find(&staleNodes).Error; err != nil {
		s.log.Warn("查询残留扫描节点失败", "error", err)
		return
	}

	if len(staleNodes) == 0 {
		s.log.Info("无残留扫描锁，跳过清理")
		return
	}

	s.log.Info("开始清理残留扫描锁", "stale_nodes_count", len(staleNodes))

	cleanedCount := 0
	for _, node := range staleNodes {
		// 2. 生成锁的 key
		lockKey := s.dedupService.GenerateSchemaLockKey(node.TenantID, node.EngineID, node.Name)

		// 3. 清理 Redis 锁
		if err := s.dedupService.ClearTask(ctx, lockKey); err != nil {
			s.log.Warn("清理残留锁失败",
				"node_id", node.ID,
				"engine_id", node.EngineID,
				"schema", node.Name,
				"error", err)
			continue
		}

		// 4. 重置节点状态为"pending"
		if err := s.db.Model(&node).Updates(map[string]interface{}{
			"scan_status": "pending",
			"scanned_at":  nil,
		}).Error; err != nil {
			s.log.Warn("重置节点状态失败",
				"node_id", node.ID,
				"error", err)
			continue
		}

		cleanedCount++
		s.log.Info("清理残留锁成功",
			"node_id", node.ID,
			"engine_id", node.EngineID,
			"tenant_id", node.TenantID,
			"schema", node.Name)
	}

	s.log.Info("残留扫描锁清理完成",
		"total", len(staleNodes),
		"cleaned", cleanedCount)
}

// publishScanCompletedEvent 发布扫描完成事件（异步）
func (s *ScanService) publishScanCompletedEvent(engineID, tenantID uint, summary models.JSONMap) {
	if s.scanEventPublisher == nil {
		return // 未配置事件发布器，跳过
	}

	// 异步发布，不阻塞主流程
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		event := scantask.ScanCompletedEvent(engineID, tenantID, commonModels.JSONMap(summary), time.Now())
		if err := s.scanEventPublisher.PublishScanCompleted(ctx, event); err != nil {
			s.log.Error("发布扫描完成事件失败",
				"engine_id", engineID,
				"tenant_id", tenantID,
				"error", err)
		}
	}()
}

// AutoScanUnscanned 自动扫描所有未扫描的资源
func (s *ScanService) AutoScanUnscanned(tenantID uint) (*models.ScanResponse, error) {
	startTime := time.Now()

	// 获取所有数据库资源
	engines, err := s.engineService.GetEnginesByTenant(tenantID)
	if err != nil {
		return nil, err
	}

	totalSchemas := 0
	totalTables := 0
	totalFields := 0
	scannedResourceIDs := []uint{}

	// 对每个资源进行扫描
	for _, engine := range engines {
		schemas, tables, fields, err := s.scanResource(engine, tenantID, 0)
		if err != nil {
			s.log.Warn("资源扫描失败",
				"engine_id", engine.ID,
				"resource_name", engine.Name,
				"tenant_id", tenantID,
				"error", err,
			)
			continue
		}

		totalSchemas += schemas
		totalTables += tables
		totalFields += fields
		scannedResourceIDs = append(scannedResourceIDs, engine.ID)
	}

	counts := scantask.ScanCounts{Namespaces: totalSchemas, Items: totalTables, Fields: totalFields}
	return scantask.AutoScanResponse(len(scannedResourceIDs), counts, startTime, time.Now()), nil
}

type ScanOptions struct {
	EngineID    uint
	TenantID    uint
	Namespaces  []string
	ObjectPaths []string
	Token       string
	ScanDepth   string
	Force       bool
	Reporter    ScanProgressReporter
	NodeID      uint
	ItemID      uint
	Targets     []string
}

// ScanEngine 扫描指定引擎
func (s *ScanService) ScanEngine(engineID, tenantID uint, namespaces, objectPaths []string, token string) (*models.ScanResponse, error) {
	return s.ScanEngineWithOptions(ScanOptions{
		EngineID:    engineID,
		TenantID:    tenantID,
		Namespaces:  namespaces,
		ObjectPaths: objectPaths,
		Token:       token,
		ScanDepth:   "basic",
	})
}

// ScanEngineWithProgress 扫描指定引擎，并通过 reporter 汇报进度
func (s *ScanService) ScanEngineWithProgress(engineID, tenantID uint, namespaces, objectPaths []string, token string, reporter ScanProgressReporter) (*models.ScanResponse, error) {
	return s.ScanEngineWithOptions(ScanOptions{
		EngineID:    engineID,
		TenantID:    tenantID,
		Namespaces:  namespaces,
		ObjectPaths: objectPaths,
		Token:       token,
		ScanDepth:   "basic",
		Reporter:    reporter,
	})
}

// ScanEngineWithDepth 扫描指定引擎，支持指定扫描深度
func (s *ScanService) ScanEngineWithDepth(engineID, tenantID uint, namespaces, objectPaths []string, token string, scanDepth string, reporter ScanProgressReporter) (*models.ScanResponse, error) {
	return s.ScanEngineWithOptions(ScanOptions{
		EngineID:    engineID,
		TenantID:    tenantID,
		Namespaces:  namespaces,
		ObjectPaths: objectPaths,
		Token:       token,
		ScanDepth:   scanDepth,
		Reporter:    reporter,
	})
}

func (s *ScanService) ScanEngineWithOptions(opts ScanOptions) (*models.ScanResponse, error) {
	startTime := time.Now()
	tenantID := opts.TenantID
	namespaces := opts.Namespaces
	objectPaths := opts.ObjectPaths
	token := opts.Token
	scanDepth := opts.ScanDepth
	reporter := opts.Reporter
	engineID, err := s.resolveScanEngineID(tenantID, opts)
	if err != nil {
		return nil, err
	}

	// 获取资源
	if reporter != nil {
		reporter.Message("正在加载资源连接信息")
	}
	resource, err := s.engineService.GetResourceByID(engineID, tenantID, token)
	if err != nil {
		return nil, err
	}
	effectiveTenantID := tenantID
	if resource.TenantID != nil && *resource.TenantID > 0 {
		effectiveTenantID = *resource.TenantID
	}

	resolvedNamespaces, resolvedObjectPaths, err := s.resolveScanTargets(effectiveTenantID, opts)
	if err != nil {
		return nil, err
	}
	namespaces = append(namespaces, resolvedNamespaces...)
	objectPaths = append(objectPaths, resolvedObjectPaths...)

	scanDepth, err = scantask.NormalizeScanDepth(scanDepth, scantask.ScanDepthBasic)
	if err != nil {
		return nil, err
	}

	var directExecID string
	if reporter == nil {
		execID, createErr := s.immediateRecorder.Create(resource, effectiveTenantID, namespaces, objectPaths, scanDepth, opts.Force, startTime)
		if createErr != nil {
			return nil, createErr
		}
		directExecID = execID
	}

	startFields := append(connectionLogFields(resource),
		"mode", "manual",
		"scan_depth", scanDepth,
		"force", opts.Force,
	)
	if len(namespaces) > 0 {
		startFields = append(startFields, "target_namespaces", namespaces)
	}
	if len(objectPaths) > 0 {
		startFields = append(startFields, "target_paths", objectPaths)
	}
	s.log.Info("开始扫描资源", startFields...)

	result, err := s.dispatchScan(scanDispatchRequest{
		Resource:    resource,
		TenantID:    effectiveTenantID,
		Namespaces:  namespaces,
		ObjectPaths: objectPaths,
		ScanDepth:   scanDepth,
		Force:       opts.Force,
		Reporter:    reporter,
		Mode:        scanDispatchManual,
	})

	if err != nil {
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描失败: %v", err))
		}
		if directExecID != "" {
			s.immediateRecorder.Fail(directExecID, int(effectiveTenantID), err, startTime)
		}
		return nil, err
	}

	completedAt := time.Now()
	durationMs := completedAt.Sub(startTime).Milliseconds()

	finishFields := append(make([]any, 0, len(startFields)+6), startFields...)
	finishFields = append(finishFields,
		"namespaces_scanned", result.Namespaces,
		"items_scanned", result.Items,
		"fields_scanned", result.Fields,
		"duration_ms", durationMs,
	)
	s.log.Info("资源扫描完成", finishFields...)

	if reporter != nil {
		reporter.Message("扫描完成")
	}

	if directExecID != "" {
		resultMeta := scantask.ScanResultMetadata(scantask.ScanCounts{Namespaces: result.Namespaces, Items: result.Items, Fields: result.Fields})
		s.immediateRecorder.Complete(directExecID, int(effectiveTenantID), resultMeta, startTime, completedAt)
		s.publishScanCompletedEvent(resource.ID, effectiveTenantID, models.JSONMap(resultMeta))
	}

	return scantask.NewScanResponse(
		"success",
		"Scan completed successfully",
		scantask.ScanCounts{Namespaces: result.Namespaces, Items: result.Items, Fields: result.Fields},
		startTime,
		completedAt,
	), nil
}

func (s *ScanService) resolveScanEngineID(tenantID uint, opts ScanOptions) (uint, error) {
	if opts.EngineID > 0 {
		return opts.EngineID, nil
	}
	if opts.NodeID > 0 {
		var node models.MetaNode
		if err := s.db.Select("engine_id").Where("tenant_id = ? AND id = ?", tenantID, opts.NodeID).First(&node).Error; err != nil {
			return 0, fmt.Errorf("node target not found: %w", err)
		}
		return node.EngineID, nil
	}
	if opts.ItemID > 0 {
		var item models.MetaItem
		if err := s.db.Select("engine_id").Where("tenant_id = ? AND id = ?", tenantID, opts.ItemID).First(&item).Error; err != nil {
			return 0, fmt.Errorf("item target not found: %w", err)
		}
		return item.EngineID, nil
	}
	for _, target := range opts.Targets {
		if id, ok := engineIDFromLocator(target); ok {
			return id, nil
		}
	}
	return 0, fmt.Errorf("engine_id is required")
}

func (s *ScanService) resolveScanTargets(tenantID uint, opts ScanOptions) ([]string, []string, error) {
	namespaces := []string{}
	objectPaths := []string{}

	if opts.NodeID > 0 {
		var node models.MetaNode
		if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, opts.NodeID).First(&node).Error; err != nil {
			return nil, nil, fmt.Errorf("node target not found: %w", err)
		}
		ns, paths := scanTargetFromNode(node)
		namespaces = append(namespaces, ns...)
		objectPaths = append(objectPaths, paths...)
	}

	if opts.ItemID > 0 {
		var item models.MetaItem
		if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, opts.ItemID).First(&item).Error; err != nil {
			return nil, nil, fmt.Errorf("item target not found: %w", err)
		}
		ns, paths := scanTargetFromItem(item)
		namespaces = append(namespaces, ns...)
		objectPaths = append(objectPaths, paths...)
	}

	for _, target := range opts.Targets {
		ns, paths := scanTargetFromLocator(target)
		namespaces = append(namespaces, ns...)
		objectPaths = append(objectPaths, paths...)
	}

	return uniqueNonEmpty(namespaces), uniqueNonEmpty(objectPaths), nil
}

func scanTargetFromNode(node models.MetaNode) ([]string, []string) {
	switch node.NodeType {
	case "schema", "database":
		if node.FullName != "" {
			return []string{node.FullName}, nil
		}
		return []string{node.Name}, nil
	case "bucket", "prefix", "root", "dir":
		if node.FullName != "" {
			return nil, []string{node.FullName}
		}
		if node.Name != "" {
			return nil, []string{node.Name}
		}
	}
	return nil, nil
}

func scanTargetFromItem(item models.MetaItem) ([]string, []string) {
	switch item.ItemType {
	case "table", "collection", "label", "relationship":
		if idx := strings.LastIndex(item.FullName, "."); idx > 0 {
			return []string{item.FullName[:idx]}, nil
		}
	case "object", "file":
		if item.FullName != "" {
			return nil, []string{item.FullName}
		}
	}
	return nil, nil
}

func scanTargetFromLocator(locator string) ([]string, []string) {
	locator = strings.TrimSpace(locator)
	if locator == "" {
		return nil, nil
	}
	typeIdx := strings.Index(locator, "?type=")
	pathPart := locator
	targetType := ""
	if typeIdx >= 0 {
		pathPart = locator[:typeIdx]
		targetType = locator[typeIdx+6:]
		if amp := strings.Index(targetType, "&"); amp >= 0 {
			targetType = targetType[:amp]
		}
	}
	pathMarker := "/path/"
	pathIdx := strings.Index(pathPart, pathMarker)
	if pathIdx < 0 {
		return nil, nil
	}
	path := strings.Trim(pathPart[pathIdx+len(pathMarker):], "/")
	if path == "" {
		return nil, nil
	}
	path = strings.ReplaceAll(path, "%2F", "/")
	path = strings.ReplaceAll(path, "%2f", "/")
	switch targetType {
	case "table", "collection", "label", "relationship":
		parts := strings.Split(path, "/")
		if len(parts) > 1 {
			return []string{parts[0]}, nil
		}
		return []string{path}, nil
	case "schema", "database":
		return []string{strings.Split(path, "/")[0]}, nil
	case "object", "file", "bucket", "prefix", "directory", "root", "dir":
		return nil, []string{path}
	}
	return nil, []string{path}
}

func engineIDFromLocator(locator string) (uint, bool) {
	locator = strings.TrimSpace(locator)
	const prefix = "addp://engine/"
	if !strings.HasPrefix(locator, prefix) {
		return 0, false
	}
	rest := strings.TrimPrefix(locator, prefix)
	idx := strings.Index(rest, "/")
	if idx < 0 {
		return 0, false
	}
	var id uint
	if _, err := fmt.Sscanf(rest[:idx], "%d", &id); err != nil {
		return 0, false
	}
	return id, id > 0
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

// scanResource 扫描单个资源的所有未扫描Schema
func (s *ScanService) scanResource(resource *commonModels.Engine, tenantID uint, scanLogID uint) (int, int, int, error) {
	startFields := append(connectionLogFields(resource),
		"scan_log_id", scanLogID,
		"mode", "auto",
	)
	s.log.Info("开始扫描资源", startFields...)

	result, err := s.dispatchScan(scanDispatchRequest{
		Resource:  resource,
		TenantID:  tenantID,
		ScanDepth: "deep",
		Force:     false,
		ScanLogID: scanLogID,
		Mode:      scanDispatchAuto,
	})
	if err != nil {
		return 0, 0, 0, err
	}

	s.log.Info("资源扫描完成", cloneLogFields(startFields,
		"namespaces_scanned", result.Namespaces,
		"items_scanned", result.Items,
		"fields_scanned", result.Fields,
	)...)
	return result.Namespaces, result.Items, result.Fields, nil
}

func (s *ScanService) scanResourceSchemasWithReporter(resource *commonModels.Engine, tenantID uint, namespaces []string, scanLogID uint, scanDepth string, force bool, reporter ScanProgressReporter) (int, int, int, error) {
	resourceID := resource.ID

	startFields := append(connectionLogFields(resource),
		"scan_log_id", scanLogID,
		"mode", "manual",
	)
	if len(namespaces) > 0 {
		startFields = append(startFields, "target_namespaces", namespaces)
	}
	s.log.Info("开始扫描指定命名空间列表", startFields...)

	// 如果未指定命名空间，则扫描所有命名空间（插件负责过滤系统命名空间）
	if len(namespaces) == 0 {
		if reporter != nil {
			reporter.Message("未指定命名空间，正在获取完整列表")
		}

		// 获取插件
		p, err := plugin.Get(resource.EngineType)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
		}

		schemasInfo, err := metacatalog.SchemaInfos(context.Background(), resource, p)
		if err != nil {
			return 0, 0, 0, err
		}

		for _, info := range schemasInfo {
			namespaces = append(namespaces, info.Name)
		}

		if reporter != nil {
			reporter.Message(fmt.Sprintf("已过滤系统命名空间，待扫描 %d 个用户命名空间", len(namespaces)))
		}
	}

	totalSchemas := 0
	totalTables := 0
	totalFields := 0
	total := len(namespaces)
	var scanErrors []error
	if reporter != nil {
		reporter.SetTotal(total)
	}
	completed := 0

	for _, schemaName := range namespaces {
		// 使用匿名函数包装每次循环，确保 defer 在每次循环结束时执行
		func(schema string) {
			if reporter != nil {
				reporter.Message(fmt.Sprintf("开始扫描命名空间 %s", schema))
			}

			// 检查命名空间级锁
			ctx := context.Background()
			var schemaLock string
			lockAcquired := false

			if s.dedupService != nil {
				schemaLock = s.dedupService.GenerateSchemaLockKey(tenantID, resourceID, schema)
				if s.dedupService.CheckTaskExists(ctx, schemaLock) {
					s.log.Info("命名空间正在扫描中，跳过",
						"engine_id", resourceID,
						"namespace", schema)
					if reporter != nil {
						reporter.Message(fmt.Sprintf("命名空间 %s 正在扫描中，跳过", schema))
					}
					completed++
					return
				}

				// 加命名空间级锁
				if err := s.dedupService.MarkTaskRunning(ctx, schemaLock, 2*time.Hour); err != nil {
					s.log.Warn("加命名空间级锁失败", "namespace", schema, "error", err)
				} else {
					lockAcquired = true
				}

				// 使用 defer 确保锁在任何情况下都会被清理（包括 panic）
				defer func() {
					if lockAcquired && schemaLock != "" {
						if clearErr := s.dedupService.ClearTask(context.Background(), schemaLock); clearErr != nil {
							s.log.Warn("清除命名空间级锁失败", "namespace", schema, "error", clearErr)
						}
					}
				}()
			}

			// 扫描命名空间
			schemas, tables, fields, err := s.dbScanService.ScanSchema(context.Background(), resource, tenantID, resourceID, schema, scanDepth, force)

			if err != nil {
				s.log.Warn("命名空间扫描失败",
					"engine_id", resourceID,
					"tenant_id", tenantID,
					"namespace", schema,
					"error", err,
				)
				if reporter != nil {
					reporter.Message(fmt.Sprintf("命名空间 %s 扫描失败: %v", schema, err))
				}
				scanErrors = append(scanErrors, fmt.Errorf("%s: %w", schema, err))
				return
			}
			totalSchemas += schemas
			totalTables += tables
			totalFields += fields

			completed++
			if reporter != nil {
				reporter.Advance(schema, completed, total, map[string]interface{}{
					"tables": tables,
					"fields": fields,
				})
			}
		}(schemaName)
	}

	s.log.Info("指定命名空间扫描完成", cloneLogFields(startFields,
		"namespaces_scanned", totalSchemas,
		"items_scanned", totalTables,
		"fields_scanned", totalFields,
	)...)

	if len(scanErrors) > 0 {
		return totalSchemas, totalTables, totalFields, fmt.Errorf("failed to scan %d namespace(s): %v", len(scanErrors), scanErrors[0])
	}

	return totalSchemas, totalTables, totalFields, nil
}

// scanNoSQLResourceWithReporter 扫描 NoSQL 资源的指定数据库列表（带进度报告）
func (s *ScanService) scanNoSQLResourceWithReporter(
	enginePlugin plugin.EnginePlugin,
	resource *commonModels.Engine,
	tenantID uint,
	databaseNames []string,
	scanDepth string,
	force bool,
	reporter ScanProgressReporter,
) (int, int, int, error) {

	resourceID := resource.ID
	ctx := context.Background()
	catalogProvider, ok := enginePlugin.(plugin.CatalogProvider)
	if !ok {
		return 0, 0, 0, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
	}

	startFields := append(connectionLogFields(resource),
		"mode", "manual",
		"scan_depth", scanDepth,
	)

	// 如果未指定数据库，列出所有数据库
	if len(databaseNames) == 0 {
		if reporter != nil {
			reporter.Message("列出所有数据库...")
		}

		databasesInfo, err := metacatalog.NoSQLDatabases(ctx, resource, catalogProvider)
		if err != nil {
			return 0, 0, 0, err
		}

		for _, info := range databasesInfo {
			databaseNames = append(databaseNames, info.Name)
		}

		if reporter != nil {
			reporter.Message(fmt.Sprintf("已过滤系统数据库，待扫描 %d 个用户数据库", len(databaseNames)))
		}
	}

	totalDatabases := 0
	totalCollections := 0
	totalFields := 0
	total := len(databaseNames)
	if reporter != nil {
		reporter.SetTotal(total)
	}
	completed := 0

	for _, databaseName := range databaseNames {
		if reporter != nil {
			reporter.Message(fmt.Sprintf("开始扫描数据库 %s", databaseName))
		}

		// 检查数据库级锁
		var dbLock string
		if s.dedupService != nil {
			dbLock = s.dedupService.GenerateSchemaLockKey(tenantID, resourceID, databaseName)
			if s.dedupService.CheckTaskExists(ctx, dbLock) {
				s.log.Info("数据库正在扫描中，跳过",
					"engine_id", resourceID,
					"database", databaseName)
				if reporter != nil {
					reporter.Message(fmt.Sprintf("数据库 %s 正在扫描中，跳过", databaseName))
				}
				completed++
				continue
			}

			// 加数据库级锁
			if err := s.dedupService.MarkTaskRunning(ctx, dbLock, 2*time.Hour); err != nil {
				s.log.Warn("加数据库级锁失败", "database", databaseName, "error", err)
			}
		}

		// 扫描数据库
		databases, collections, fields, err := s.nosqlScanService.ScanDatabase(
			ctx, enginePlugin, resource, tenantID, databaseName, scanDepth, force,
		)

		// 扫描完成后清理锁
		if s.dedupService != nil && dbLock != "" {
			if clearErr := s.dedupService.ClearTask(ctx, dbLock); clearErr != nil {
				s.log.Warn("清除数据库级锁失败", "database", databaseName, "error", clearErr)
			}
		}

		if err != nil {
			s.log.Warn("数据库扫描失败",
				"engine_id", resourceID,
				"tenant_id", tenantID,
				"database", databaseName,
				"error", err,
			)
			if reporter != nil {
				reporter.Message(fmt.Sprintf("数据库 %s 扫描失败: %v", databaseName, err))
			}
			continue
		}
		totalDatabases += databases
		totalCollections += collections
		totalFields += fields

		completed++
		if reporter != nil {
			reporter.Advance(databaseName, completed, total, map[string]interface{}{
				"collections": collections,
				"fields":      fields,
			})
		}
	}

	s.log.Info("指定数据库扫描完成", cloneLogFields(startFields,
		"databases_scanned", totalDatabases,
		"collections_scanned", totalCollections,
		"fields_scanned", totalFields,
	)...)

	return totalDatabases, totalCollections, totalFields, nil
}

// scanFileCatalogResourceWithReporter 扫描文件 catalog 资源。
func (s *ScanService) scanFileCatalogResourceWithReporter(resource *commonModels.Engine, tenantID uint, objectPaths []string, scanDepth string, force bool, reporter ScanProgressReporter) (int, int, int, error) {
	roots, items, err := s.fileCatalogScanService.ScanPaths(resource, tenantID, objectPaths, scanDepth, force, reporter)
	if err != nil {
		return 0, 0, 0, err
	}
	return roots, items, 0, nil
}

func (s *ScanService) scanObjectCatalogResourceWithReporter(resource *commonModels.Engine, tenantID uint, objectPaths []string, scanDepth string, force bool, reporter ScanProgressReporter) (int, int, int, error) {
	// 标准化 scanDepth
	if scanDepth == "" {
		scanDepth = "deep"
	}

	if reporter != nil && len(objectPaths) > 0 {
		reporter.SetTotal(len(objectPaths))
	}

	// 调用 ObjectCatalogScanService 进行扫描
	buckets, objects, err := s.objectCatalogScanService.ScanPaths(
		resource,
		tenantID,
		objectPaths,
		nil,
		scanDepth,
		force,
		reporter,
	)
	if err != nil {
		return 0, 0, 0, err
	}

	return buckets, objects, 0, nil
}

// GetObjectMetadata 获取指定对象的元数据 (代理到 metadataExtractor)
func (s *ScanService) GetObjectMetadata(tenantID, engineID uint, objectKey string) (*models.MetaItem, error) {
	return s.metadataExtractor.GetObjectMetadata(tenantID, engineID, objectKey)
}

// ExtractObjectMetadataOnDemand 按需提取对象的深度元数据 (代理到 metadataExtractor)
func (s *ScanService) ExtractObjectMetadataOnDemand(tenantID, engineID uint, objectKey string, token string, objectReader io.Reader) (map[string]interface{}, error) {
	return s.metadataExtractor.ExtractObjectMetadataOnDemand(tenantID, engineID, objectKey, token, objectReader)
}

// BuildObjectContentIndexOnDemand 按需建立对象内容索引。
func (s *ScanService) BuildObjectContentIndexOnDemand(tenantID, engineID uint, objectKey string, objectReader io.Reader) (models.JSONMap, error) {
	return s.metadataExtractor.BuildObjectContentIndexOnDemand(tenantID, engineID, objectKey, objectReader)
}

// ============================================================================
// 元数据查询接口（委托给 MetadataQueryService）
// ============================================================================

// ListItemsByEngine 获取引擎下所有已扫描数据项。
func (s *ScanService) ListItemsByEngine(engineID, tenantID uint) ([]models.MetaItemLite, error) {
	return s.metadataQueryService.ListItemsByEngine(engineID, tenantID)
}

// ListItemsByNamespace 获取命名空间下所有已扫描数据项。
func (s *ScanService) ListItemsByNamespace(engineID, tenantID uint, namespace string) ([]models.MetaItemLite, error) {
	return s.metadataQueryService.ListItemsByNamespace(engineID, tenantID, namespace)
}

// GetItemFieldNames 获取数据项字段名列表。
func (s *ScanService) GetItemFieldNames(engineID uint, namespace, itemName string, tenantID uint) ([]string, error) {
	return s.metadataQueryService.GetItemFieldNames(engineID, namespace, itemName, tenantID)
}

// GetItemFieldDetailsByName 获取数据项字段详细信息（支持空间字段识别）。
func (s *ScanService) GetItemFieldDetailsByName(engineID uint, namespace, itemName string, tenantID uint) ([]commonModels.FieldInfo, error) {
	return s.metadataQueryService.GetItemFieldDetailsByName(engineID, namespace, itemName, tenantID)
}

// GetItemFieldDetailsByID 按 item_id 获取数据项字段详细信息。
func (s *ScanService) GetItemFieldDetailsByID(tenantID, itemID uint) ([]commonModels.FieldInfo, error) {
	return s.metadataQueryService.GetItemFieldDetailsByID(tenantID, itemID)
}

// GetMetadataTree 获取资源的完整元数据树（用于Manager模块）
func (s *ScanService) GetMetadataTree(tenantID, engineID uint) (*models.MetadataTreeResponse, error) {
	return s.metadataQueryService.GetMetadataTree(tenantID, engineID)
}

// GetNodeByPath 按路径查询节点（用于Manager模块）
func (s *ScanService) GetNodeByPath(tenantID, engineID uint, nodePath string) (*models.MetaNodeLite, error) {
	return s.metadataQueryService.GetNodeByPath(tenantID, engineID, nodePath)
}

// GetItemByPath 按路径查询项目（用于Manager模块）
func (s *ScanService) GetItemByPath(tenantID, engineID uint, bucketName, objectPath string) (*models.MetaItemLite, error) {
	return s.metadataQueryService.GetItemByPath(tenantID, engineID, bucketName, objectPath)
}

// GetNodeChildren 获取节点的子节点（用于Manager模块）
func (s *ScanService) GetNodeChildren(tenantID, nodeID uint) ([]models.MetaNodeLite, error) {
	return s.metadataQueryService.GetNodeChildren(tenantID, nodeID)
}

// GetNodeItems 获取节点下的项目（用于Manager模块）
func (s *ScanService) GetNodeItems(tenantID, nodeID uint) ([]models.MetaItemLite, error) {
	return s.metadataQueryService.GetNodeItems(tenantID, nodeID)
}

// GetItemSpatialMetadataByName 获取数据项空间元数据（用于 Manager/Service）。
func (s *ScanService) GetItemSpatialMetadataByName(tenantID, engineID uint, namespace, itemName string) (*models.SpatialMetadataResponse, error) {
	return s.metadataQueryService.GetItemSpatialMetadataByName(tenantID, engineID, namespace, itemName)
}

// GetItemSpatialMetadataByID 按 item_id 获取数据项空间元数据。
func (s *ScanService) GetItemSpatialMetadataByID(tenantID, itemID uint) (*models.SpatialMetadataResponse, error) {
	return s.metadataQueryService.GetItemSpatialMetadataByID(tenantID, itemID)
}

// GetMetaNodeByID 获取单个节点详情（用于Manager模块）
func (s *ScanService) GetMetaNodeByID(tenantID, nodeID uint) (*models.MetaNodeLite, error) {
	return s.metadataQueryService.GetMetaNodeByID(tenantID, nodeID)
}

// GetItemByID 按 ID 查询 MetaItem
func (s *ScanService) GetItemByID(tenantID, itemID uint) (*models.MetaItemLite, error) {
	return s.metadataQueryService.GetItemByID(tenantID, itemID)
}
