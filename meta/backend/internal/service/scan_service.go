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
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/meta/internal/config"
	metaErrors "github.com/addp/meta/internal/errors"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/search"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ScanService 统一扫描服务
type ScanService struct {
	db                       *gorm.DB
	repo                     *ScanRepository           // 数据访问层
	dbScanService            *DatabaseScanService      // 数据库扫描服务
	nosqlScanService         *NoSQLScanService         // NoSQL 数据库扫描服务
	objectScanService        *ObjectStorageScanService // 对象存储扫描服务
	fsScanService            *FileSystemScanService    // 文件系统扫描服务（湖表检测）
	metadataQueryService     *MetadataQueryService     // 元数据查询服务（独立）
	resourceDiscoveryService *ResourceDiscoveryService // 资源发现服务（独立）
	engineService            *EngineService
	config                   *config.Config
	log                      *slog.Logger
	indexer                  *search.Indexer
	indexerService           *IndexerService                     // 索引服务（独立）
	spatialService           *SpatialMetadataService             // 空间元数据服务（独立）
	scanEventPublisher       *events.ScanEventPublisher          // 扫描事件发布器
	metadataExtractor        *MetadataExtractor                  // 元数据提取器
	dedupService             *ScanDedupService                   // 扫描去重服务（可选）
	taskExecutionRepo        *commonRepo.TaskExecutionRepository // 统一执行记录仓库
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
	indexerService := NewIndexerService(nil, log)         // indexer 稍后通过 SetIndexer 注入
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
		taskExecutionRepo: commonRepo.NewTaskExecutionRepository(db),
	}

	// 创建 DatabaseScanService（使用独立服务，无循环依赖）
	s.dbScanService = NewDatabaseScanService(db, log, nil, repo, spatialService, indexerService)

	// 创建 NoSQLScanService（使用独立服务，无循环依赖）
	s.nosqlScanService = NewNoSQLScanService(db, log, nil, repo, indexerService)

	// 创建 ObjectStorageScanService（使用独立服务，无循环依赖）
	s.objectScanService = NewObjectStorageScanService(db, log, repo, metadataExtractor, indexerService)

	// 创建 FileSystemScanService（湖表检测）
	s.fsScanService = NewFileSystemScanService(db, log, repo, indexerService)

	// 创建 MetadataQueryService（提供元数据查询接口）
	s.metadataQueryService = NewMetadataQueryService(db, spatialService, engineService, log)

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

	// 启动时清理所有残留的扫描锁（防止上次服务异常退出时的锁未清理）
	if dedupService != nil {
		s.cleanupStaleScanLocks()
	}
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
			EngineID:          engineID,
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

func (s *ScanService) upsertNode(tenantID, engineID uint, parent *models.MetaNode, nodeType, name string, fullName *string, attrs models.JSONMap) (*models.MetaNode, error) {
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

	var directExecID string
	if reporter == nil {
		execID, createErr := s.createImmediateExecution(resource, tenantID, schemaNames, objectPaths, startTime)
		if createErr != nil {
			return nil, createErr
		}
		directExecID = execID
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

	schemas, tables, fields := 0, 0, 0

	// 获取插件以判断类型
	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	family := storageFamily(p)

	// 根据能力族路由到对应的扫描服务
	if family == "document" || family == "graph" {
		if _, ok := p.(plugin.CatalogProvider); !ok {
			err = fmt.Errorf("engine %s declares %s storage but does not implement CatalogProvider", resource.EngineType, family)
		} else {
			// NoSQL 扫描（MongoDB、CouchDB 等）
			schemas, tables, fields, err = s.scanNoSQLResourceWithReporter(p, resource, tenantID, schemaNames, scanDepth, reporter)
		}
	} else if family == "object" {
		// 对象存储扫描（MinIO、S3 等）—— 写入 bucket/prefix/object 语义节点
		schemas, tables, fields, err = s.scanObjectStorageResourceWithReporter(resource, tenantID, objectPaths, scanDepth, reporter)
	} else if family == "file" {
		// 文件系统扫描（NFS、HDFS 等）—— 写入 root/dir/file/lake_table 语义节点
		schemas, tables, fields, err = s.scanFileSystemResourceWithReporter(resource, tenantID, objectPaths, scanDepth, reporter)
	} else if family == "tabular" {
		// 关系型数据库扫描（PostgreSQL、MySQL 等）
		schemas, tables, fields, err = s.scanResourceSchemasWithReporter(resource, tenantID, schemaNames, 0, scanDepth, reporter)
	} else {
		err = fmt.Errorf("plugin does not support metadata query")
	}

	if err != nil {
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描失败: %v", err))
		}
		if directExecID != "" {
			s.failImmediateExecution(directExecID, int(tenantID), err, startTime)
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

	if directExecID != "" {
		resultMeta := commonModels.JSONMap{
			"schemas_scanned": schemas,
			"tables_scanned":  tables,
			"fields_scanned":  fields,
		}
		s.completeImmediateExecution(directExecID, int(tenantID), resultMeta, startTime, completedAt)
		s.publishScanCompletedEvent(resource.ID, tenantID, models.JSONMap(resultMeta))
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

func (s *ScanService) createImmediateExecution(resource *commonModels.Engine, tenantID uint, schemaNames, objectPaths []string, startTime time.Time) (string, error) {
	if resource == nil {
		return "", fmt.Errorf("resource is required to create immediate execution")
	}

	execID := uuid.New().String()
	engineIDInt := int(resource.ID)
	exec := &commonModels.TaskExecution{
		TenantID:    int(tenantID),
		ExecutionID: execID,
		Module:      commonModels.ModuleMeta,
		TaskType:    "scan",
		Status:      commonModels.ExecutionStatusRunning,
		TriggerType: commonModels.TriggerTypeAPI,
		ExecutionConfig: commonModels.JSONMap{
			"engine_id":    engineIDInt,
			"namespaces":   schemaNames,
			"object_paths": objectPaths,
		},
		StartedAt: &startTime,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.taskExecutionRepo.Create(context.Background(), exec); err != nil {
		return "", fmt.Errorf("failed to create immediate execution record: %w", err)
	}
	return execID, nil
}

func (s *ScanService) failImmediateExecution(execID string, tenantID int, scanErr error, startTime time.Time) {
	completedAt := time.Now()
	durationMs := completedAt.Sub(startTime).Milliseconds()
	_ = s.taskExecutionRepo.UpdateFields(context.Background(), execID, tenantID, map[string]interface{}{
		"status":            commonModels.ExecutionStatusFailed,
		"error_details":     commonModels.JSONMap{"message": scanErr.Error()},
		"execution_time_ms": durationMs,
		"completed_at":      completedAt,
		"updated_at":        time.Now(),
	})
}

func (s *ScanService) completeImmediateExecution(execID string, tenantID int, meta commonModels.JSONMap, startTime, completedAt time.Time) {
	durationMs := completedAt.Sub(startTime).Milliseconds()
	_ = s.taskExecutionRepo.UpdateFields(context.Background(), execID, tenantID, map[string]interface{}{
		"status":            commonModels.ExecutionStatusSuccess,
		"metadata":          meta,
		"execution_time_ms": durationMs,
		"progress":          100,
		"completed_at":      completedAt,
		"updated_at":        time.Now(),
	})
}

// scanResource 扫描单个资源的所有未扫描Schema
func (s *ScanService) scanResource(resource *commonModels.Engine, tenantID uint, scanLogID uint) (int, int, int, error) {
	engineID := resource.ID

	startFields := append(connectionLogFields(resource),
		"scan_log_id", scanLogID,
		"mode", "auto",
	)
	s.log.Info("开始扫描资源", startFields...)

	// 根据插件类型路由到对应的扫描服务
	p0, err0 := plugin.Get(resource.EngineType)
	if err0 != nil {
		return 0, 0, 0, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	family := storageFamily(p0)

	if family == "object" {
		// 对象存储类型（MinIO、S3 等）—— 写入 bucket/prefix/object 语义节点
		buckets, objects, _, err := s.scanObjectStorageResourceWithReporter(resource, tenantID, nil, "deep", nil)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("object storage scan failed: %w", err)
		}
		s.log.Info("对象存储资源扫描完成", cloneLogFields(startFields,
			"buckets_scanned", buckets,
			"objects_scanned", objects,
		)...)
		return buckets, objects, 0, nil
	}

	if family == "file" {
		// 文件系统类型（NFS 等）—— 写入 root/dir/file/lake_table 语义节点
		paths, err := s.listFileSystemRootPaths(resource, p0)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("failed to list roots: %w", err)
		}

		if len(paths) == 0 {
			s.log.Info("文件系统资源无可扫描根节点，跳过扫描", startFields...)
			return 0, 0, 0, nil
		}
		sort.Strings(paths)

		s.log.Info("文件系统资源扫描开始", cloneLogFields(startFields, "root_count", len(paths), "roots", paths)...)

		totalRoots, totalItems, err := s.fsScanService.ScanPaths(resource, tenantID, paths, nil)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("filesystem scan failed: %w", err)
		}

		s.log.Info("文件系统资源扫描完成", cloneLogFields(startFields,
			"roots_scanned", totalRoots,
			"items_scanned", totalItems,
		)...)
		return totalRoots, totalItems, 0, nil
	}

	schemasInfo, err := s.listSchemaInfos(resource, p0)
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

func (s *ScanService) listFileSystemRootPaths(resource *commonModels.Engine, p plugin.EnginePlugin) ([]string, error) {
	if catalogProvider, ok := p.(plugin.CatalogProvider); ok {
		nodes, err := catalogProvider.ListChildren(context.Background(), plugin.ConnectionInfo(resource.ConnectionInfo), plugin.CatalogPath{
			Version:  plugin.CatalogPathVersion,
			EngineID: resource.ID,
		}, plugin.ListOptions{})
		if err != nil {
			return nil, err
		}
		paths := make([]string, 0, len(nodes))
		for _, node := range nodes {
			if raw, ok := node.Attributes["path"].(string); ok && raw != "" {
				paths = append(paths, raw)
				continue
			}
			paths = append(paths, node.Path.StringPath())
		}
		return paths, nil
	}
	return nil, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
}

func (s *ScanService) listSchemaInfos(resource *commonModels.Engine, p plugin.EnginePlugin) ([]plugin.SchemaInfo, error) {
	if catalogProvider, ok := p.(plugin.CatalogProvider); ok {
		nodes, err := catalogProvider.ListChildren(context.Background(), plugin.ConnectionInfo(resource.ConnectionInfo), plugin.CatalogPath{
			Version:  plugin.CatalogPathVersion,
			EngineID: resource.ID,
		}, plugin.ListOptions{})
		if err != nil {
			return nil, err
		}
		schemas := make([]plugin.SchemaInfo, 0, len(nodes))
		for _, node := range nodes {
			tableCount := 0
			if count, ok := int64Stat(node.Stats, "table_count"); ok {
				tableCount = int(count)
			}
			schemas = append(schemas, plugin.SchemaInfo{
				Name:       node.Name,
				TableCount: tableCount,
			})
		}
		return schemas, nil
	}
	return nil, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
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

		schemasInfo, err := s.listSchemaInfos(resource, p)
		if err != nil {
			return 0, 0, 0, err
		}

		for _, info := range schemasInfo {
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
		// 使用匿名函数包装每次循环，确保 defer 在每次循环结束时执行
		func(schema string) {
			if reporter != nil {
				reporter.Message(fmt.Sprintf("开始扫描 Schema %s", schema))
			}

			// 检查Schema级锁
			ctx := context.Background()
			var schemaLock string
			lockAcquired := false

			if s.dedupService != nil {
				schemaLock = s.dedupService.GenerateSchemaLockKey(tenantID, resourceID, schema)
				if s.dedupService.CheckTaskExists(ctx, schemaLock) {
					s.log.Info("Schema正在扫描中，跳过",
						"engine_id", resourceID,
						"schema", schema)
					if reporter != nil {
						reporter.Message(fmt.Sprintf("Schema %s 正在扫描中，跳过", schema))
					}
					completed++
					return
				}

				// 加Schema级锁
				if err := s.dedupService.MarkTaskRunning(ctx, schemaLock, 2*time.Hour); err != nil {
					s.log.Warn("加Schema级锁失败", "schema", schema, "error", err)
				} else {
					lockAcquired = true
				}

				// 使用 defer 确保锁在任何情况下都会被清理（包括 panic）
				defer func() {
					if lockAcquired && schemaLock != "" {
						if clearErr := s.dedupService.ClearTask(context.Background(), schemaLock); clearErr != nil {
							s.log.Warn("清除Schema级锁失败", "schema", schema, "error", clearErr)
						}
					}
				}()
			}

			// 扫描Schema
			schemas, tables, fields, err := s.dbScanService.ScanSchema(context.Background(), resource, tenantID, resourceID, schema, scanDepth)

			if err != nil {
				s.log.Warn("Schema 扫描失败",
					"engine_id", resourceID,
					"tenant_id", tenantID,
					"schema", schema,
					"error", err,
				)
				if reporter != nil {
					reporter.Message(fmt.Sprintf("Schema %s 扫描失败: %v", schema, err))
				}
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

	s.log.Info("指定 Schema 扫描完成", cloneLogFields(startFields,
		"schemas_scanned", totalSchemas,
		"tables_scanned", totalTables,
		"fields_scanned", totalFields,
	)...)

	return totalSchemas, totalTables, totalFields, nil
}

// scanNoSQLResourceWithReporter 扫描 NoSQL 资源的指定数据库列表（带进度报告）
func (s *ScanService) scanNoSQLResourceWithReporter(
	enginePlugin plugin.EnginePlugin,
	resource *commonModels.Engine,
	tenantID uint,
	databaseNames []string,
	scanDepth string,
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

		databasesInfo, err := s.listNoSQLDatabases(ctx, resource, catalogProvider)
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
			ctx, enginePlugin, resource, tenantID, databaseName, scanDepth,
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

func (s *ScanService) listNoSQLDatabases(ctx context.Context, resource *commonModels.Engine, catalogProvider plugin.CatalogProvider) ([]plugin.DatabaseInfo, error) {
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: resource.ID,
	}, plugin.ListOptions{})
	if err != nil {
		return nil, err
	}
	databases := make([]plugin.DatabaseInfo, 0, len(nodes))
	for _, node := range nodes {
		if !node.IsContainer {
			continue
		}
		sizeBytes, _ := int64Stat(node.Stats, "size_bytes")
		databases = append(databases, plugin.DatabaseInfo{
			Name:      node.Name,
			SizeBytes: sizeBytes,
		})
	}
	return databases, nil
}

// scanSingleSchema 扫描单个Schema（表+字段）
func (s *ScanService) scanObjectStorageResource(resource *commonModels.Engine, tenantID uint, objectPaths []string) (int, int, int, error) {
	return s.scanObjectStorageResourceWithReporter(resource, tenantID, objectPaths, "deep", nil)
}

// scanFileSystemResourceWithReporter 扫描文件系统资源（湖表检测 + 对象存储回退）
func (s *ScanService) scanFileSystemResourceWithReporter(resource *commonModels.Engine, tenantID uint, objectPaths []string, scanDepth string, reporter ScanProgressReporter) (int, int, int, error) {
	roots, items, err := s.fsScanService.ScanPaths(resource, tenantID, objectPaths, reporter)
	if err != nil {
		return 0, 0, 0, err
	}
	return roots, items, 0, nil
}

func (s *ScanService) scanObjectStorageResourceWithReporter(resource *commonModels.Engine, tenantID uint, objectPaths []string, scanDepth string, reporter ScanProgressReporter) (int, int, int, error) {
	// 标准化 scanDepth
	if scanDepth == "" {
		scanDepth = "deep"
	}

	if reporter != nil && len(objectPaths) > 0 {
		reporter.SetTotal(len(objectPaths))
	}

	// 调用 ObjectStorageScanService 进行扫描
	buckets, objects, err := s.objectScanService.ScanPaths(
		resource,
		tenantID,
		objectPaths,
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

		relative := sanitizeObjectPath(meta.Path)
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

func (s *ScanService) clearObjectMetadataUnderPath(tenantID, engineID uint, bucketNode *models.MetaNode, bucketName, path string) error {
	clean := sanitizeObjectPath(path)
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

// ListObjectStorageNodes 列出对象存储的节点结构（用于Manager模块）
func (s *ScanService) ListObjectStorageNodes(engineID, tenantID uint, path, token string) ([]*models.ObjectNode, error) {
	return s.resourceDiscoveryService.ListObjectStorageNodes(engineID, tenantID, path, token)
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

func storageFamily(p plugin.EnginePlugin) string {
	if p == nil {
		return ""
	}
	caps := p.Capabilities()
	if caps.Storage == nil {
		return ""
	}
	for _, family := range caps.Storage.Families {
		normalized := strings.ToLower(family)
		switch normalized {
		case "object", "file", "tabular", "document", "graph":
			return normalized
		}
	}
	return ""
}
