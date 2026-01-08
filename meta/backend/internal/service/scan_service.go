package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/events"
	"github.com/addp/common/format"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/config"
	metaErrors "github.com/addp/meta/internal/errors"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/search"
	"gorm.io/gorm"
)

// ScanService 统一扫描服务
type ScanService struct {
	db                       *gorm.DB
	repo                     *ScanRepository               // 数据访问层
	dbScanService            *DatabaseScanService          // 数据库扫描服务
	nosqlScanService         *NoSQLScanService             // NoSQL 数据库扫描服务
	objectScanService        *ObjectStorageScanService     // 对象存储扫描服务
	metadataQueryService     *MetadataQueryService         // 元数据查询服务（独立）
	resourceDiscoveryService *ResourceDiscoveryService     // 资源发现服务（独立）
	engineService            *EngineService
	config                   *config.Config
	log                      *slog.Logger
	indexer                  *search.Indexer
	indexerService           *IndexerService               // 索引服务（独立）
	spatialService           *SpatialMetadataService       // 空间元数据服务（独立）
	scanEventPublisher       *events.ScanEventPublisher    // 扫描事件发布器
	metadataExtractor        *MetadataExtractor            // 元数据提取器
	dedupService             *ScanDedupService             // 扫描去重服务（可选）
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

	repo := NewScanRepository(db)
	log := logger.With("component", "scan_service")

	// 创建独立的服务（消除循环依赖）
	indexerService := NewIndexerService(nil, log) // indexer 稍后通过 SetIndexer 注入
	spatialService := NewSpatialMetadataService(nil, log) // config 稍后通过 SetConfig 注入
	metadataExtractor := NewMetadataExtractor(db)

	s := &ScanService{
		db:                db,
		repo:              repo,
		engineService:     engineService,
		log:               log,
		metadataExtractor: metadataExtractor,
		indexerService:    indexerService,
		spatialService:    spatialService,
	}

	// 创建 DatabaseScanService（使用独立服务，无循环依赖）
	s.dbScanService = NewDatabaseScanService(db, log, nil, repo, spatialService, indexerService)

	// 创建 NoSQLScanService（使用独立服务，无循环依赖）
	s.nosqlScanService = NewNoSQLScanService(db, log, nil, repo, indexerService)

	// 创建 ObjectStorageScanService（使用独立服务，无循环依赖）
	s.objectScanService = NewObjectStorageScanService(db, log, repo, metadataExtractor, indexerService)

	// 创建 MetadataQueryService（提供元数据查询接口）
	s.metadataQueryService = NewMetadataQueryService(db, spatialService, log)

	// 创建 ResourceDiscoveryService（提供资源发现接口）
	s.resourceDiscoveryService = NewResourceDiscoveryService(db, engineService, log)

	return s
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
}

// verifyResourceAccess 验证租户是否有权限访问资源
// 返回 nil 表示有权限，返回错误表示无权限或资源不存在
func (s *ScanService) verifyResourceAccess(engineID, tenantID uint, token string) error {
	// 通过 ResourceService 获取资源（内部已包含租户校验）
	resource, err := s.engineService.GetResourceByID(engineID, tenantID, token)
	if err != nil {
		s.log.Warn("资源访问验证失败",
			"engine_id", engineID,
			"tenant_id", tenantID,
			"error", err)
		return metaErrors.ErrEngineAccessDenied
	}

	// 非超级管理员（tenant_id > 0）必须验证租户匹配
	if tenantID > 0 && (resource.TenantID == nil || *resource.TenantID != tenantID) {
		s.log.Warn("跨租户访问被拒绝",
			"engine_id", engineID,
			"resource_tenant_id", resource.TenantID,
			"request_tenant_id", tenantID)
		return metaErrors.ErrEngineAccessDenied
	}

	return nil
}

// publishScanCompletedEvent 发布扫描完成事件（异步）
func (s *ScanService) publishScanCompletedEvent(engineID, tenantID uint, summary models.JSONMap) {
	if s.scanEventPublisher == nil {
		return // 未配置事件发布器，跳过
	}

	// 异步发布，不阻塞主流程
	go func() {
		// 从 summary 中提取扫描信息
		scannedNodes := []string{}
		scannedItemsCount := 0
		scanType := events.ScanTypeDatabase // 默认为数据库扫描

		if schemasScanned, ok := summary["schemas_scanned"].(int); ok && schemasScanned > 0 {
			scanType = events.ScanTypeDatabase
		}
		if objectsScanned, ok := summary["objects_scanned"].(int); ok && objectsScanned > 0 {
			scanType = events.ScanTypeObjectStorage
			scannedItemsCount = objectsScanned
		}
		if tablesScanned, ok := summary["tables_scanned"].(int); ok {
			scannedItemsCount += tablesScanned
		}

		event := events.ScanCompletedEvent{
			EngineID:        engineID,
			TenantID:          tenantID,
			ScanType:          scanType,
			ScannedNodes:      scannedNodes,
			ScannedItemsCount: scannedItemsCount,
			Timestamp:         time.Now(),
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := s.scanEventPublisher.PublishScanCompleted(ctx, event); err != nil {
			s.log.Error("发布扫描完成事件失败",
				"engine_id", engineID,
				"tenant_id", tenantID,
				"error", err)
		}
	}()
}

func isObjectStorageType(resourceType string) bool {
	switch strings.ToLower(resourceType) {
	case "s3", "minio", "oss", "object_storage", "object-storage":
		return true
	default:
		return false
	}
}

type nodeAggregate struct {
	node      *models.MetaNode
	itemCount int
	totalSize int64
}

func (s *ScanService) upsertNode(tenantID, engineID uint, parent *models.MetaNode, nodeType, name, fullName string, attrs models.JSONMap) (*models.MetaNode, error) {
	return s.repo.UpsertNode(tenantID, engineID, parent, nodeType, name, fullName, attrs)
}

func (s *ScanService) resetNodeState(node *models.MetaNode, status string) error {
	return s.repo.ResetNodeState(node, status)
}

func (s *ScanService) finalizeNodeState(node *models.MetaNode, status string, itemCount int, totalSize int64, errMsg string) error {
	return s.repo.FinalizeNodeState(node, status, itemCount, totalSize, errMsg)
}

func (s *ScanService) hardDeleteItemsByNode(nodeID uint) error {
	return s.repo.HardDeleteItemsByNode(nodeID)
}

func (s *ScanService) hardDeleteDescendantNodes(node *models.MetaNode) error {
	return s.repo.HardDeleteDescendantNodes(node)
}

func (s *ScanService) upsertItem(
	tenantID, engineID uint,
	node *models.MetaNode,
	itemType, name, fullName string,
	attrs models.JSONMap,
	rowCount, sizeBytes *int64,
	dataUpdated *time.Time,
) (*models.MetaItem, error) {
	return s.repo.UpsertItem(tenantID, engineID, node, itemType, name, fullName, attrs, rowCount, sizeBytes, dataUpdated)
}

// upsertItemSelective 选择性更新item
// 当attrs为nil时，不更新attributes字段（用于basic扫描保留deep扫描的元数据）
func (s *ScanService) upsertItemSelective(
	tenantID, engineID uint,
	node *models.MetaNode,
	itemType, name, fullName string,
	attrs models.JSONMap,
	rowCount, sizeBytes *int64,
	dataUpdated *time.Time,
) (*models.MetaItem, error) {
	return s.repo.UpsertItemSelective(tenantID, engineID, node, itemType, name, fullName, attrs, rowCount, sizeBytes, dataUpdated)
}

func buildFieldAttributes(fields []format.ScannerFieldInfo) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(fields))
	for _, field := range fields {
		result = append(result, map[string]interface{}{
			"name":              field.Name,
			"ordinal_position":  field.OrdinalPosition,
			"data_type":         field.DataType,
			"column_type":       field.ColumnType,
			"is_nullable":       field.IsNullable,
			"default_value":     field.DefaultValue,
			"comment":           field.Comment,
			"is_primary_key":    field.IsPrimaryKey,
			"is_unique_key":     field.IsUniqueKey,
			"character_set":     field.CharacterSet,
			"collation":         field.Collation,
			"numeric_precision": field.NumericPrecision,
			"numeric_scale":     field.NumericScale,
		})
	}
	return result
}

func ensureNodeAggregate(stats map[uint]*nodeAggregate, node *models.MetaNode) *nodeAggregate {
	if agg, ok := stats[node.ID]; ok {
		return agg
	}
	agg := &nodeAggregate{node: node}
	stats[node.ID] = agg
	return agg
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

	completedAt := time.Now()
	durationMs := completedAt.Sub(startTime).Milliseconds()

	return &models.ScanResponse{
		Status:         "success",
		Message:        fmt.Sprintf("Successfully scanned %d engines", len(scannedResourceIDs)),
		SchemasScanned: totalSchemas,
		TablesScanned:  totalTables,
		FieldsScanned:  totalFields,
		DurationMs:     durationMs,
		StartedAt:      startTime.Format("2006-01-02 15:04:05"),
	}, nil
}

// ScanEngine 扫描指定引擎
func (s *ScanService) ScanEngine(engineID, tenantID uint, schemaNames, objectPaths []string, token string) (*models.ScanResponse, error) {
	return s.scanResourceInternal(engineID, tenantID, schemaNames, objectPaths, token, "deep", nil)
}

// ScanEngineWithProgress 扫描指定引擎，并通过 reporter 汇报进度
func (s *ScanService) ScanEngineWithProgress(engineID, tenantID uint, schemaNames, objectPaths []string, token string, reporter ScanProgressReporter) (*models.ScanResponse, error) {
	return s.scanResourceInternal(engineID, tenantID, schemaNames, objectPaths, token, "deep", reporter)
}

// ScanEngineWithDepth 扫描指定引擎，支持指定扫描深度
func (s *ScanService) ScanEngineWithDepth(engineID, tenantID uint, schemaNames, objectPaths []string, token string, scanDepth string, reporter ScanProgressReporter) (*models.ScanResponse, error) {
	return s.scanResourceInternal(engineID, tenantID, schemaNames, objectPaths, token, scanDepth, reporter)
}

func (s *ScanService) scanResourceInternal(engineID, tenantID uint, schemaNames, objectPaths []string, token string, scanDepth string, reporter ScanProgressReporter) (*models.ScanResponse, error) {
	startTime := time.Now()

	// 获取资源
	if reporter != nil {
		reporter.Message("正在加载资源连接信息")
	}
	resource, err := s.engineService.GetResourceByID(engineID, tenantID, token)
	if err != nil {
		return nil, err
	}

	var directRun *models.ScanTaskRun
	if reporter == nil {
		directRun, err = s.createImmediateRunRecord(resource, tenantID, schemaNames, objectPaths, startTime)
		if err != nil {
			return nil, err
		}
	}

	// 标准化 scanDepth 参数
	if scanDepth == "" {
		scanDepth = "basic" // 默认使用基础扫描
	}
	scanDepth = strings.ToLower(scanDepth)

	// 标准化深度值：统一使用 basic/deep 命名
	// 向后兼容：自动将旧版 shallow 转换为 basic
	if scanDepth == "shallow" {
		scanDepth = "basic" // 向后兼容：shallow 自动转为 basic
	}

	// 验证有效值
	if scanDepth != "basic" && scanDepth != "deep" {
		scanDepth = "basic" // 无效值默认使用基础扫描
	}

	startFields := append(connectionLogFields(resource),
		"mode", "manual",
		"scan_depth", scanDepth,
	)
	if len(schemaNames) > 0 {
		startFields = append(startFields, "target_schemas", schemaNames)
	}
	if len(objectPaths) > 0 {
		startFields = append(startFields, "target_paths", objectPaths)
	}
	s.log.Info("开始扫描资源", startFields...)

	resourceType := strings.ToLower(resource.EngineType)

	schemas, tables, fields := 0, 0, 0

	// 获取插件以判断类型
	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	// 根据插件类型路由到对应的扫描服务
	if nosqlPlugin, ok := p.(plugin.NoSQLPlugin); ok {
		// NoSQL 扫描（MongoDB、CouchDB 等）
		schemas, tables, fields, err = s.scanNoSQLResourceWithReporter(nosqlPlugin, resource, tenantID, schemaNames, scanDepth, reporter)
	} else if _, ok := p.(plugin.ObjectStoragePlugin); ok && isObjectStorageType(resourceType) {
		// 对象存储扫描（MinIO、S3 等）
		schemas, tables, fields, err = s.scanObjectStorageResourceWithReporter(resource, tenantID, objectPaths, schemaNames, scanDepth, reporter)
	} else if _, ok := p.(plugin.RelationalDBPlugin); ok {
		// 关系型数据库扫描（PostgreSQL、MySQL 等）
		schemas, tables, fields, err = s.scanResourceSchemasWithReporter(resource, tenantID, schemaNames, 0, scanDepth, reporter)
	} else {
		err = fmt.Errorf("plugin does not support metadata query")
	}

	if err != nil {
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描失败: %v", err))
		}
		if directRun != nil {
			s.failImmediateRun(directRun, err)
		}
		return nil, err
	}

	completedAt := time.Now()
	durationMs := completedAt.Sub(startTime).Milliseconds()

	finishFields := append(make([]any, 0, len(startFields)+6), startFields...)
	finishFields = append(finishFields,
		"schemas_scanned", schemas,
		"tables_scanned", tables,
		"fields_scanned", fields,
		"duration_ms", durationMs,
	)
	s.log.Info("资源扫描完成", finishFields...)

	if reporter != nil {
		reporter.Message("扫描完成")
	}

	// 完成运行记录
	if directRun != nil {
		resultSummary := models.JSONMap{
			"schemas_scanned": schemas,
			"tables_scanned":  tables,
			"fields_scanned":  fields,
		}
		s.completeImmediateRun(directRun, resultSummary, completedAt)
	} else {
		// 创建扫描运行记录
		run, err := s.createImmediateRunRecord(resource, tenantID, schemaNames, objectPaths, startTime)
		if err != nil {
			s.log.Warn("创建扫描运行记录失败", "engine_id", resource.ID, "error", err)
		} else {
			resultSummary := models.JSONMap{
				"schemas_scanned": schemas,
				"tables_scanned":  tables,
				"fields_scanned":  fields,
			}
			s.completeImmediateRun(run, resultSummary, completedAt)
		}
	}

	return &models.ScanResponse{
		Status:         "success",
		Message:        "Scan completed successfully",
		SchemasScanned: schemas,
		TablesScanned:  tables,
		FieldsScanned:  fields,
		DurationMs:     durationMs,
		StartedAt:      startTime.Format("2006-01-02 15:04:05"),
	}, nil
}

func (s *ScanService) createImmediateRunRecord(resource *commonModels.Engine, tenantID uint, schemaNames, objectPaths []string, startTime time.Time) (*models.ScanTaskRun, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required to create immediate run")
	}

	params := models.JSONMap{}
	if len(schemaNames) > 0 {
		params["schema_names"] = schemaNames
	}
	if len(objectPaths) > 0 {
		params["object_paths"] = objectPaths
	}
	if len(params) == 0 {
		params = nil
	}

	run := &models.ScanTaskRun{
		TenantID:        tenantID,
		EngineID:      resource.ID,
		StorageType:     normalizeStorageType(resource.EngineType),
		TriggerType:     triggerTypeManual,
		Status:          runStatusRunning,
		Parameters:      params,
		ProgressMessage: "任务开始执行",
		StartedAt:       &startTime,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := s.db.Create(run).Error; err != nil {
		return nil, fmt.Errorf("failed to create immediate run record: %w", err)
	}

	setRunName(s.db, run, run.StorageType, triggerTypeManual, s.log)
	return run, nil
}

func (s *ScanService) failImmediateRun(run *models.ScanTaskRun, scanErr error) {
	if run == nil {
		return
	}

	// 计算执行耗时
	completedAt := time.Now()
	var durationMs int64
	if run.StartedAt != nil {
		durationMs = completedAt.Sub(*run.StartedAt).Milliseconds()
	}

	update := map[string]interface{}{
		"status":           runStatusFailed,
		"error_message":    scanErr.Error(),
		"progress_message": fmt.Sprintf("执行失败: %v", scanErr),
		"duration_ms":      durationMs,
		"completed_at":     completedAt,
		"updated_at":       time.Now(),
	}
	if err := s.db.Model(&models.ScanTaskRun{}).Where("id = ?", run.ID).Updates(update).Error; err != nil {
		s.log.Error("更新即时运行失败状态出错", "run_id", run.ID, "error", err)
	}
}

func (s *ScanService) completeImmediateRun(run *models.ScanTaskRun, summary models.JSONMap, completedAt time.Time) {
	if run == nil {
		return
	}

	// 计算执行耗时
	var durationMs int64
	if run.StartedAt != nil {
		durationMs = completedAt.Sub(*run.StartedAt).Milliseconds()
	}

	update := map[string]interface{}{
		"status":           runStatusSuccess,
		"result_summary":   summary,
		"duration_ms":      durationMs,
		"progress_message": "执行完成",
		"progress_percent": 100.0,
		"completed_at":     completedAt,
		"updated_at":       time.Now(),
	}

	if err := s.db.Model(&models.ScanTaskRun{}).Where("id = ?", run.ID).Updates(update).Error; err != nil {
		s.log.Error("更新即时运行成功状态出错", "run_id", run.ID, "error", err)
		return
	}

	// 发布扫描完成事件（异步，不阻塞主流程）
	s.publishScanCompletedEvent(run.EngineID, run.TenantID, summary)
}

// scanResource 扫描单个资源的所有未扫描Schema
func (s *ScanService) scanResource(resource *commonModels.Engine, tenantID uint, scanLogID uint) (int, int, int, error) {
	engineID := resource.ID

	startFields := append(connectionLogFields(resource),
		"scan_log_id", scanLogID,
		"mode", "auto",
	)
	s.log.Info("开始扫描资源", startFields...)

	// 检查是否为对象存储类型
	if isObjectStorageType(strings.ToLower(resource.EngineType)) {
		// 使用 ObjectStoragePlugin 扫描
		p, err := plugin.Get(resource.EngineType)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
		}

		objPlugin, ok := p.(plugin.ObjectStoragePlugin)
		if !ok {
			return 0, 0, 0, fmt.Errorf("engine %s does not implement ObjectStoragePlugin", resource.EngineType)
		}

		// 列出所有 buckets
		buckets, err := objPlugin.ListBuckets(context.Background(), plugin.ConnectionInfo(resource.ConnectionInfo))
		if err != nil {
			return 0, 0, 0, fmt.Errorf("failed to list buckets: %w", err)
		}

		if len(buckets) == 0 {
			s.log.Info("对象存储资源未配置可扫描桶，跳过扫描", cloneLogFields(startFields, "allowed_bucket_count", 0)...)
			return 0, 0, 0, nil
		}

		// 构建扫描路径列表
		var paths []string
		for _, b := range buckets {
			paths = append(paths, b.Name)
		}
		sort.Strings(paths)

		s.log.Info("对象存储资源扫描开始", cloneLogFields(startFields, "bucket_count", len(buckets), "buckets", paths)...)

		// 调用 ObjectStorageScanService 进行扫描
		totalBuckets, totalObjects, err := s.objectScanService.ScanPaths(
			resource,
			tenantID,
			paths,
			nil,
			"deep",
			nil,
		)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("object storage scan failed: %w", err)
		}

		s.log.Info("对象存储资源扫描完成", cloneLogFields(startFields,
			"buckets_scanned", totalBuckets,
			"objects_scanned", totalObjects,
		)...)
		return totalBuckets, totalObjects, 0, nil
	}

	// 数据库扫描：直接使用 RelationalDBPlugin
	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	relPlugin, ok := p.(plugin.RelationalDBPlugin)
	if !ok {
		return 0, 0, 0, fmt.Errorf("engine %s does not implement RelationalDBPlugin", resource.EngineType)
	}

	db, err := plugin.GetOrCreatePoolFromFactory(&plugin.Engine{
		ID:             resource.ID,
		EngineType:     resource.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(resource.ConnectionInfo),
	}, nil)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to create connection pool: %w", err)
	}

	schemasInfo, err := relPlugin.ListSchemas(context.Background(), db)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to list schemas: %w", err)
	}

	totalSchemas := 0
	totalTables := 0
	totalFields := 0

	s.log.Info("数据库资源扫描开始", cloneLogFields(startFields, "schema_total", len(schemasInfo))...)

	// 记录本次扫描到的 schema
	scannedSchemas := make(map[string]bool)

	for _, schemaInfo := range schemasInfo {
		// 过滤系统 schema
		if relPlugin.IsSystemSchema(schemaInfo.Name) {
			s.log.Debug("跳过系统 schema", "schema", schemaInfo.Name)
			continue
		}

		scannedSchemas[schemaInfo.Name] = true

		var node models.MetaNode
		err := s.db.Where("tenant_id = ? AND engine_id = ? AND node_type = ? AND name = ?",
			tenantID, engineID, "schema", schemaInfo.Name).First(&node).Error

		// 无论 schema 是否已存在,都进行扫描以确保元数据同步
		// 这样可以触发表的增量更新和过期表的清理
		if err == gorm.ErrRecordNotFound || err == nil {
			schemas, tables, fields, err := s.dbScanService.ScanSchema(context.Background(), resource, tenantID, engineID, schemaInfo.Name, "deep")
			if err != nil {
				s.log.Warn("Schema 扫描失败",
					"engine_id", engineID,
					"tenant_id", tenantID,
					"schema", schemaInfo.Name,
					"error", err,
				)
				continue
			}
			totalSchemas += schemas
			totalTables += tables
			totalFields += fields
		} else {
			s.log.Warn("查询 Schema 节点失败",
				"engine_id", engineID,
				"tenant_id", tenantID,
				"schema", schemaInfo.Name,
				"error", err,
			)
		}
	}

	// 软删除那些不再存在的 schema
	var existingSchemas []models.MetaNode
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND node_type = ?",
		tenantID, engineID, "schema").Find(&existingSchemas).Error; err != nil {
		s.log.Warn("查询已存在 schema 节点失败", "error", err)
	} else {
		s.log.Info("开始检查需要清理的 schema",
			"engine_id", engineID,
			"existing_count", len(existingSchemas),
			"scanned_count", len(scannedSchemas),
		)
		for _, schemaNode := range existingSchemas {
			if !scannedSchemas[schemaNode.Name] {
				s.log.Info("Schema 已不存在，标记删除",
					"engine_id", engineID,
					"schema", schemaNode.Name,
				)
				if err := s.db.Delete(&schemaNode).Error; err != nil {
					s.log.Warn("软删除 schema 节点失败",
						"schema", schemaNode.Name,
						"error", err,
					)
				} else {
					// 同时软删除该 schema 下的所有表
					if err := s.db.Where("node_id = ?", schemaNode.ID).Delete(&models.MetaItem{}).Error; err != nil {
						s.log.Warn("软删除 schema 下的表失败",
							"schema", schemaNode.Name,
							"error", err,
						)
					}
				}
			}
		}
	}

	s.log.Info("数据库资源扫描完成", cloneLogFields(startFields,
		"schemas_scanned", totalSchemas,
		"tables_scanned", totalTables,
		"fields_scanned", totalFields,
	)...)

	return totalSchemas, totalTables, totalFields, nil
}

// scanResourceSchemas 扫描资源的指定Schema列表
func (s *ScanService) scanResourceSchemas(resource *commonModels.Engine, tenantID uint, schemaNames []string, scanLogID uint, scanDepth string) (int, int, int, error) {
	return s.scanResourceSchemasWithReporter(resource, tenantID, schemaNames, scanLogID, scanDepth, nil)
}

func (s *ScanService) scanResourceSchemasWithReporter(resource *commonModels.Engine, tenantID uint, schemaNames []string, scanLogID uint, scanDepth string, reporter ScanProgressReporter) (int, int, int, error) {
	resourceID := resource.ID

	startFields := append(connectionLogFields(resource),
		"scan_log_id", scanLogID,
		"mode", "manual",
	)
	if len(schemaNames) > 0 {
		startFields = append(startFields, "target_schemas", schemaNames)
	}
	s.log.Info("开始扫描指定 Schema 列表", startFields...)

	// 如果未指定Schema，则扫描所有Schema（自动过滤系统schema）
	if len(schemaNames) == 0 {
		if reporter != nil {
			reporter.Message("未指定 Schema，正在获取完整列表")
		}

		// 获取插件
		p, err := plugin.Get(resource.EngineType)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
		}

		relPlugin, ok := p.(plugin.RelationalDBPlugin)
		if !ok {
			return 0, 0, 0, fmt.Errorf("engine %s does not implement RelationalDBPlugin", resource.EngineType)
		}

		db, err := plugin.GetOrCreatePoolFromFactory(&plugin.Engine{
			ID:             resource.ID,
			EngineType:     resource.EngineType,
			ConnectionInfo: plugin.ConnectionInfo(resource.ConnectionInfo),
		}, nil)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("failed to create connection pool: %w", err)
		}

		schemasInfo, err := relPlugin.ListSchemas(context.Background(), db)
		if err != nil {
			return 0, 0, 0, err
		}

		for _, info := range schemasInfo {
			// 使用插件的 IsSystemSchema 方法过滤系统 schema
			if relPlugin.IsSystemSchema(info.Name) {
				s.log.Debug("跳过系统 schema", "schema", info.Name)
				continue
			}
			schemaNames = append(schemaNames, info.Name)
		}

		if reporter != nil {
			reporter.Message(fmt.Sprintf("已过滤系统 schema，待扫描 %d 个用户 schema", len(schemaNames)))
		}
	}

	totalSchemas := 0
	totalTables := 0
	totalFields := 0
	total := len(schemaNames)
	if reporter != nil {
		reporter.SetTotal(total)
	}
	completed := 0

	for _, schemaName := range schemaNames {
		if reporter != nil {
			reporter.Message(fmt.Sprintf("开始扫描 Schema %s", schemaName))
		}

		// 检查Schema级锁
		ctx := context.Background()
		var schemaLock string
		if s.dedupService != nil {
			schemaLock = s.dedupService.GenerateSchemaLockKey(tenantID, resourceID, schemaName)
			if s.dedupService.CheckTaskExists(ctx, schemaLock) {
				s.log.Info("Schema正在扫描中，跳过",
					"engine_id", resourceID,
					"schema", schemaName)
				if reporter != nil {
					reporter.Message(fmt.Sprintf("Schema %s 正在扫描中，跳过", schemaName))
				}
				completed++
				continue
			}

			// 加Schema级锁
			if err := s.dedupService.MarkTaskRunning(ctx, schemaLock, 2*time.Hour); err != nil {
				s.log.Warn("加Schema级锁失败", "schema", schemaName, "error", err)
			}
		}

		// 扫描Schema
		schemas, tables, fields, err := s.dbScanService.ScanSchema(context.Background(), resource, tenantID, resourceID, schemaName, scanDepth)

		// 扫描完成后清理锁
		if s.dedupService != nil && schemaLock != "" {
			if clearErr := s.dedupService.ClearTask(context.Background(), schemaLock); clearErr != nil {
				s.log.Warn("清除Schema级锁失败", "schema", schemaName, "error", clearErr)
			}
		}

		if err != nil {
			s.log.Warn("Schema 扫描失败",
				"engine_id", resourceID,
				"tenant_id", tenantID,
				"schema", schemaName,
				"error", err,
			)
			if reporter != nil {
				reporter.Message(fmt.Sprintf("Schema %s 扫描失败: %v", schemaName, err))
			}
			continue
		}
		totalSchemas += schemas
		totalTables += tables
		totalFields += fields

		completed++
		if reporter != nil {
			reporter.Advance(schemaName, completed, total, map[string]interface{}{
				"tables": tables,
				"fields": fields,
			})
		}
	}

	s.log.Info("指定 Schema 扫描完成", cloneLogFields(startFields,
		"schemas_scanned", totalSchemas,
		"tables_scanned", totalTables,
		"fields_scanned", totalFields,
	)...)

	return totalSchemas, totalTables, totalFields, nil
}

// scanNoSQLResourceWithReporter 扫描 NoSQL 资源的指定数据库列表（带进度报告）
func (s *ScanService) scanNoSQLResourceWithReporter(
	nosqlPlugin plugin.NoSQLPlugin,
	resource *commonModels.Engine,
	tenantID uint,
	databaseNames []string,
	scanDepth string,
	reporter ScanProgressReporter,
) (int, int, int, error) {

	resourceID := resource.ID
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	ctx := context.Background()

	startFields := append(connectionLogFields(resource),
		"mode", "manual",
		"scan_depth", scanDepth,
	)

	// 如果未指定数据库，列出所有数据库
	if len(databaseNames) == 0 {
		if reporter != nil {
			reporter.Message("列出所有数据库...")
		}

		databasesInfo, err := nosqlPlugin.ListDatabases(ctx, connInfo)
		if err != nil {
			return 0, 0, 0, err
		}

		// 过滤系统数据库
		for _, info := range databasesInfo {
			if nosqlPlugin.IsSystemDatabase(info.Name) {
				s.log.Debug("跳过系统数据库", "database", info.Name)
				continue
			}
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
			ctx, nosqlPlugin, resource, tenantID, databaseName, scanDepth,
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

// scanSingleSchema 扫描单个Schema（表+字段）
func (s *ScanService) scanObjectStorageResource(resource *commonModels.Engine, tenantID uint, objectPaths, fallback []string) (int, int, int, error) {
	return s.scanObjectStorageResourceWithReporter(resource, tenantID, objectPaths, fallback, "deep", nil)
}

func (s *ScanService) scanObjectStorageResourceWithReporter(resource *commonModels.Engine, tenantID uint, objectPaths, fallback []string, scanDepth string, reporter ScanProgressReporter) (int, int, int, error) {
	// 标准化 scanDepth
	if scanDepth == "" {
		scanDepth = "deep"
	}

	// ✅ 重构后：直接使用 ObjectStoragePlugin
	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	objPlugin, ok := p.(plugin.ObjectStoragePlugin)
	if !ok {
		return 0, 0, 0, fmt.Errorf("engine %s does not implement ObjectStoragePlugin", resource.EngineType)
	}

	// 准备扫描路径
	var paths []string
	if len(objectPaths) > 0 {
		paths = objectPaths
	} else if len(fallback) > 0 {
		paths = fallback
	} else {
		// 如果未指定路径，列出所有 buckets
		buckets, err := objPlugin.ListBuckets(context.Background(), plugin.ConnectionInfo(resource.ConnectionInfo))
		if err != nil {
			return 0, 0, 0, fmt.Errorf("failed to list buckets: %w", err)
		}
		for _, b := range buckets {
			paths = append(paths, b.Name)
		}
	}

	if len(paths) == 0 {
		if reporter != nil {
			reporter.Message("未检测到可扫描的对象路径")
			reporter.SetTotal(0)
		}
		return 0, 0, 0, nil
	}

	if reporter != nil {
		reporter.SetTotal(len(paths))
	}

	// 调用 ObjectStorageScanService 进行扫描
	buckets, objects, err := s.objectScanService.ScanPaths(
		resource,
		tenantID,
		paths,
		nil,
		scanDepth,
		reporter,
	)
	if err != nil {
		return 0, 0, 0, err
	}

	return buckets, objects, 0, nil
}

// persistObjectMetas 持久化对象元数据到数据库
//
// 职责划分：
// 1. 目录树构建：根据对象路径构建层级目录节点
// 2. 对象元数据持久化：保存对象的基本信息和增强元数据
// 3. 文档向量化：为支持的文档类型生成向量嵌入（如果启用）
// 4. 搜索索引更新：将对象信息同步到Meilisearch
// 5. 统计聚合：更新各层级节点的统计信息（对象数、总大小）
//
// 参数：
//   - resource: 数据源引擎配置
//   - tenantID: 租户ID
//   - engineID: 引擎ID
//   - bucketNode: Bucket节点
//   - metas: 对象元数据列表
//   - stats: 节点统计聚合map
//   - includeBucketAggregate: 是否包含bucket级别的聚合
//   - scanDepth: 扫描深度
//   - scanPathPrefix: 扫描路径前缀
//   - scannedFingerprints: 已扫描对象的指纹集合
//
// 返回：(持久化对象数量, error)
func (s *ScanService) persistObjectMetas(resource *commonModels.Engine, tenantID, engineID uint, bucketNode *models.MetaNode, metas []format.ObjectMetadata, stats map[uint]*nodeAggregate, includeBucketAggregate bool, scanDepth string, scanPathPrefix string, scannedFingerprints map[string]bool) (int, error) {
	// 委托给 ObjectStorageScanService
	return s.objectScanService.persistObjectMetas(resource, tenantID, engineID, bucketNode, metas, stats, includeBucketAggregate, scanDepth, scanPathPrefix, scannedFingerprints)
}

// extractEnhancedMetadata 使用插件提取增强的元数据 (代理到 metadataExtractor)
func (s *ScanService) extractEnhancedMetadata(engineID uint, meta format.ObjectMetadata, baseAttrs models.JSONMap) models.JSONMap {
	return s.metadataExtractor.ExtractEnhancedMetadata(engineID, meta, baseAttrs)
}

// extractEnhancedMetadataWithCache 带缓存检查的元数据提取 (代理到 metadataExtractor)
func (s *ScanService) extractEnhancedMetadataWithCache(engineID uint, meta format.ObjectMetadata, baseAttrs models.JSONMap, fullPath string) models.JSONMap {
	return s.metadataExtractor.ExtractEnhancedMetadataWithCache(engineID, meta, baseAttrs, fullPath)
}

// GetObjectMetadata 获取指定对象的元数据 (代理到 metadataExtractor)
func (s *ScanService) GetObjectMetadata(tenantID, engineID uint, objectKey string) (*models.MetaItem, error) {
	return s.metadataExtractor.GetObjectMetadata(tenantID, engineID, objectKey)
}

// ExtractObjectMetadataOnDemand 按需提取对象的深度元数据 (代理到 metadataExtractor)
func (s *ScanService) ExtractObjectMetadataOnDemand(tenantID, engineID uint, objectKey string, token string, objectReader io.Reader) (*format.ExtractedMetadata, error) {
	return s.metadataExtractor.ExtractObjectMetadataOnDemand(tenantID, engineID, objectKey, token, objectReader)
}

func filterObjectMetasForDepth(metas []format.ObjectMetadata, basePath string) []format.ObjectMetadata {
	base := sanitizeObjectPath(basePath)
	if len(metas) == 0 {
		return metas
	}

	filtered := make([]format.ObjectMetadata, 0, len(metas))
	for _, meta := range metas {
		if meta.NodeType == "bucket" {
			filtered = append(filtered, meta)
			continue
		}

		relative := sanitizeObjectPath(meta.RelativePath)
		trimmed := relative
		if base != "" {
			switch {
			case trimmed == base:
				trimmed = ""
			case strings.HasPrefix(trimmed, base+"/"):
				trimmed = strings.TrimPrefix(trimmed, base+"/")
			}
		}

		switch strings.ToLower(meta.NodeType) {
		case "prefix":
			if trimmed == "" || !strings.Contains(trimmed, "/") {
				filtered = append(filtered, meta)
			}
		case "object":
			if trimmed != "" && strings.Contains(trimmed, "/") {
				continue
			}
			filtered = append(filtered, meta)
		default:
			filtered = append(filtered, meta)
		}
	}
	return filtered
}

func (s *ScanService) clearObjectMetadataUnderPath(tenantID, engineID uint, bucketNode *models.MetaNode, bucketName, relativePath string) error {
	clean := sanitizeObjectPath(relativePath)
	if s.indexerService != nil {
		s.indexerService.DeleteObjectsFromIndex(tenantID, engineID, bucketName, clean)
	}
	if clean == "" {
		if err := s.hardDeleteDescendantNodes(bucketNode); err != nil {
			return err
		}
		return s.hardDeleteItemsByNode(bucketNode.ID)
	}

	var targetNodes []models.MetaNode
	if err := s.db.
		Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).
		Where("node_type = ?", "prefix").
		Where("(attributes ->> 'bucket') = ?", bucketName).
		Where("(attributes ->> 'path') = ? OR (attributes ->> 'path') LIKE ?", clean, clean+"/%").
		Find(&targetNodes).Error; err != nil {
		return fmt.Errorf("failed to query prefix nodes for cleanup: %w", err)
	}

	if len(targetNodes) > 0 {
		ids := make([]uint, 0, len(targetNodes))
		for _, node := range targetNodes {
			ids = append(ids, node.ID)
		}

		if err := s.db.Unscoped().Where("node_id IN ?", ids).Delete(&models.MetaItem{}).Error; err != nil {
			return fmt.Errorf("failed to delete object items for prefix: %w", err)
		}

		if err := s.db.Unscoped().Where("id IN ?", ids).Delete(&models.MetaNode{}).Error; err != nil {
			return fmt.Errorf("failed to delete prefix nodes: %w", err)
		}
	}

	if err := s.db.Unscoped().
		Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).
		Where("(attributes ->> 'bucket') = ?", bucketName).
		Where("(attributes ->> 'relative_path') = ? OR (attributes ->> 'relative_path') LIKE ?", clean, clean+"/%").
		Delete(&models.MetaItem{}).Error; err != nil {
		return fmt.Errorf("failed to delete object items by relative path: %w", err)
	}

	return nil
}

// ============================================================================
// 资源发现接口（委托给 ResourceDiscoveryService）
// ============================================================================

// GetSchemasByResource 获取资源的所有Schema（用于Manager模块）
func (s *ScanService) GetSchemasByResource(engineID, tenantID uint) ([]*models.SchemaWithStatus, error) {
	return s.resourceDiscoveryService.GetSchemasByResource(engineID, tenantID)
}

// ListAvailableSchemas 列出资源中可用的Schema（从数据库实时查询）
func (s *ScanService) ListAvailableSchemas(engineID, tenantID uint, token string) ([]*models.SchemaInfo, error) {
	return s.resourceDiscoveryService.ListAvailableSchemas(engineID, tenantID, token)
}

// ListObjectStorageNodes 列出对象存储的节点结构（用于Manager模块）
func (s *ScanService) ListObjectStorageNodes(engineID, tenantID uint, path, token string) ([]*models.ObjectNode, error) {
	return s.resourceDiscoveryService.ListObjectStorageNodes(engineID, tenantID, path, token)
}

// ============================================================================
// 元数据查询接口（委托给 MetadataQueryService）
// ============================================================================

// GetTablesByResource 获取资源下所有的表（用于Transfer模块）
func (s *ScanService) GetTablesByResource(engineID, tenantID uint) ([]models.MetaItem, error) {
	return s.metadataQueryService.GetTablesByResource(engineID, tenantID)
}

// GetTableFields 获取表的字段名列表（用于Transfer模块）
func (s *ScanService) GetTableFields(engineID uint, tableName string, tenantID uint) ([]string, error) {
	return s.metadataQueryService.GetTableFields(engineID, tableName, tenantID)
}

// GetTableFieldDetails 获取表字段详细信息（支持空间字段识别）
func (s *ScanService) GetTableFieldDetails(engineID uint, tableName string, tenantID uint) ([]TableFieldInfo, error) {
	return s.metadataQueryService.GetTableFieldDetails(engineID, tableName, tenantID)
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

// GetTableSpatialMetadata 获取表的空间元数据（用于Manager模块）
func (s *ScanService) GetTableSpatialMetadata(tenantID, engineID uint, schema, table string) (*models.SpatialMetadataResponse, error) {
	return s.metadataQueryService.GetTableSpatialMetadata(tenantID, engineID, schema, table)
}

// GetMetaNodeByID 获取单个节点详情（用于Manager模块）
func (s *ScanService) GetMetaNodeByID(tenantID, nodeID uint) (*models.MetaNodeLite, error) {
	return s.metadataQueryService.GetMetaNodeByID(tenantID, nodeID)
}
