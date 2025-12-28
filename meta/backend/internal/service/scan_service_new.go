package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	pathpkg "path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/addp/common/embedding"
	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/vectorstore"
	metaErrors "github.com/addp/meta/internal/errors"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/plugins"
	_ "github.com/addp/meta/plugins/extractors" // 自动注册提取器
	"github.com/addp/meta/internal/search"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/gorm"
)

// ScanServiceNew 新的统一扫描服务
type ScanServiceNew struct {
	db                 *gorm.DB
	resourceService    *ResourceService
	log                *slog.Logger
	indexer            *search.Indexer
	vectorStore        *vectorstore.PgVectorStore
	multiModalEmbedder embedding.MultiModalEmbedder
	embeddingTimeout   time.Duration
	objectClients      map[uint]*minio.Client
	objectClientMu     sync.Mutex
	scanEventPublisher *events.ScanEventPublisher // 扫描事件发布器
}

const (
	documentPreviewRuneLimit   = 400
	documentContentRuneLimit   = 20000
	documentEmbeddingRuneLimit = 3500
	maxImageEmbeddingBytes     = 3 * 1024 * 1024
	maxVideoEmbeddingBytes     = 10 * 1024 * 1024
)

// ScanProgressReporter 用于在长时间扫描任务中更新进度
type ScanProgressReporter interface {
	SetTotal(total int)
	Advance(label string, completed, total int, meta map[string]interface{})
	Message(message string)
}

func NewScanServiceNew(db *gorm.DB, resourceService *ResourceService) *ScanServiceNew {
	if resourceService == nil {
		resourceService = NewResourceService(db, "", "", nil) // nil Redis client for fallback
	}

	return &ScanServiceNew{
		db:               db,
		resourceService:  resourceService,
		log:              logger.With("component", "scan_service"),
		embeddingTimeout: 20 * time.Second,
		objectClients:    make(map[uint]*minio.Client),
	}
}

// SetIndexer 注入搜索索引器
func (s *ScanServiceNew) SetIndexer(indexer *search.Indexer) {
	s.indexer = indexer
}

// SetScanEventPublisher 注入扫描事件发布器
func (s *ScanServiceNew) SetScanEventPublisher(publisher *events.ScanEventPublisher) {
	s.scanEventPublisher = publisher
}

// EnableDocumentVectorization 为文档扫描启用向量化
func (s *ScanServiceNew) EnableDocumentVectorization(store *vectorstore.PgVectorStore, embedder embedding.MultiModalEmbedder, timeout time.Duration) {
	s.vectorStore = store
	s.multiModalEmbedder = embedder
	if timeout > 0 {
		s.embeddingTimeout = timeout
	}
}

// DisableDocumentVectorization 关闭文档向量化能力
func (s *ScanServiceNew) DisableDocumentVectorization() {
	s.vectorStore = nil
	s.multiModalEmbedder = nil
}

// verifyResourceAccess 验证租户是否有权限访问资源
// 返回 nil 表示有权限，返回错误表示无权限或资源不存在
func (s *ScanServiceNew) verifyResourceAccess(engineID, tenantID uint, token string) error {
	// 通过 ResourceService 获取资源（内部已包含租户校验）
	resource, err := s.resourceService.GetResourceByID(engineID, tenantID, token)
	if err != nil {
		s.log.Warn("资源访问验证失败",
			"engine_id", engineID,
			"tenant_id", tenantID,
			"error", err)
		return metaErrors.ErrResourceAccessDenied
	}

	// 非超级管理员（tenant_id > 0）必须验证租户匹配
	if tenantID > 0 && (resource.TenantID == nil || *resource.TenantID != tenantID) {
		s.log.Warn("跨租户访问被拒绝",
			"engine_id", engineID,
			"resource_tenant_id", resource.TenantID,
			"request_tenant_id", tenantID)
		return metaErrors.ErrResourceAccessDenied
	}

	return nil
}

// publishScanCompletedEvent 发布扫描完成事件（异步）
func (s *ScanServiceNew) publishScanCompletedEvent(engineID, tenantID uint, summary models.JSONMap) {
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

func composeNodePath(nodeID uint, parent *models.MetaNode) string {
	current := fmt.Sprintf("%d", nodeID)
	if parent == nil || parent.Path == "" {
		if parent == nil {
			return current
		}
		return fmt.Sprintf("%d/%s", parent.ID, current)
	}
	return fmt.Sprintf("%s/%s", parent.Path, current)
}

func composeNodeFullName(name string, parent *models.MetaNode, separator string) string {
	if parent == nil || parent.FullName == "" {
		return name
	}
	if separator == "" {
		separator = "."
	}
	return fmt.Sprintf("%s%s%s", parent.FullName, separator, name)
}

type nodeAggregate struct {
	node      *models.MetaNode
	itemCount int
	totalSize int64
}

func (s *ScanServiceNew) upsertNode(tenantID, engineID uint, parent *models.MetaNode, nodeType, name, fullName string, attrs models.JSONMap) (*models.MetaNode, error) {
	var parentID *uint
	depth := 1
	if parent != nil {
		parentID = &parent.ID
		depth = parent.Depth + 1
	}

	query := s.db.Unscoped().Where("res_id = ? AND tenant_id = ? AND node_type = ? AND name = ?", engineID, tenantID, nodeType, name)
	if parentID == nil {
		query = query.Where("parent_node_id IS NULL")
	} else {
		query = query.Where("parent_node_id = ?", *parentID)
	}

	var node models.MetaNode
	err := query.First(&node).Error
	if err == gorm.ErrRecordNotFound {
		node = models.MetaNode{
			TenantID:     tenantID,
			EngineID:     engineID,
			ParentNodeID: parentID,
			NodeType:     nodeType,
			Name:         name,
			Depth:        depth,
			Status:       "active",
			ScanStatus:   "未扫描",
			Attributes:   models.JSONMap{},
		}
		if fullName != "" {
			node.FullName = fullName
		}
		if attrs != nil {
			node.Attributes = attrs
		}
		if err := s.db.Create(&node).Error; err != nil {
			return nil, err
		}

		path := composeNodePath(node.ID, parent)
		update := map[string]interface{}{"path": path}
		node.Path = path
		if node.FullName == "" {
			node.FullName = composeNodeFullName(node.Name, parent, ".")
			update["full_name"] = node.FullName
		}
		if err := s.db.Model(&node).Updates(update).Error; err != nil {
			return nil, err
		}
		return &node, nil
	} else if err != nil {
		return nil, err
	}

	updates := map[string]interface{}{
		"deleted_at": nil, // 恢复软删除的节点
	}
	if node.Depth != depth {
		updates["depth"] = depth
		node.Depth = depth
	}

	path := composeNodePath(node.ID, parent)
	if node.Path != path {
		updates["path"] = path
		node.Path = path
	}

	expectedFullName := fullName
	if expectedFullName == "" {
		expectedFullName = composeNodeFullName(name, parent, ".")
	}
	if node.FullName != expectedFullName {
		updates["full_name"] = expectedFullName
		node.FullName = expectedFullName
	}

	if attrs != nil && len(attrs) > 0 {
		updates["attributes"] = attrs
		node.Attributes = attrs
	}

	if len(updates) > 0 {
		updates["updated_at"] = time.Now()
		if err := s.db.Unscoped().Model(&node).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	return &node, nil
}

func (s *ScanServiceNew) resetNodeState(node *models.MetaNode, status string) error {
	now := time.Now()
	update := map[string]interface{}{
		"scan_status":   status,
		"error_message": "",
		"updated_at":    now,
	}
	if status == "扫描中" {
		update["last_scan_at"] = now
	}
	return s.db.Model(node).Updates(update).Error
}

func (s *ScanServiceNew) finalizeNodeState(node *models.MetaNode, status string, itemCount int, totalSize int64, errMsg string) error {
	update := map[string]interface{}{
		"scan_status":      status,
		"item_count":       itemCount,
		"total_size_bytes": totalSize,
		"error_message":    errMsg,
		"updated_at":       time.Now(),
	}
	if status == "已扫描" {
		update["last_scan_at"] = time.Now()
	}
	return s.db.Model(node).Updates(update).Error
}

func (s *ScanServiceNew) hardDeleteItemsByNode(nodeID uint) error {
	return s.db.Unscoped().Where("node_id = ?", nodeID).Delete(&models.MetaItem{}).Error
}

func (s *ScanServiceNew) hardDeleteDescendantNodes(node *models.MetaNode) error {
	if node.Path == "" {
		return nil
	}
	prefix := fmt.Sprintf("%s/%%", node.Path)
	return s.db.Unscoped().
		Where("path LIKE ?", prefix).
		Where("id <> ?", node.ID).
		Delete(&models.MetaNode{}).Error
}

func (s *ScanServiceNew) upsertItem(
	tenantID, engineID uint,
	node *models.MetaNode,
	itemType, name, fullName string,
	attrs models.JSONMap,
	rowCount, sizeBytes, objectSize *int64,
	lastModified *time.Time,
	schemaVersion int,
) (*models.MetaItem, error) {
	return s.upsertItemSelective(tenantID, engineID, node, itemType, name, fullName, attrs, rowCount, sizeBytes, objectSize, lastModified, schemaVersion)
}

// upsertItemSelective 选择性更新item
// 当attrs为nil时，不更新attributes字段（用于basic扫描保留deep扫描的元数据）
func (s *ScanServiceNew) upsertItemSelective(
	tenantID, engineID uint,
	node *models.MetaNode,
	itemType, name, fullName string,
	attrs models.JSONMap,
	rowCount, sizeBytes, objectSize *int64,
	lastModified *time.Time,
	schemaVersion int,
) (*models.MetaItem, error) {
	// 生成数据指纹
	var fingerprint string
	if attrs != nil {
		// 对象存储：使用 bucket/path
		if bucket, ok := attrs["bucket"].(string); ok {
			path := ""
			if p, ok := attrs["path"].(string); ok {
				path = p
			} else if rp, ok := attrs["relative_path"].(string); ok {
				path = rp
			}
			// 注意：path可能已经包含bucket前缀（历史设计）
			// GenerateObjectFingerprint会再添加bucket，形成"bucket/bucket/path"
			// 为了兼容性，保持这种方式
			fingerprint = commonModels.GenerateObjectFingerprint(engineID, bucket, path)
		} else if schema, ok := attrs["schema_name"].(string); ok {
			// 关系数据库：使用 schema.table
			fingerprint = commonModels.GenerateTableFingerprint(engineID, schema, name)
		} else {
			// 其他类型：使用 fullName
			fingerprint = commonModels.GenerateItemFingerprint(engineID, fullName)
		}
	} else {
		// 无attributes，使用fullName作为标识
		// 注意：当attrs为nil时（basic扫描保留已有数据），需要先查找已有记录获取指纹
		var existingItem models.MetaItem
		err := s.db.Where("res_id = ? AND node_id = ? AND item_type = ? AND name = ?",
			engineID, node.ID, itemType, name).First(&existingItem).Error
		if err == nil {
			fingerprint = existingItem.Fingerprint
		} else {
			// 如果找不到已有记录且attrs为nil，无法生成fingerprint，报错
			return nil, fmt.Errorf("cannot generate fingerprint without attributes for new item")
		}
	}

	var item models.MetaItem
	// 使用fingerprint查找记录（唯一索引），包括软删除的记录
	err := s.db.Unscoped().Where("fingerprint = ?", fingerprint).First(&item).Error

	if err == gorm.ErrRecordNotFound {
		// 创建新记录
		item = models.MetaItem{
			TenantID:          tenantID,
			EngineID:          engineID,
			NodeID:            node.ID,
			ItemType:          itemType,
			Name:              name,
			FullName:          fullName,
			Fingerprint:       fingerprint, // 设置指纹
			Status:            "active",
			MetaSchemaVersion: schemaVersion,
			Attributes:        models.JSONMap{},
			RowCount:          rowCount,
			SizeBytes:         sizeBytes,
			ObjectSizeBytes:   objectSize,
			LastModifiedAt:    lastModified,
		}
		if attrs != nil {
			item.Attributes = attrs
		}
		if err := s.db.Create(&item).Error; err != nil {
			return nil, err
		}
		return &item, nil
	} else if err != nil {
		return nil, err
	}

	// 更新已有记录（包括恢复软删除的记录）
	updates := map[string]interface{}{
		"node_id":             node.ID, // 允许node_id变化（数据移动）
		"full_name":           fullName,
		"meta_schema_version": schemaVersion,
		"row_count":           rowCount,
		"size_bytes":          sizeBytes,
		"object_size_bytes":   objectSize,
		"last_modified_at":    lastModified,
		"updated_at":          time.Now(),
		"deleted_at":          nil, // 恢复软删除的记录
	}

	// 只有当attrs不为nil时才更新attributes（basic扫描时保留已有的deep元数据）
	if attrs != nil {
		updates["attributes"] = attrs
	}

	if err := s.db.Unscoped().Model(&item).Updates(updates).Error; err != nil {
		return nil, err
	}

	// 更新内存中的对象
	item.NodeID = node.ID
	item.FullName = fullName
	item.MetaSchemaVersion = schemaVersion
	if attrs != nil {
		item.Attributes = attrs
	}
	item.RowCount = rowCount
	item.SizeBytes = sizeBytes
	item.ObjectSizeBytes = objectSize
	item.LastModifiedAt = lastModified

	return &item, nil
}

func buildFieldAttributes(fields []plugins.FieldInfo) []map[string]interface{} {
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
func (s *ScanServiceNew) AutoScanUnscanned(tenantID uint) (*models.ScanResponse, error) {
	startTime := time.Now()

	// 创建扫描日志
	scanLog := &models.ScanLog{
		TenantID:  tenantID,
		ScanType:  "auto",
		ScanDepth: "deep",
		Status:    "running",
		StartedAt: &startTime,
	}
	if err := s.db.Create(scanLog).Error; err != nil {
		return nil, fmt.Errorf("failed to create scan log: %w", err)
	}

	// 获取所有数据库资源
	engines, err := s.resourceService.GetEnginesByTenant(tenantID)
	if err != nil {
		s.updateScanLogFailed(scanLog, err.Error())
		return nil, err
	}

	totalSchemas := 0
	totalTables := 0
	totalFields := 0
	scannedResourceIDs := []uint{}

	// 对每个资源进行扫描
	for _, engine := range engines {
		schemas, tables, fields, err := s.scanResource(engine, tenantID, scanLog.ID)
		if err != nil {
			s.log.Warn("资源扫描失败",
				"engine_id", engine.ID,
				"resource_name", engine.Name,
				"tenant_id", tenantID,
				"scan_log_id", scanLog.ID,
				"error", err,
			)
			continue
		}

		totalSchemas += schemas
		totalTables += tables
		totalFields += fields
		scannedResourceIDs = append(scannedResourceIDs, engine.ID)
	}

	// 更新扫描日志
	completedAt := time.Now()
	scanLog.EngineID = 0 // 多资源扫描，不关联特定资源
	scanLog.Status = "success"
	scanLog.SchemasScanned = totalSchemas
	scanLog.TablesScanned = totalTables
	scanLog.FieldsScanned = totalFields
	scanLog.CompletedAt = &completedAt
	scanLog.DurationMs = completedAt.Sub(startTime).Milliseconds()
	s.db.Save(scanLog)

	return &models.ScanResponse{
		Status:         "success",
		Message:        fmt.Sprintf("Successfully scanned %d engines", len(scannedResourceIDs)),
		SchemasScanned: totalSchemas,
		TablesScanned:  totalTables,
		FieldsScanned:  totalFields,
		DurationMs:     scanLog.DurationMs,
		StartedAt:      startTime.Format("2006-01-02 15:04:05"),
	}, nil
}

// ScanResource 扫描指定资源
func (s *ScanServiceNew) ScanResource(engineID, tenantID uint, schemaNames, objectPaths []string, token string) (*models.ScanResponse, error) {
	return s.scanResourceInternal(engineID, tenantID, schemaNames, objectPaths, token, "deep", nil)
}

// ScanResourceWithProgress 扫描指定资源，并通过 reporter 汇报进度
func (s *ScanServiceNew) ScanResourceWithProgress(engineID, tenantID uint, schemaNames, objectPaths []string, token string, reporter ScanProgressReporter) (*models.ScanResponse, error) {
	return s.scanResourceInternal(engineID, tenantID, schemaNames, objectPaths, token, "deep", reporter)
}

// ScanResourceWithDepth 扫描指定资源，支持指定扫描深度
func (s *ScanServiceNew) ScanResourceWithDepth(engineID, tenantID uint, schemaNames, objectPaths []string, token string, scanDepth string, reporter ScanProgressReporter) (*models.ScanResponse, error) {
	return s.scanResourceInternal(engineID, tenantID, schemaNames, objectPaths, token, scanDepth, reporter)
}

func (s *ScanServiceNew) scanResourceInternal(engineID, tenantID uint, schemaNames, objectPaths []string, token string, scanDepth string, reporter ScanProgressReporter) (*models.ScanResponse, error) {
	startTime := time.Now()

	// 获取资源
	if reporter != nil {
		reporter.Message("正在加载资源连接信息")
	}
	resource, err := s.resourceService.GetResourceByID(engineID, tenantID, token)
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

	// 创建扫描日志
	schemasJSON, _ := json.Marshal(schemaNames)
	scanLog := &models.ScanLog{
		EngineID:    engineID,
		TenantID:      tenantID,
		ScanType:      "manual",
		ScanDepth:     scanDepth,
		TargetSchemas: string(schemasJSON),
		Status:        "running",
		StartedAt:     &startTime,
	}
	if err := s.db.Create(scanLog).Error; err != nil {
		return nil, fmt.Errorf("failed to create scan log: %w", err)
	}

	startFields := append(connectionLogFields(resource),
		"scan_log_id", scanLog.ID,
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

	if isObjectStorageType(resourceType) {
		schemas, tables, fields, err = s.scanObjectStorageResourceWithReporter(resource, tenantID, objectPaths, schemaNames, scanDepth, reporter)
	} else {
		schemas, tables, fields, err = s.scanResourceSchemasWithReporter(resource, tenantID, schemaNames, scanLog.ID, scanDepth, reporter)
	}

	if err != nil {
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描失败: %v", err))
		}
		s.updateScanLogFailed(scanLog, err.Error())
		if directRun != nil {
			s.failImmediateRun(directRun, err)
		}
		return nil, err
	}

	// 更新扫描日志
	completedAt := time.Now()
	scanLog.Status = "success"
	scanLog.SchemasScanned = schemas
	scanLog.TablesScanned = tables
	scanLog.FieldsScanned = fields
	scanLog.CompletedAt = &completedAt
	scanLog.DurationMs = completedAt.Sub(startTime).Milliseconds()
	s.db.Save(scanLog)

	finishFields := append(make([]any, 0, len(startFields)+6), startFields...)
	finishFields = append(finishFields,
		"schemas_scanned", schemas,
		"tables_scanned", tables,
		"fields_scanned", fields,
		"duration_ms", scanLog.DurationMs,
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
			"duration_ms":     scanLog.DurationMs,
			"started_at":      scanLog.StartedAt,
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
				"duration_ms":     scanLog.DurationMs,
				"started_at":      scanLog.StartedAt,
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
		DurationMs:     scanLog.DurationMs,
		StartedAt:      startTime.Format("2006-01-02 15:04:05"),
	}, nil
}

func (s *ScanServiceNew) createImmediateRunRecord(resource *commonModels.Engine, tenantID uint, schemaNames, objectPaths []string, startTime time.Time) (*models.ScanTaskRun, error) {
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

func (s *ScanServiceNew) failImmediateRun(run *models.ScanTaskRun, scanErr error) {
	if run == nil {
		return
	}
	update := map[string]interface{}{
		"status":           runStatusFailed,
		"error_message":    scanErr.Error(),
		"progress_message": fmt.Sprintf("执行失败: %v", scanErr),
		"completed_at":     time.Now(),
		"updated_at":       time.Now(),
	}
	if err := s.db.Model(&models.ScanTaskRun{}).Where("id = ?", run.ID).Updates(update).Error; err != nil {
		s.log.Error("更新即时运行失败状态出错", "run_id", run.ID, "error", err)
	}
}

func (s *ScanServiceNew) completeImmediateRun(run *models.ScanTaskRun, summary models.JSONMap, completedAt time.Time) {
	if run == nil {
		return
	}

	update := map[string]interface{}{
		"status":           runStatusSuccess,
		"result_summary":   summary,
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
func (s *ScanServiceNew) scanResource(resource *commonModels.Engine, tenantID uint, scanLogID uint) (int, int, int, error) {
	engineID := resource.ID

	startFields := append(connectionLogFields(resource),
		"scan_log_id", scanLogID,
		"mode", "auto",
	)
	s.log.Info("开始扫描资源", startFields...)

	// 重构后：直接传入Resource对象，由插件系统管理连接
	scan, err := plugins.NewScanner(resource)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	if objectScanner, ok := scan.(plugins.ObjectStorageScanner); ok && isObjectStorageType(strings.ToLower(resource.EngineType)) {
		buckets := objectScanner.AllowedBuckets()
		if len(buckets) == 0 {
			s.log.Info("对象存储资源未配置可扫描桶，跳过扫描", cloneLogFields(startFields, "allowed_bucket_count", 0)...)
			return 0, 0, 0, nil
		}
		sort.Strings(buckets)

		totalBuckets := 0
		totalObjects := 0

		s.log.Info("对象存储资源扫描开始", cloneLogFields(startFields, "allowed_bucket_count", len(buckets), "allowed_buckets", buckets)...)

		// 记录本次扫描到的 bucket
		scannedBuckets := make(map[string]bool)

		for _, bucket := range buckets {
			scannedBuckets[bucket] = true

			var node models.MetaNode
			err := s.db.Where("tenant_id = ? AND res_id = ? AND node_type = ? AND name = ?",
				tenantID, engineID, "bucket", bucket).First(&node).Error

			// 无论 bucket 是否已存在,都进行扫描以确保元数据同步
			// 这样可以触发对象的增量更新和过期对象的清理
			if err == gorm.ErrRecordNotFound || err == nil {
				schemas, objects, err := s.scanObjectStoragePaths(resource, tenantID, engineID, objectScanner, []string{bucket}, "deep", nil)
				if err != nil {
					s.log.Warn("对象存储桶扫描失败",
						"engine_id", engineID,
						"tenant_id", tenantID,
						"bucket", bucket,
						"error", err,
					)
					continue
				}
				totalBuckets += schemas
				totalObjects += objects
			} else {
				s.log.Warn("查询对象存储节点失败",
					"engine_id", engineID,
					"tenant_id", tenantID,
					"bucket", bucket,
					"error", err,
				)
			}
		}

		// 软删除那些不再存在的 bucket
		var existingBuckets []models.MetaNode
		if err := s.db.Where("tenant_id = ? AND res_id = ? AND node_type = ?",
			tenantID, engineID, "bucket").Find(&existingBuckets).Error; err != nil {
			s.log.Warn("查询已存在 bucket 节点失败", "error", err)
		} else {
			for _, bucketNode := range existingBuckets {
				if !scannedBuckets[bucketNode.Name] {
					s.log.Info("Bucket 已不存在，标记删除",
						"engine_id", engineID,
						"bucket", bucketNode.Name,
					)
					if err := s.db.Delete(&bucketNode).Error; err != nil {
						s.log.Warn("软删除 bucket 节点失败",
							"bucket", bucketNode.Name,
							"error", err,
						)
					} else {
						// 同时软删除该 bucket 下的所有对象
						if err := s.db.Where("node_id = ?", bucketNode.ID).Delete(&models.MetaItem{}).Error; err != nil {
							s.log.Warn("软删除 bucket 下的对象失败",
								"bucket", bucketNode.Name,
								"error", err,
							)
						}
					}
				}
			}
		}

		s.log.Info("对象存储资源扫描完成", cloneLogFields(startFields,
			"buckets_scanned", totalBuckets,
			"objects_scanned", totalObjects,
		)...)
		return totalBuckets, totalObjects, 0, nil
	}

	schemasInfo, err := scan.ListSchemas()
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
		err := s.db.Where("tenant_id = ? AND res_id = ? AND node_type = ? AND name = ?",
			tenantID, engineID, "schema", schemaInfo.Name).First(&node).Error

		// 无论 schema 是否已存在,都进行扫描以确保元数据同步
		// 这样可以触发表的增量更新和过期表的清理
		if err == gorm.ErrRecordNotFound || err == nil {
			schemas, tables, fields, err := s.scanDatabaseSchema(resource, scan, tenantID, engineID, schemaInfo.Name, "deep")
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
	if err := s.db.Where("tenant_id = ? AND res_id = ? AND node_type = ?",
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
func (s *ScanServiceNew) scanResourceSchemas(resource *commonModels.Engine, tenantID uint, schemaNames []string, scanLogID uint, scanDepth string) (int, int, int, error) {
	return s.scanResourceSchemasWithReporter(resource, tenantID, schemaNames, scanLogID, scanDepth, nil)
}

func (s *ScanServiceNew) scanResourceSchemasWithReporter(resource *commonModels.Engine, tenantID uint, schemaNames []string, scanLogID uint, scanDepth string, reporter ScanProgressReporter) (int, int, int, error) {
	resourceID := resource.ID

	startFields := append(connectionLogFields(resource),
		"scan_log_id", scanLogID,
		"mode", "manual",
	)
	if len(schemaNames) > 0 {
		startFields = append(startFields, "target_schemas", schemaNames)
	}
	s.log.Info("开始扫描指定 Schema 列表", startFields...)

	// 重构后：不再手动构建连接字符串，由插件系统管理
	s.log.Info("🔧 准备创建Scanner",
		"engine_id", engineID,
		"resource_type", resource.EngineType,
	)

	scan, err := plugins.NewScanner(resource)
	if err != nil {
		s.log.Error("❌ 创建Scanner失败",
			"engine_id", engineID,
			"resource_type", resource.EngineType,
			"error", err,
		)
		return 0, 0, 0, fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	s.log.Info("✅ Scanner创建成功",
		"engine_id", engineID,
		"resource_type", resource.EngineType,
	)

	// 如果未指定Schema，则扫描所有Schema（自动过滤系统schema）
	if len(schemaNames) == 0 {
		if reporter != nil {
			reporter.Message("未指定 Schema，正在获取完整列表")
		}
		schemasInfo, err := scan.ListSchemas()
		if err != nil {
			return 0, 0, 0, err
		}

		// 系统 schema 黑名单（自动过滤）
		systemSchemas := map[string]bool{
			"pg_catalog":         true,
			"information_schema": true,
			"pg_toast":           true,
		}

		for _, info := range schemasInfo {
			// 过滤系统 schema
			if systemSchemas[info.Name] {
				s.log.Debug("跳过系统 schema", "schema", info.Name)
				continue
			}
			// 过滤 pg_toast 开头的 schema（动态生成的临时表）
			if strings.HasPrefix(info.Name, "pg_toast_") {
				s.log.Debug("跳过系统临时 schema", "schema", info.Name)
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

		schemas, tables, fields, err := s.scanDatabaseSchema(resource, scan, tenantID, engineID, schemaName, scanDepth)
		if err != nil {
			s.log.Warn("Schema 扫描失败",
				"engine_id", engineID,
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

// scanSingleSchema 扫描单个Schema（表+字段）
func (s *ScanServiceNew) scanObjectStorageResource(resource *commonModels.Engine, tenantID uint, objectPaths, fallback []string) (int, int, int, error) {
	return s.scanObjectStorageResourceWithReporter(resource, tenantID, objectPaths, fallback, "deep", nil)
}

func (s *ScanServiceNew) scanObjectStorageResourceWithReporter(resource *commonModels.Engine, tenantID uint, objectPaths, fallback []string, scanDepth string, reporter ScanProgressReporter) (int, int, int, error) {
	resourceID := resource.ID

	// 标准化 scanDepth
	if scanDepth == "" {
		scanDepth = "deep"
	}

	// 重构后：直接传入Resource对象，由插件系统管理连接
	scan, err := plugins.NewScanner(resource)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	objectScanner, ok := scan.(plugins.ObjectStorageScanner)
	if !ok {
		return 0, 0, 0, fmt.Errorf("resource %s is not object storage", resource.EngineType)
	}

	paths := prepareObjectPaths(objectPaths, fallback, objectScanner)
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

	buckets, objects, err := s.scanObjectStoragePaths(resource, tenantID, engineID, objectScanner, paths, scanDepth, reporter)
	if err != nil {
		return 0, 0, 0, err
	}

	return buckets, objects, 0, nil
}

func (s *ScanServiceNew) scanObjectStoragePaths(resource *commonModels.Engine, tenantID, engineID uint, objectScanner plugins.ObjectStorageScanner, paths []string, scanDepth string, reporter ScanProgressReporter) (int, int, error) {
	s.log.Info("🔍 进入scanObjectStoragePaths函数",
		"engine_id", engineID,
		"tenant_id", tenantID,
		"paths_count", len(paths),
		"scanDepth", scanDepth)

	bucketNodes := make(map[string]*models.MetaNode)
	processedBuckets := make(map[string]bool)
	nodeStats := make(map[uint]*nodeAggregate)

	// 记录本次扫描到的所有fingerprints，用于后续清理未扫描到的item
	scannedFingerprints := make(map[string]bool)

	// 如果scanner支持SetResourceID和SetScanDepth，设置扫描参数
	if s3Scanner, ok := objectScanner.(interface {
		SetResourceID(uint)
		SetScanDepth(string)
	}); ok {
		// 设置扫描深度
		s3Scanner.SetScanDepth(scanDepth)

		// 深度扫描时才启用详细元数据提取
		if strings.EqualFold(scanDepth, "deep") {
			s3Scanner.SetResourceID(engineID)
		} else {
			// 使用0关闭提取，避免浅度扫描额外读取对象内容
			s3Scanner.SetResourceID(0)
		}
	}

	totalBuckets := 0
	totalObjects := 0
	total := len(paths)
	completed := 0

	for _, rawPath := range paths {
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描对象路径 %s", rawPath))
		}
		bucketName, relativePath := splitObjectPath(rawPath)
		if bucketName == "" {
			s.log.Warn("对象存储路径缺少 bucket，跳过刷新", "path", rawPath)
			if reporter != nil {
				reporter.Message(fmt.Sprintf("对象路径 %s 缺少 bucket 信息，已跳过", rawPath))
			}
			completed++
			if reporter != nil {
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": 0})
			}
			continue
		}

		metas, err := objectScanner.ScanPath(rawPath)
		if err != nil {
			s.log.Warn("对象存储路径扫描失败",
				"engine_id", engineID,
				"tenant_id", tenantID,
				"path", rawPath,
				"error", err,
			)
			if reporter != nil {
				reporter.Message(fmt.Sprintf("对象路径 %s 扫描失败: %v", rawPath, err))
			}
			completed++
			if reporter != nil {
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": 0})
			}
			continue
		}

		if !strings.EqualFold(scanDepth, "deep") {
			metas = filterObjectMetasForDepth(metas, relativePath)
		}

		bucketNode, ok := bucketNodes[bucketName]
		if !ok {
			attrs := models.JSONMap{"bucket": bucketName}
			bucketNode, err = s.upsertNode(tenantID, engineID, nil, "bucket", bucketName, bucketName, attrs)
			if err != nil {
				return totalBuckets, totalObjects, err
			}
			bucketNodes[bucketName] = bucketNode
			totalBuckets++
		}

		fullBucket := relativePath == ""
		if fullBucket {
			if !processedBuckets[bucketName] {
				if err := s.resetNodeState(bucketNode, "扫描中"); err != nil {
					return totalBuckets, totalObjects, err
				}
			}
			processedBuckets[bucketName] = true
		}

		if len(metas) == 0 {
			if fullBucket {
				ensureNodeAggregate(nodeStats, bucketNode)
			}
			if reporter != nil {
				reporter.Message(fmt.Sprintf("对象路径 %s 未发现新对象", rawPath))
			}
			completed++
			if reporter != nil {
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": 0})
			}
			continue
		}

		// 传递扫描路径前缀，用于正确计算fullName
		scanPathPrefix := relativePath
		s.log.Info("传递scanPathPrefix到persistObjectMetas",
			"rawPath", rawPath,
			"bucketName", bucketName,
			"relativePath", relativePath,
			"scanPathPrefix", scanPathPrefix,
			"fullBucket", fullBucket,
			"metasCount", len(metas),
			"scanDepth", scanDepth)
		objects, err := s.persistObjectMetas(resource, tenantID, engineID, bucketNode, metas, nodeStats, fullBucket, scanDepth, scanPathPrefix, scannedFingerprints)
		if err != nil {
			s.log.Error("对象存储元数据持久化失败",
				"engine_id", engineID,
				"tenant_id", tenantID,
				"bucket", bucketName,
				"error", err,
			)
			continue
		}
		totalObjects += objects
		completed++
		if reporter != nil {
			reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": objects})
		}
	}

	// 清理未在本次扫描中出现的旧item
	// 深度扫描时才执行清理，浅度扫描不应删除未扫描到的对象
	if strings.EqualFold(scanDepth, "deep") && len(scannedFingerprints) > 0 {
		// 查询本次扫描路径下的所有对象
		for bucketName := range processedBuckets {
			var existingItems []models.MetaItem
			bucketNode := bucketNodes[bucketName]
			if bucketNode == nil {
				continue
			}

			// 查询该 bucket 下的所有对象
			if err := s.db.Where("tenant_id = ? AND res_id = ? AND item_type = ?",
				tenantID, engineID, "object").
				Where("attributes->>'bucket' = ?", bucketName).
				Find(&existingItems).Error; err != nil {
				s.log.Warn("查询已存在对象元数据失败",
					"bucket", bucketName,
					"error", err,
				)
				continue
			}

			// 软删除未在本次扫描中出现的对象
			for _, item := range existingItems {
				if !scannedFingerprints[item.Fingerprint] {
					s.log.Info("对象已不存在，标记删除",
						"bucket", bucketName,
						"fingerprint", item.Fingerprint,
						"name", item.Name,
					)
					if err := s.db.Delete(&item).Error; err != nil {
						s.log.Warn("软删除对象元数据失败",
							"bucket", bucketName,
							"fingerprint", item.Fingerprint,
							"error", err,
						)
					}
				}
			}
		}
	}

	for _, agg := range nodeStats {
		if err := s.finalizeNodeState(agg.node, "已扫描", agg.itemCount, agg.totalSize, ""); err != nil {
			return totalBuckets, totalObjects, err
		}
	}

	// 确保所有已处理的bucket节点状态被更新为"已扫描"
	// 即使扫描的是子目录而非整个bucket，bucket节点也应该标记为已扫描
	s.log.Info("检查processedBuckets以更新bucket状态",
		"processedBuckets_count", len(processedBuckets),
		"bucketNodes_count", len(bucketNodes),
		"nodeStats_count", len(nodeStats))

	for bucketName := range processedBuckets {
		bucketNode := bucketNodes[bucketName]
		if bucketNode != nil {
			// 如果bucket节点不在nodeStats中（扫描子目录时），单独更新其状态
			if _, exists := nodeStats[bucketNode.ID]; !exists {
				s.log.Info("bucket节点不在nodeStats中，需要单独更新",
					"bucket", bucketName,
					"bucket_node_id", bucketNode.ID)

				// 计算该bucket的统计信息
				var itemCount int64
				var totalSize int64
				s.db.Model(&models.MetaItem{}).
					Where("tenant_id = ? AND res_id = ? AND attributes->>'bucket' = ?",
						tenantID, engineID, bucketName).
					Count(&itemCount)
				s.db.Model(&models.MetaItem{}).
					Where("tenant_id = ? AND res_id = ? AND attributes->>'bucket' = ?",
						tenantID, engineID, bucketName).
					Select("COALESCE(SUM(size_bytes), 0)").
					Scan(&totalSize)

				s.log.Info("更新bucket节点状态为已扫描",
					"bucket", bucketName,
					"item_count", itemCount,
					"total_size", totalSize)

				if err := s.finalizeNodeState(bucketNode, "已扫描", int(itemCount), totalSize, ""); err != nil {
					s.log.Warn("更新bucket节点状态失败", "bucket", bucketName, "error", err)
				} else {
					s.log.Info("成功更新bucket节点状态", "bucket", bucketName)
				}
			} else {
				s.log.Info("bucket节点已在nodeStats中，无需单独更新",
					"bucket", bucketName,
					"bucket_node_id", bucketNode.ID)
			}
		}
	}

	return totalBuckets, totalObjects, nil
}

func (s *ScanServiceNew) persistObjectMetas(resource *commonModels.Engine, tenantID, engineID uint, bucketNode *models.MetaNode, metas []plugins.ObjectMetadata, stats map[uint]*nodeAggregate, includeBucketAggregate bool, scanDepth string, scanPathPrefix string, scannedFingerprints map[string]bool) (int, error) {
	objects := 0

	// 重要：在循环外部只处理一次scanPathPrefix，建立基础父节点
	// 例如：扫描 "addp/shapefile" 时，scanPathPrefix = "shapefile"
	// 需要先创建/查找 "shapefile" 节点作为所有对象的基础父节点
	basePrefixNode := bucketNode
	if scanPathPrefix != "" {
		s.log.Info("处理scanPathPrefix建立基础父节点",
			"scanPathPrefix", scanPathPrefix,
			"bucket", bucketNode.Name)
		prefixSegments := strings.Split(sanitizeObjectPath(scanPathPrefix), "/")
		currentParent := bucketNode
		for idx, segment := range prefixSegments {
			fullName := composeNodeFullName(segment, currentParent, "/")
			pathSoFar := strings.Join(prefixSegments[:idx+1], "/")
			attrs := models.JSONMap{
				"bucket": bucketNode.Name,
				"path":   pathSoFar,
			}
			childNode, err := s.upsertNode(tenantID, engineID, currentParent, "prefix", segment, fullName, attrs)
			if err != nil {
				return objects, err
			}
			s.log.Info("创建/找到前缀节点",
				"segment", segment,
				"node_id", childNode.ID,
				"node_name", childNode.Name,
				"fullName", fullName)
			currentParent = childNode
			ensureNodeAggregate(stats, childNode)
		}
		basePrefixNode = currentParent
		s.log.Info("scanPathPrefix处理完成",
			"basePrefixNode_id", basePrefixNode.ID,
			"basePrefixNode_name", basePrefixNode.Name)
	}

	for _, meta := range metas {
		if meta.NodeType == "bucket" {
			if includeBucketAggregate {
				ensureNodeAggregate(stats, bucketNode)
			}
			continue
		}

		// 使用基础前缀节点作为起点
		parentChain := []*models.MetaNode{bucketNode}
		if basePrefixNode != bucketNode {
			parentChain = append(parentChain, basePrefixNode)
		}
		currentParent := basePrefixNode

		// 然后处理相对路径（相对于scanPathPrefix）
		trimmed := sanitizeObjectPath(meta.RelativePath)

		// 调试日志：记录每个meta对象的处理
		s.log.Info("处理meta对象",
			"meta.NodeType", meta.NodeType,
			"meta.Path", meta.Path,
			"meta.RelativePath", meta.RelativePath,
			"trimmed", trimmed,
			"scanPathPrefix", scanPathPrefix,
			"basePrefixNode", basePrefixNode.Name)

		// 重要：处理scanPathPrefix相关的路径，避免重复创建节点
		// Scanner在扫描子目录时会返回包含scanPathPrefix的路径
		var segmentsToProcess []string
		skipReason := ""
		if scanPathPrefix != "" && trimmed == scanPathPrefix {
			// 情况1：trimmed刚好等于scanPathPrefix（Scanner返回的prefix汇总对象）
			// 跳过segment处理，basePrefixNode已经创建了这个节点
			skipReason = "trimmed==scanPathPrefix"
			if meta.NodeType == "prefix" {
				ensureNodeAggregate(stats, basePrefixNode)
			}
			// 不处理segments
		} else if scanPathPrefix != "" && strings.HasPrefix(trimmed, scanPathPrefix+"/") {
			// 情况2：trimmed以"scanPathPrefix/"开头（Scanner的dirAgg中间目录）
			// 去掉scanPathPrefix前缀，只处理剩余部分
			skipReason = "trimmed以scanPathPrefix/开头，去掉前缀"
			remaining := strings.TrimPrefix(trimmed, scanPathPrefix+"/")
			if remaining != "" {
				segmentsToProcess = strings.Split(remaining, "/")
			}
		} else if trimmed != "" {
			// 情况3：正常路径，不包含scanPathPrefix前缀
			segmentsToProcess = strings.Split(trimmed, "/")
		} else if includeBucketAggregate {
			// 情况4：空路径，统计bucket
			skipReason = "空路径"
			ensureNodeAggregate(stats, bucketNode)
		}

		if skipReason != "" {
			s.log.Info("跳过或特殊处理",
				"reason", skipReason,
				"segmentsToProcess", segmentsToProcess)
		} else if len(segmentsToProcess) > 0 {
			s.log.Info("准备处理segments",
				"segmentsToProcess", segmentsToProcess)
		}

		// 处理segments（如果有）
		if len(segmentsToProcess) > 0 {
			for idx, segment := range segmentsToProcess {
				isLast := idx == len(segmentsToProcess)-1
				if meta.NodeType == "object" && isLast {
					break
				}
				fullName := composeNodeFullName(segment, currentParent, "/")
				attrs := models.JSONMap{
					"bucket": meta.Bucket,
					"path":   strings.Join(segmentsToProcess[:idx+1], "/"),
				}
				childNode, err := s.upsertNode(tenantID, engineID, currentParent, "prefix", segment, fullName, attrs)
				if err != nil {
					return objects, err
				}
				currentParent = childNode
				parentChain = append(parentChain, childNode)
				ensureNodeAggregate(stats, childNode)
			}
		}

		if meta.NodeType != "object" {
			continue
		}

		objectName := pathpkg.Base(strings.Trim(meta.Path, "/"))
		if objectName == "" {
			objectName = trimmed
		}
		objectName = strings.Trim(objectName, "/")
		if objectName == "" {
			objectName = fmt.Sprintf("object_%d", meta.SizeBytes)
		}

		// 构建基础属性
		attrs := models.JSONMap{
			"bucket":        meta.Bucket,
			"path":          meta.Path,
			"relative_path": trimmed,
			"file_type":     meta.FileType,
			"object_count":  meta.ObjectCount,
		}
		if meta.LastModified != nil {
			attrs["last_modified_at"] = meta.LastModified
		}

		// 生成fingerprint - 使用meta.Path（包含bucket前缀）
		// 注意：meta.Path已经包含bucket前缀（如"addp/2024东北高斯三维.mov"）
		// GenerateObjectFingerprint会再添加bucket，结果是"addp/addp/2024东北高斯三维.mov"
		// 这是历史设计，为了保持兼容性，我们继续使用这种方式
		fingerprint := commonModels.GenerateObjectFingerprint(engineID, meta.Bucket, meta.Path)
		if scannedFingerprints != nil {
			scannedFingerprints[fingerprint] = true
		}

		// 检查记录是否已存在（包括软删除的记录）
		var existingItem models.MetaItem
		itemExists := s.db.Unscoped().Where("fingerprint = ?", fingerprint).First(&existingItem).Error == nil

		// 增量更新逻辑：对比 LastModifiedAt 和 SizeBytes
		needsUpdate := !itemExists || // 新对象
			(existingItem.LastModifiedAt != nil && meta.LastModified != nil && !existingItem.LastModifiedAt.Equal(*meta.LastModified)) || // 修改时间变化
			(existingItem.SizeBytes != nil && *existingItem.SizeBytes != meta.SizeBytes) || // 大小变化
			(existingItem.LastModifiedAt == nil && meta.LastModified != nil) || // 之前未记录修改时间
			(existingItem.SizeBytes == nil && meta.SizeBytes != 0) // 之前未记录大小

		// 浅度扫描时，如果对象未变化，跳过更新（保留已有的深度元数据）
		if !strings.EqualFold(scanDepth, "deep") && itemExists && !needsUpdate {
			s.log.Debug("对象未变化，跳过更新",
				"bucket", meta.Bucket,
				"path", meta.Path,
			)
			// 仍然需要统计
			objects++
			for idx, node := range parentChain {
				if !includeBucketAggregate && idx == 0 {
					continue
				}
				agg := ensureNodeAggregate(stats, node)
				agg.itemCount++
				agg.totalSize += meta.SizeBytes
			}
			continue
		}

		// 根据扫描深度决定是否提取深度元数据
		var enhancedAttrs models.JSONMap
		if strings.EqualFold(scanDepth, "deep") {
			// 深度扫描：提取详细元数据（用于文件预览/搜索）
			// 传递meta.Path用于正确的fingerprint生成
			enhancedAttrs = s.extractEnhancedMetadataWithCache(engineID, meta, attrs, meta.Path)
		} else if itemExists {
			// 浅层扫描 + 记录已存在：保留原有attributes，只更新基础字段
			// 但仍需要更新node_id（文件可能移动到了不同的目录）
			enhancedAttrs = existingItem.Attributes // 使用已有的attributes
		} else {
			// 浅层扫描 + 新记录：使用基础属性
			enhancedAttrs = attrs
		}

		sizeVal := meta.SizeBytes
		objectSizeVal := meta.SizeBytes

		// 重要：fullName必须基于扫描路径前缀正确计算
		// Scanner返回的RelativePath是相对于扫描路径的，不是相对于bucket的
		// 例如：扫描 addp/shapefile 时，文件 shapefile/示例数据.shp 的 RelativePath 是 "示例数据.shp"
		// 我们需要加上 scanPathPrefix 来得到完整路径
		fullName := meta.Bucket
		if scanPathPrefix != "" && trimmed != "" {
			// 扫描子目录：bucket/scanPathPrefix/relativePath
			fullName = meta.Bucket + "/" + scanPathPrefix + "/" + trimmed
		} else if scanPathPrefix != "" {
			// 扫描子目录但文件在根目录（不应该发生）
			fullName = meta.Bucket + "/" + scanPathPrefix
		} else if trimmed != "" {
			// 扫描整个bucket：bucket/relativePath
			fullName = meta.Bucket + "/" + trimmed
		}
		// 否则 fullName = bucket（bucket级别的对象）

		// Debug logging (using INFO to ensure it shows)
		s.log.Info("计算fullName和父节点",
			"meta.Bucket", meta.Bucket,
			"meta.RelativePath", meta.RelativePath,
			"trimmed", trimmed,
			"scanPathPrefix", scanPathPrefix,
			"calculated_fullName", fullName,
			"currentParent_id", currentParent.ID,
			"currentParent_name", currentParent.Name,
			"objectName", objectName)

		item, err := s.upsertItem(tenantID, engineID, currentParent, "object", objectName, fullName, enhancedAttrs, nil, &sizeVal, &objectSizeVal, meta.LastModified, 1)
		if err != nil {
			return objects, err
		}

		// 只在deep扫描时索引（basic扫描的数据不完整）
		if scanDepth == "deep" {
			s.indexObjectAsset(resource, tenantID, engineID, meta, trimmed, fullName, item)
		}

		objects++
		for idx, node := range parentChain {
			if !includeBucketAggregate && idx == 0 {
				continue
			}
			agg := ensureNodeAggregate(stats, node)
			agg.itemCount++
			agg.totalSize += meta.SizeBytes
		}
	}

	if includeBucketAggregate {
		ensureNodeAggregate(stats, bucketNode)
	}
	return objects, nil
}

func (s *ScanServiceNew) indexTableAsset(resource *commonModels.Engine, tenantID uint, schemaName string, tableInfo plugins.TableInfo, fields []plugins.FieldInfo, item *models.MetaItem) {
	if s.indexer == nil || !s.indexer.Enabled() || resource == nil || item == nil {
		return
	}

	metadata := search.NormalizeMap(copyJSONMap(item.Attributes))
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	delete(metadata, "fields")

	fieldRecords := make([]search.FieldRecord, 0, len(fields))
	for _, field := range fields {
		fieldRecords = append(fieldRecords, search.FieldRecord{
			Name:            field.Name,
			DataType:        field.DataType,
			ColumnType:      field.ColumnType,
			Comment:         field.Comment,
			OrdinalPosition: field.OrdinalPosition,
			IsNullable:      field.IsNullable,
			IsPrimaryKey:    field.IsPrimaryKey,
			IsUniqueKey:     field.IsUniqueKey,
		})
	}

	record := &search.AssetRecord{
		AssetID:      item.Fingerprint,
		TenantID:     tenantID,
		EngineID:   resource.ID,
		ResourceName: resource.Name,
		ResourceType: resource.EngineType,
		AssetType:    "table",
		Name:         item.Name,
		FullName:     item.FullName,
		Schema:       schemaName,
		TableType:    tableInfo.Type,
		Description:  tableInfo.Comment,
		RowCount:     item.RowCount,
		SizeBytes:    item.SizeBytes,
		Metadata:     metadata,
		Fields:       fieldRecords,
		LastModified: item.LastModifiedAt,
	}

	if err := s.indexer.IndexAsset(context.Background(), record); err != nil {
		s.log.Warn("索引表元数据失败", "fingerprint", item.Fingerprint, "schema", schemaName, "error", err)
	}
}

func (s *ScanServiceNew) indexObjectAsset(resource *commonModels.Engine, tenantID, engineID uint, meta plugins.ObjectMetadata, relativePath, fullName string, item *models.MetaItem) {
	if s.indexer == nil || !s.indexer.Enabled() || resource == nil || item == nil {
		return
	}

	attributes := copyJSONMap(item.Attributes)
	metadata := search.NormalizeMap(attributes)
	if metadata == nil {
		metadata = map[string]interface{}{}
	}

	tags := extractStringSlice(metadata["tags"])
	if len(tags) > 0 {
		delete(metadata, "tags")
	}

	plainText := ""
	if meta.ExtractedMetadata != nil && meta.ExtractedMetadata.CustomAttrs != nil {
		if v, ok := meta.ExtractedMetadata.CustomAttrs["plain_text"].(string); ok {
			plainText = v
		}
	}
	if v, ok := metadata["plain_text"].(string); ok && plainText == "" {
		plainText = v
	}
	delete(metadata, "plain_text")

	record := &search.AssetRecord{
		AssetID:         item.Fingerprint,
		TenantID:        tenantID,
		EngineID:      engineID,
		ResourceName:    resource.Name,
		ResourceType:    resource.EngineType,
		AssetType:       "object",
		Name:            item.Name,
		FullName:        fullName,
		Bucket:          meta.Bucket,
		Path:            meta.Path,
		RelativePath:    relativePath,
		Metadata:        metadata,
		SizeBytes:       item.SizeBytes,
		ObjectSizeBytes: item.ObjectSizeBytes,
		LastModified:    meta.LastModified,
	}

	if len(tags) > 0 {
		record.Tags = tags
	}
	if desc := getStringFromMap(metadata, "file_type_friendly"); desc != "" {
		record.Description = desc
	}

	if err := s.indexer.IndexAsset(context.Background(), record); err != nil {
		s.log.Warn("索引对象元数据失败", "fingerprint", item.Fingerprint, "bucket", meta.Bucket, "path", meta.Path, "error", err)
	}

	truncatedContent := truncateRunes(plainText, documentContentRuneLimit)
	contentPreview := previewText(truncatedContent, documentPreviewRuneLimit)
	docMetadata := cloneInterfaceMap(metadata)

	docRecord := &search.DocumentRecord{
		DocumentID:     item.Fingerprint,
		AssetID:        item.Fingerprint,
		TenantID:       tenantID,
		EngineID:     engineID,
		ResourceName:   resource.Name,
		ResourceType:   resource.EngineType,
		Bucket:         meta.Bucket,
		RelativePath:   relativePath,
		FileName:       item.Name,
		FilePath:       meta.Path,
		Content:        truncatedContent,
		ContentPreview: contentPreview,
		FileSize:       meta.SizeBytes,
		Metadata:       docMetadata,
	}

	if meta.ExtractedMetadata != nil {
		if basic := meta.ExtractedMetadata.BasicInfo; basic.FileName != "" {
			docRecord.FileName = basic.FileName
		}
		if basic := meta.ExtractedMetadata.BasicInfo; basic.ContentType != "" {
			docRecord.ContentType = basic.ContentType
		}
		if basic := meta.ExtractedMetadata.BasicInfo; !basic.LastModified.IsZero() {
			lm := basic.LastModified
			docRecord.LastModified = &lm
		} else if meta.LastModified != nil {
			docRecord.LastModified = meta.LastModified
		}
	}
	if docRecord.LastModified == nil && meta.LastModified != nil {
		docRecord.LastModified = meta.LastModified
	}

	if value := getStringFromMap(metadata, "document_type"); value != "" {
		docRecord.DocumentType = value
	}
	if value := getStringFromMap(metadata, "title"); value != "" {
		docRecord.Title = value
	}
	if value := getStringFromMap(metadata, "author"); value != "" {
		docRecord.Author = value
	}
	if keywords := extractStringSlice(metadata["keywords"]); len(keywords) > 0 {
		docRecord.Keywords = keywords
	}
	if wc := intFromInterface(metadata["word_count"]); wc > 0 {
		docRecord.WordCount = wc
	}
	if pc := intFromInterface(metadata["page_count"]); pc > 0 {
		docRecord.PageCount = pc
	}
	if created := extractTimePtr(metadata["created_date"]); created != nil {
		docRecord.CreatedDate = created
	}
	if modified := extractTimePtr(metadata["modified_date"]); modified != nil {
		docRecord.ModifiedDate = modified
	}

	if err := s.indexer.IndexDocument(context.Background(), docRecord); err != nil {
		s.log.Warn("索引文档内容失败", "fingerprint", item.Fingerprint, "error", err)
	}

	s.upsertDocumentEmbedding(docRecord, truncatedContent)
}

func (s *ScanServiceNew) upsertDocumentEmbedding(docRecord *search.DocumentRecord, content string) {
	if s.vectorStore == nil || s.multiModalEmbedder == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.resolveEmbeddingTimeout())
	defer cancel()

	modality := detectDocumentModality(docRecord)

	var (
		embeddingResult *embedding.Embedding
		usage           *embedding.Usage
		err             error
	)

	switch modality {
	case embedding.ModalityImage:
		embeddingResult, usage, err = s.generateImageEmbedding(ctx, docRecord)
	case embedding.ModalityVideo:
		embeddingResult, usage, err = s.generateVideoEmbedding(ctx, docRecord)
	default:
		embeddingResult, usage, err = s.generateDocumentEmbedding(ctx, docRecord, content)
	}

	if err != nil {
		s.log.Warn("文档向量化失败", "document_id", docRecord.DocumentID, "modality", string(modality), "error", err)
		return
	}
	if embeddingResult == nil {
		return
	}

	vectorMeta := s.buildVectorMetadata(docRecord, modality, embeddingResult, usage)
	record := vectorstore.Record{
		ObjectID:  docRecord.DocumentID,
		AssetID:   docRecord.AssetID,
		Modality:  modality,
		Model:     embeddingResult.Model,
		Vector:    embeddingResult.Vector,
		Metadata:  vectorMeta,
		Dimension: embeddingResult.Dimension,
	}

	if docRecord.TenantID != 0 {
		tenantID := docRecord.TenantID
		record.TenantID = &tenantID
	}
	if docRecord.EngineID != 0 {
		resourceID := docRecord.EngineID
		record.EngineID = &resourceID
	}

	dbCtx, cancelDB := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelDB()

	if _, err := s.vectorStore.Upsert(dbCtx, record); err != nil {
		s.log.Warn("向量存储写入失败", "document_id", docRecord.DocumentID, "error", err)
	}
}

func (s *ScanServiceNew) resolveEmbeddingTimeout() time.Duration {
	if s.embeddingTimeout <= 0 {
		return 20 * time.Second
	}
	return s.embeddingTimeout
}

func detectDocumentModality(docRecord *search.DocumentRecord) embedding.Modality {
	contentType := strings.ToLower(strings.TrimSpace(docRecord.ContentType))
	if contentType == "" {
		if metaType := strings.ToLower(strings.TrimSpace(getStringFromMap(docRecord.Metadata, "content_type"))); metaType != "" {
			contentType = metaType
		}
	}
	if contentType == "" {
		if metaType := strings.ToLower(strings.TrimSpace(getStringFromMap(docRecord.Metadata, "mime_type"))); metaType != "" {
			contentType = metaType
		}
	}
	if contentType == "" {
		if ext := strings.ToLower(strings.TrimSpace(pathpkg.Ext(docRecord.FileName))); ext != "" {
			if inferred := strings.ToLower(strings.TrimSpace(mime.TypeByExtension(ext))); inferred != "" {
				contentType = inferred
			}
		}
	}
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return embedding.ModalityImage
	case strings.HasPrefix(contentType, "video/"):
		return embedding.ModalityVideo
	case strings.HasPrefix(contentType, "audio/"):
		return embedding.ModalityAudio
	default:
		return embedding.ModalityDocument
	}
}

func (s *ScanServiceNew) generateDocumentEmbedding(ctx context.Context, docRecord *search.DocumentRecord, content string) (*embedding.Embedding, *embedding.Usage, error) {
	text := strings.TrimSpace(content)
	if text == "" {
		text = strings.TrimSpace(docRecord.ContentPreview)
	}
	if text == "" {
		return nil, nil, nil
	}

	trimmedContent := truncateRunes(text, documentEmbeddingRuneLimit)
	embeddingText := strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(docRecord.Title),
		strings.TrimSpace(trimmedContent),
	}, "\n\n"))
	if embeddingText == "" {
		return nil, nil, nil
	}

	language := ""
	if docRecord.Metadata != nil {
		language = getStringFromMap(docRecord.Metadata, "language")
	}

	metaForEmbedding := map[string]string{
		"file_name": docRecord.FileName,
		"bucket":    docRecord.Bucket,
	}
	if docRecord.RelativePath != "" {
		metaForEmbedding["relative_path"] = docRecord.RelativePath
	}

	result, err := s.multiModalEmbedder.EmbedDocument(ctx, []embedding.DocumentInput{
		{
			ID:       docRecord.DocumentID,
			Content:  embeddingText,
			Title:    docRecord.Title,
			Language: language,
			Metadata: metaForEmbedding,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	if result == nil || len(result.Embeddings) == 0 {
		return nil, nil, fmt.Errorf("embedding service returned empty result")
	}

	embeddingResult := result.Embeddings[0]
	return &embeddingResult, result.Usage, nil
}

func (s *ScanServiceNew) generateImageEmbedding(ctx context.Context, docRecord *search.DocumentRecord) (*embedding.Embedding, *embedding.Usage, error) {
	if strings.TrimSpace(docRecord.Bucket) == "" {
		return nil, nil, nil
	}

	objectPath := docRecord.RelativePath
	if objectPath == "" {
		objectPath = docRecord.FilePath
	}

	data, contentType, err := s.fetchObjectContent(ctx, docRecord.EngineID, docRecord.TenantID, docRecord.Bucket, objectPath, maxImageEmbeddingBytes)
	if err != nil {
		return nil, nil, err
	}
	if len(data) == 0 {
		return nil, nil, nil
	}

	mimeType := normalizeMimeType(contentType, docRecord.ContentType, docRecord.FileName, "image/png")

	meta := map[string]string{
		"file_name":     docRecord.FileName,
		"bucket":        docRecord.Bucket,
		"relative_path": docRecord.RelativePath,
	}

	result, err := s.multiModalEmbedder.EmbedImage(ctx, []embedding.ImageInput{{
		ID:       docRecord.DocumentID,
		Data:     data,
		MIMEType: mimeType,
		Metadata: meta,
	}})
	if err != nil {
		return nil, nil, err
	}
	if result == nil || len(result.Embeddings) == 0 {
		return nil, nil, fmt.Errorf("embedding service returned empty result")
	}

	embeddingResult := result.Embeddings[0]
	return &embeddingResult, result.Usage, nil
}

func (s *ScanServiceNew) generateVideoEmbedding(ctx context.Context, docRecord *search.DocumentRecord) (*embedding.Embedding, *embedding.Usage, error) {
	if strings.TrimSpace(docRecord.Bucket) == "" {
		return nil, nil, nil
	}

	objectPath := docRecord.RelativePath
	if objectPath == "" {
		objectPath = docRecord.FilePath
	}

	data, contentType, err := s.fetchObjectContent(ctx, docRecord.EngineID, docRecord.TenantID, docRecord.Bucket, objectPath, maxVideoEmbeddingBytes)
	if err != nil {
		return nil, nil, err
	}
	if len(data) == 0 {
		return nil, nil, nil
	}

	mimeType := normalizeMimeType(contentType, docRecord.ContentType, docRecord.FileName, "video/mp4")

	meta := map[string]string{
		"file_name":     docRecord.FileName,
		"bucket":        docRecord.Bucket,
		"relative_path": docRecord.RelativePath,
	}

	result, err := s.multiModalEmbedder.EmbedVideo(ctx, []embedding.VideoInput{{
		ID:       docRecord.DocumentID,
		Data:     data,
		MIMEType: mimeType,
		Metadata: meta,
	}})
	if err != nil {
		return nil, nil, err
	}
	if result == nil || len(result.Embeddings) == 0 {
		return nil, nil, fmt.Errorf("embedding service returned empty result")
	}

	embeddingResult := result.Embeddings[0]
	return &embeddingResult, result.Usage, nil
}

func (s *ScanServiceNew) buildVectorMetadata(docRecord *search.DocumentRecord, modality embedding.Modality, embed *embedding.Embedding, usage *embedding.Usage) map[string]any {
	meta := map[string]any{
		"file_name":     docRecord.FileName,
		"bucket":        docRecord.Bucket,
		"relative_path": docRecord.RelativePath,
		"engine_id":   docRecord.EngineID,
		"resource_name": docRecord.ResourceName,
		"resource_type": docRecord.ResourceType,
		"document_type": docRecord.DocumentType,
		"content_type":  docRecord.ContentType,
		"modality":      string(modality),
	}
	if docRecord.Title != "" {
		meta["title"] = docRecord.Title
	}
	if docRecord.ContentPreview != "" {
		meta["content_preview"] = docRecord.ContentPreview
	}
	if docRecord.Metadata != nil && len(docRecord.Metadata) > 0 {
		meta["source_metadata"] = docRecord.Metadata
	}
	if embed != nil && len(embed.Metadata) > 0 {
		meta["embedding_metadata"] = embed.Metadata
	}
	if usage != nil {
		meta["vector_usage"] = map[string]any{
			"prompt_tokens": usage.PromptTokens,
			"total_tokens":  usage.TotalTokens,
			"latency_ms":    usage.Latency.Milliseconds(),
		}
	}
	return meta
}

func (s *ScanServiceNew) fetchObjectContent(ctx context.Context, engineID, tenantID uint, bucket, objectPath string, maxSize int64) ([]byte, string, error) {
	client, err := s.getObjectClient(engineID, tenantID)
	if err != nil {
		return nil, "", err
	}

	path := strings.TrimPrefix(strings.TrimSpace(objectPath), "/")
	if path == "" {
		return nil, "", fmt.Errorf("object path is empty")
	}

	stat, err := client.StatObject(ctx, bucket, path, minio.StatObjectOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("stat object failed: %w", err)
	}
	if maxSize > 0 && stat.Size > maxSize {
		return nil, "", fmt.Errorf("object size %d exceeds limit %d", stat.Size, maxSize)
	}

	reader, err := client.GetObject(ctx, bucket, path, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("get object failed: %w", err)
	}
	defer reader.Close()

	limit := stat.Size
	if maxSize > 0 && limit > maxSize {
		limit = maxSize
	}

	data, err := io.ReadAll(io.LimitReader(reader, limit))
	if err != nil {
		return nil, "", fmt.Errorf("read object failed: %w", err)
	}

	return data, stat.ContentType, nil
}

func (s *ScanServiceNew) getObjectClient(engineID, tenantID uint) (*minio.Client, error) {
	s.objectClientMu.Lock()
	client, ok := s.objectClients[engineID]
	s.objectClientMu.Unlock()
	if ok && client != nil {
		return client, nil
	}

	resource, err := s.resourceService.GetResourceByID(engineID, tenantID, "")
	if err != nil {
		return nil, fmt.Errorf("load resource connection info failed: %w", err)
	}

	cfg, err := parseObjectStorageConfig(resource.ConnectionInfo)
	if err != nil {
		return nil, err
	}

	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	}
	if cfg.Region != "" {
		opts.Region = cfg.Region
	}
	if cfg.PathStyle {
		opts.BucketLookup = minio.BucketLookupPath
	}

	client, err = minio.New(cfg.Endpoint, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	s.objectClientMu.Lock()
	s.objectClients[resourceID] = client
	s.objectClientMu.Unlock()

	return client, nil
}

type objectStorageConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
	PathStyle bool
}

func parseObjectStorageConfig(info commonModels.ConnectionInfo) (*objectStorageConfig, error) {
	cfg := &objectStorageConfig{}

	cfg.Endpoint = getStringFromConn(info, "endpoint")
	cfg.AccessKey = getStringFromConn(info, "access_key")
	cfg.SecretKey = getStringFromConn(info, "secret_key")
	cfg.Region = getStringFromConn(info, "region")
	cfg.UseSSL = getBoolFromConn(info, "use_ssl")
	cfg.PathStyle = getBoolFromConn(info, "path_style")

	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("object storage endpoint is empty")
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("object storage credentials missing")
	}

	return cfg, nil
}

func getStringFromConn(info commonModels.ConnectionInfo, key string) string {
	if raw, ok := info[key]; ok {
		switch v := raw.(type) {
		case string:
			return v
		case fmt.Stringer:
			return v.String()
		case float64:
			return fmt.Sprintf("%.0f", v)
		case int64:
			return fmt.Sprintf("%d", v)
		case int:
			return fmt.Sprintf("%d", v)
		case bool:
			if v {
				return "true"
			}
			return "false"
		}
	}
	return ""
}

func getBoolFromConn(info commonModels.ConnectionInfo, key string) bool {
	if raw, ok := info[key]; ok {
		switch v := raw.(type) {
		case bool:
			return v
		case string:
			lower := strings.ToLower(strings.TrimSpace(v))
			return lower == "true" || lower == "1" || lower == "yes"
		case float64:
			return v != 0
		case int:
			return v != 0
		case int64:
			return v != 0
		}
	}
	return false
}

func normalizeMimeType(primary, secondary, fileName, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	if strings.TrimSpace(secondary) != "" {
		return strings.TrimSpace(secondary)
	}
	if ext := strings.ToLower(strings.TrimSpace(pathpkg.Ext(fileName))); ext != "" {
		if mimeType := mime.TypeByExtension(ext); mimeType != "" {
			return mimeType
		}
	}
	return fallback
}

func (s *ScanServiceNew) deleteTablesFromIndex(tenantID, engineID uint, schemaName string) {
	if s.indexer == nil || !s.indexer.Enabled() || schemaName == "" {
		return
	}
	if err := s.indexer.DeleteTables(context.Background(), tenantID, engineID, schemaName); err != nil {
		s.log.Warn("删除表索引失败", "schema", schemaName, "engine_id", engineID, "error", err)
	}
}

func (s *ScanServiceNew) deleteObjectsFromIndex(tenantID, engineID uint, bucketName, relativePath string) {
	if s.indexer == nil || !s.indexer.Enabled() || bucketName == "" {
		return
	}
	if err := s.indexer.DeleteObjects(context.Background(), tenantID, engineID, bucketName, relativePath); err != nil {
		s.log.Warn("删除对象索引失败", "bucket", bucketName, "path", relativePath, "error", err)
	}
}

func copyJSONMap(m models.JSONMap) map[string]interface{} {
	if m == nil {
		return nil
	}
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

func cloneInterfaceMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		switch vv := v.(type) {
		case map[string]interface{}:
			result[k] = cloneInterfaceMap(vv)
		case []interface{}:
			arr := make([]interface{}, 0, len(vv))
			for _, item := range vv {
				switch nested := item.(type) {
				case map[string]interface{}:
					arr = append(arr, cloneInterfaceMap(nested))
				default:
					arr = append(arr, nested)
				}
			}
			result[k] = arr
		default:
			result[k] = vv
		}
	}
	return result
}

func extractStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if item != "" {
				out = append(out, item)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, raw := range v {
			if s, ok := raw.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}

func getStringFromMap(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	if val, ok := metadata[key]; ok {
		switch v := val.(type) {
		case string:
			return v
		case fmt.Stringer:
			return v.String()
		case []byte:
			return string(v)
		default:
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

func truncateRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func previewText(text string, limit int) string {
	return truncateRunes(text, limit)
}

func intFromInterface(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case json.Number:
		if i64, err := v.Int64(); err == nil {
			return int(i64)
		}
	case string:
		if v == "" {
			return 0
		}
		if i64, err := strconv.ParseInt(v, 10, 64); err == nil {
			return int(i64)
		}
	}
	return 0
}

func extractTimePtr(value interface{}) *time.Time {
	switch v := value.(type) {
	case time.Time:
		if v.IsZero() {
			return nil
		}
		vv := v
		return &vv
	case *time.Time:
		if v == nil || v.IsZero() {
			return nil
		}
		vv := v.UTC()
		return &vv
	case string:
		if v == "" {
			return nil
		}
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			return &parsed
		}
	}
	return nil
}

// extractEnhancedMetadata 使用插件提取增强的元数据
func (s *ScanServiceNew) extractEnhancedMetadata(engineID uint, meta plugins.ObjectMetadata, baseAttrs models.JSONMap) models.JSONMap {
	// 如果S3Scanner已经提取了元数据，直接使用
	if meta.ExtractedMetadata != nil && meta.ExtractedMetadata.CustomAttrs != nil {
		if text, ok := meta.ExtractedMetadata.CustomAttrs["plain_text"].(string); ok && text != "" {
			baseAttrs["plain_text_preview"] = previewText(text, documentPreviewRuneLimit)
		}
		// 将提取的CustomAttrs合并到baseAttrs中，跳过大字段
		for key, value := range meta.ExtractedMetadata.CustomAttrs {
			if key == "plain_text" {
				continue
			}
			baseAttrs[key] = value
		}

		// 添加基本信息
		baseAttrs["metadata_extracted"] = true
		baseAttrs["file_type_friendly"] = meta.ExtractedMetadata.BasicInfo.FileType
		if meta.ExtractedMetadata.BasicInfo.ContentType != "" {
			baseAttrs["content_type"] = meta.ExtractedMetadata.BasicInfo.ContentType
		}

		return baseAttrs
	}

	// 如果没有提取元数据，检查是否有可用的提取器
	contentType := mime.TypeByExtension(pathpkg.Ext(meta.Path))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 获取适配的元数据提取器
	extractor := plugins.GetExtractor(contentType)
	if extractor == nil {
		// 没有合适的提取器，返回基础属性
		return baseAttrs
	}

	// 标记有提取器可用（但本次扫描未提取）
	baseAttrs["extractor_available"] = true
	baseAttrs["content_type"] = contentType

	return baseAttrs
}

// extractEnhancedMetadataWithCache 带缓存检查的元数据提取
// 如果文件未修改（基于last_modified时间），跳过重新提取
// fullPath: 文件在bucket中的完整路径（用于fingerprint生成）
func (s *ScanServiceNew) extractEnhancedMetadataWithCache(engineID uint, meta plugins.ObjectMetadata, baseAttrs models.JSONMap, fullPath string) models.JSONMap {
	// 生成fingerprint以查找已有记录 - 使用传入的fullPath确保一致性
	bucket, _ := baseAttrs["bucket"].(string)
	fingerprint := commonModels.GenerateObjectFingerprint(engineID, bucket, fullPath)

	// 查找已有的meta_item记录
	var existingItem models.MetaItem
	err := s.db.Where("fingerprint = ?", fingerprint).First(&existingItem).Error

	// 如果找到已有记录，检查last_modified是否变化
	if err == nil && existingItem.LastModifiedAt != nil && meta.LastModified != nil {
		// 比较时间（精确到秒）
		existingTime := existingItem.LastModifiedAt.Truncate(time.Second)
		newTime := meta.LastModified.Truncate(time.Second)

		if existingTime.Equal(newTime) {
			// 文件未变化，复用已有的metadata
			if existingItem.Attributes != nil && len(existingItem.Attributes) > 0 {
				// 检查是否已经提取过元数据
				if extracted, ok := existingItem.Attributes["metadata_extracted"].(bool); ok && extracted {
					s.log.Debug("复用缓存的元数据",
						"fingerprint", fingerprint,
						"fullPath", fullPath,
						"last_modified", existingTime)
					// 将已有的attributes合并到baseAttrs
					for key, value := range existingItem.Attributes {
						baseAttrs[key] = value
					}
					return baseAttrs
				}
			}
		}
	}

	// 文件是新的或已更新，执行完整的元数据提取
	return s.extractEnhancedMetadata(engineID, meta, baseAttrs)
}

// GetObjectMetadata 获取指定对象的元数据
// 用于Manager模块预览时查询已扫描的元数据
func (s *ScanServiceNew) GetObjectMetadata(tenantID, engineID uint, objectKey string) (*models.MetaItem, error) {
	// 解析对象路径
	bucket, relativePath := splitObjectPath(objectKey)
	if bucket == "" {
		return nil, fmt.Errorf("invalid object key: %s", objectKey)
	}

	// 查找bucket节点
	var bucketNode models.MetaNode
	err := s.db.Where("tenant_id = ? AND res_id = ? AND node_type = ? AND name = ?",
		tenantID, engineID, "bucket", bucket).First(&bucketNode).Error
	if err != nil {
		return nil, fmt.Errorf("bucket not found: %w", err)
	}

	// 查找对象item
	objectName := pathpkg.Base(relativePath)
	if objectName == "" {
		objectName = relativePath
	}

	var item models.MetaItem

	// 如果有相对路径，需要先找到对应的prefix节点
	if relativePath != "" && relativePath != objectName {
		// 构建prefix路径
		prefixPath := pathpkg.Dir(relativePath)
		segments := strings.Split(prefixPath, "/")

		currentParent := &bucketNode
		for _, segment := range segments {
			if segment == "" || segment == "." {
				continue
			}

			var prefixNode models.MetaNode
			err := s.db.Where("tenant_id = ? AND res_id = ? AND parent_node_id = ? AND node_type = ? AND name = ?",
				tenantID, engineID, currentParent.ID, "prefix", segment).First(&prefixNode).Error
			if err != nil {
				return nil, fmt.Errorf("prefix not found: %s", segment)
			}
			currentParent = &prefixNode
		}

		// 在最终的prefix节点下查找对象
		err = s.db.Where("tenant_id = ? AND res_id = ? AND node_id = ? AND item_type = ? AND name = ?",
			tenantID, engineID, currentParent.ID, "object", objectName).First(&item).Error
	} else {
		// 直接在bucket下查找对象
		err = s.db.Where("tenant_id = ? AND res_id = ? AND node_id = ? AND item_type = ? AND name = ?",
			tenantID, engineID, bucketNode.ID, "object", objectName).First(&item).Error
	}

	if err != nil {
		return nil, fmt.Errorf("object metadata not found: %w", err)
	}

	return &item, nil
}

// ExtractObjectMetadataOnDemand 按需提取对象的深度元数据
// 当Manager预览时发现元数据未提取，可以调用此方法触发实时提取
func (s *ScanServiceNew) ExtractObjectMetadataOnDemand(tenantID, engineID uint, objectKey string, token string, objectReader io.Reader) (*plugins.Metadata, error) {
	// 检测内容类型
	contentType := mime.TypeByExtension(pathpkg.Ext(objectKey))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 获取元数据提取器
	extractor := plugins.GetExtractor(contentType)
	if extractor == nil {
		return nil, fmt.Errorf("no extractor available for content type: %s", contentType)
	}

	// 获取对象的基本信息
	item, err := s.GetObjectMetadata(tenantID, engineID, objectKey)
	if err != nil {
		// 如果元数据不存在，使用默认值
		s.log.Warn("对象元数据不存在，使用默认值", "object_key", objectKey, "error", err)
	}

	// 构建提取输入
	input := plugins.ExtractInput{
		EngineID:  engineID,
		ObjectKey:   objectKey,
		ContentType: contentType,
		Reader:      objectReader,
	}

	if item != nil {
		if item.ObjectSizeBytes != nil {
			input.Size = *item.ObjectSizeBytes
		}
		if item.LastModifiedAt != nil {
			input.LastModified = *item.LastModifiedAt
		}
		if etag, ok := item.Attributes["etag"].(string); ok {
			input.ETag = etag
		}
	}

	// 调用提取器
	ctx := context.Background()
	metadata, err := extractor.Extract(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to extract metadata: %w", err)
	}

	// 如果元数据item存在，更新attributes
	if item != nil {
		enhancedAttrs := item.Attributes
		if enhancedAttrs == nil {
			enhancedAttrs = make(models.JSONMap)
		}

		// 合并提取的元数据到attributes
		enhancedAttrs["extracted_metadata"] = map[string]interface{}{
			"basic_info":   metadata.BasicInfo,
			"custom_attrs": metadata.CustomAttrs,
		}

		if metadata.SchemaInfo != nil {
			enhancedAttrs["schema_info"] = metadata.SchemaInfo
		}

		// 更新数据库
		if err := s.db.Model(item).Update("attributes", enhancedAttrs).Error; err != nil {
			s.log.Warn("更新元数据失败", "item_id", item.ID, "error", err)
		}
	}

	return metadata, nil
}

func prepareObjectPaths(paths, fallback []string, scanner plugins.ObjectStorageScanner) []string {
	pathSet := map[string]struct{}{}
	for _, p := range paths {
		clean := sanitizeObjectPath(p)
		if clean != "" {
			pathSet[clean] = struct{}{}
		}
	}

	if len(pathSet) == 0 {
		for _, p := range fallback {
			clean := sanitizeObjectPath(p)
			if clean != "" {
				pathSet[clean] = struct{}{}
			}
		}
	}

	if len(pathSet) == 0 {
		for _, bucket := range scanner.AllowedBuckets() {
			clean := sanitizeObjectPath(bucket)
			if clean != "" {
				pathSet[clean] = struct{}{}
			}
		}
	}

	var result []string
	for p := range pathSet {
		result = append(result, p)
	}
	sort.Strings(result)
	return result
}

func filterObjectMetasForDepth(metas []plugins.ObjectMetadata, basePath string) []plugins.ObjectMetadata {
	base := sanitizeObjectPath(basePath)
	if len(metas) == 0 {
		return metas
	}

	filtered := make([]plugins.ObjectMetadata, 0, len(metas))
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

func sanitizeObjectPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, "/")
	return path
}

func splitObjectPath(path string) (string, string) {
	clean := sanitizeObjectPath(path)
	if clean == "" {
		return "", ""
	}
	parts := strings.SplitN(clean, "/", 2)
	bucket := parts[0]
	if bucket == "" {
		return "", ""
	}
	if len(parts) == 1 {
		return bucket, ""
	}
	return bucket, parts[1]
}

func (s *ScanServiceNew) clearObjectMetadataUnderPath(tenantID, engineID uint, bucketNode *models.MetaNode, bucketName, relativePath string) error {
	clean := sanitizeObjectPath(relativePath)
	s.deleteObjectsFromIndex(tenantID, engineID, bucketName, clean)
	if clean == "" {
		if err := s.hardDeleteDescendantNodes(bucketNode); err != nil {
			return err
		}
		return s.hardDeleteItemsByNode(bucketNode.ID)
	}

	var targetNodes []models.MetaNode
	if err := s.db.
		Where("tenant_id = ? AND res_id = ?", tenantID, engineID).
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
		Where("tenant_id = ? AND res_id = ?", tenantID, engineID).
		Where("(attributes ->> 'bucket') = ?", bucketName).
		Where("(attributes ->> 'relative_path') = ? OR (attributes ->> 'relative_path') LIKE ?", clean, clean+"/%").
		Delete(&models.MetaItem{}).Error; err != nil {
		return fmt.Errorf("failed to delete object items by relative path: %w", err)
	}

	return nil
}

func (s *ScanServiceNew) scanDatabaseSchema(resource *commonModels.Engine, scan plugins.Scanner, tenantID, engineID uint, schemaName string, scanDepth string) (int, int, int, error) {
	schemaNode, err := s.upsertNode(tenantID, engineID, nil, "schema", schemaName, "", nil)
	if err != nil {
		return 0, 0, 0, err
	}

	if err := s.resetNodeState(schemaNode, "扫描中"); err != nil {
		return 0, 0, 0, err
	}

	// 判断是否为深度扫描
	isDeepScan := strings.EqualFold(scanDepth, "deep")

	// 增量更新逻辑：查询已存在的表items，建立 tableName -> item 映射
	var existingItems []models.MetaItem
	if err := s.db.Where("tenant_id = ? AND res_id = ? AND node_id = ? AND item_type = ?",
		tenantID, engineID, schemaNode.ID, "table").Find(&existingItems).Error; err != nil {
		s.log.Warn("查询已存在表元数据失败",
			"engine_id", engineID,
			"tenant_id", tenantID,
			"schema", schemaName,
			"error", err,
		)
	}

	existingTableMap := make(map[string]*models.MetaItem)
	for i := range existingItems {
		existingTableMap[existingItems[i].Name] = &existingItems[i]
	}

	s.log.Info("开始扫描 Schema",
		"tenant_id", tenantID,
		"engine_id", engineID,
		"schema", schemaName,
		"scan_depth", scanDepth,
		"existing_tables", len(existingTableMap),
	)

	s.log.Info("🔍 即将调用 scan.ScanTables",
		"schema", schemaName,
		"engine_id", engineID,
	)

	tables, err := scan.ScanTables(schemaName)

	s.log.Info("📊 scan.ScanTables 返回结果",
		"schema", schemaName,
		"tables_count", len(tables),
		"has_error", err != nil,
		"error", err,
	)

	if err != nil {
		s.finalizeNodeState(schemaNode, "未扫描", 0, 0, err.Error())
		return 0, 0, 0, err
	}

	if len(tables) == 0 {
		s.log.Warn("⚠️  ScanTables 返回空数组",
			"schema", schemaName,
			"engine_id", engineID,
		)
	}

	totalTables := 0
	totalFields := 0
	var totalSize int64
	scannedTables := make(map[string]bool) // 记录本次扫描到的表

	s.log.Info("🔄 开始处理扫描到的表",
		"schema", schemaName,
		"total_tables_from_scanner", len(tables),
	)

	for i, tableInfo := range tables {
		s.log.Info(fmt.Sprintf("📋 处理第 %d/%d 张表", i+1, len(tables)),
			"table_name", tableInfo.Name,
			"table_type", tableInfo.Type,
			"row_count", tableInfo.RowCount,
			"size_bytes", tableInfo.SizeBytes,
		)

		var fields []plugins.FieldInfo
		var attrs models.JSONMap

		scannedTables[tableInfo.Name] = true

		// 检查是否需要更新：对比 RowCount 和 SizeBytes
		existingItem := existingTableMap[tableInfo.Name]
		needsUpdate := existingItem == nil || // 新表
			(existingItem.RowCount != nil && *existingItem.RowCount != tableInfo.RowCount) || // 行数变化
			(existingItem.SizeBytes != nil && *existingItem.SizeBytes != tableInfo.SizeBytes) || // 大小变化
			(existingItem.RowCount == nil && tableInfo.RowCount != 0) || // 之前未记录行数
			(existingItem.SizeBytes == nil && tableInfo.SizeBytes != 0) // 之前未记录大小

		s.log.Info("🔍 检查更新条件",
			"table", tableInfo.Name,
			"existing_item", existingItem != nil,
			"needs_update", needsUpdate,
			"is_deep_scan", isDeepScan,
		)

		// 浅度扫描时，如果表未变化，跳过更新（保留已有的深度元数据）
		if !isDeepScan && existingItem != nil && !needsUpdate {
			s.log.Debug("表未变化，跳过更新",
				"schema", schemaName,
				"table", tableInfo.Name,
			)
			totalTables++
			totalSize += tableInfo.SizeBytes
			continue
		}

		// 浅度扫描：跳过字段扫描
		if isDeepScan {
			s.log.Info("开始扫描字段",
				"table", tableInfo.Name,
			)
			fields, err = scan.ScanFields(schemaName, tableInfo.Name)
			if err != nil {
				s.log.Warn("❌ 字段扫描失败，跳过此表",
					"engine_id", engineID,
					"tenant_id", tenantID,
					"schema", schemaName,
					"table", tableInfo.Name,
					"error", err,
				)
				continue
			}

			s.log.Info("✅ 字段扫描成功",
				"table", tableInfo.Name,
				"field_count", len(fields),
			)

			attrs = models.JSONMap{
				"schema":        schemaName,
				"table_type":    tableInfo.Type,
				"table_comment": tableInfo.Comment,
				"fields":        buildFieldAttributes(fields),
			}

			// 扫描空间元数据（仅 PostgreSQL）
			if resource.EngineType == "postgresql" {
				// 尝试从 scanner 中获取数据库连接
				type dbGetter interface {
					GetDB() *sql.DB
				}
				if dbScanner, ok := scan.(dbGetter); ok {
					spatialMeta, err := s.scanSpatialMetadata(context.Background(), dbScanner.GetDB(), schemaName, tableInfo.Name)
					if err != nil {
						s.log.Warn("空间元数据扫描失败",
							"schema", schemaName,
							"table", tableInfo.Name,
							"error", err,
						)
					} else if spatialMeta != nil {
						// 将空间元数据添加到 attributes
						attrs["spatial_metadata"] = spatialMeta
						s.log.Info("✅ 空间元数据扫描成功",
							"table", tableInfo.Name,
							"geometry_column", spatialMeta.GeometryColumn,
							"srid", spatialMeta.SRID,
							"extent", spatialMeta.Extent,
						)
					}
				}
			}
		} else {
			// 浅度扫描：不包含字段信息（保留已有的字段信息）
			if existingItem != nil && existingItem.Attributes != nil {
				// 保留已有的 fields，只更新基本信息
				attrs = existingItem.Attributes
				attrs["schema"] = schemaName
				attrs["table_type"] = tableInfo.Type
				attrs["table_comment"] = tableInfo.Comment
			} else {
				attrs = models.JSONMap{
					"schema":        schemaName,
					"table_type":    tableInfo.Type,
					"table_comment": tableInfo.Comment,
				}
			}
		}

		s.log.Info("准备写入表元数据",
			"table", tableInfo.Name,
			"full_name", composeNodeFullName(tableInfo.Name, schemaNode, "."),
		)

		rowCount := tableInfo.RowCount
		sizeBytes := tableInfo.SizeBytes

		fullName := composeNodeFullName(tableInfo.Name, schemaNode, ".")
		item, err := s.upsertItem(tenantID, engineID, schemaNode, "table", tableInfo.Name, fullName, attrs, &rowCount, &sizeBytes, nil, nil, 1)
		if err != nil {
			s.log.Error("❌ 表元数据持久化失败，跳过此表",
				"engine_id", engineID,
				"tenant_id", tenantID,
				"schema", schemaName,
				"table", tableInfo.Name,
				"error", err,
			)
			continue
		}

		s.log.Info("✅ 表元数据写入成功",
			"table", tableInfo.Name,
			"item_id", item.ID,
		)

		// 深度扫描才索引表资产
		if isDeepScan {
			s.indexTableAsset(resource, tenantID, schemaName, tableInfo, fields, item)
		}

		totalTables++
		totalFields += len(fields)
		totalSize += tableInfo.SizeBytes
	}

	// 软删除那些不再存在的表
	for tableName, item := range existingTableMap {
		if !scannedTables[tableName] {
			s.log.Info("表已不存在，标记删除",
				"schema", schemaName,
				"table", tableName,
			)
			if err := s.db.Delete(item).Error; err != nil {
				s.log.Warn("软删除表元数据失败",
					"schema", schemaName,
					"table", tableName,
					"error", err,
				)
			}
			// 从索引中删除
			s.deleteTablesFromIndex(tenantID, engineID, schemaName)
		}
	}

	s.log.Info("Schema 扫描完成",
		"tenant_id", tenantID,
		"engine_id", engineID,
		"schema", schemaName,
		"tables", totalTables,
		"fields", totalFields,
		"total_size_bytes", totalSize,
	)

	if err := s.finalizeNodeState(schemaNode, "已扫描", totalTables, totalSize, ""); err != nil {
		return 0, totalTables, totalFields, err
	}

	return 1, totalTables, totalFields, nil
}

// GetSchemasByResource 获取资源的所有Schema
func (s *ScanServiceNew) GetSchemasByResource(engineID, tenantID uint) ([]*models.SchemaWithStatus, error) {
	var nodes []models.MetaNode
	if err := s.db.Where("tenant_id = ? AND res_id = ? AND parent_node_id IS NULL", tenantID, engineID).
		Order("name").
		Find(&nodes).Error; err != nil {
		return nil, err
	}

	result := make([]*models.SchemaWithStatus, 0, len(nodes))
	for _, node := range nodes {
		item := &models.SchemaWithStatus{
			ID:              node.ID,
			SchemaName:      node.Name,
			ScanStatus:      node.ScanStatus,
			TableCount:      node.ItemCount,
			TotalSizeBytes:  node.TotalSizeBytes,
			AutoScanEnabled: node.AutoScanEnabled,
			AutoScanCron:    node.AutoScanCron,
		}
		if node.LastScanAt != nil {
			item.LastScanAt = node.LastScanAt.Format("2006-01-02 15:04:05")
		}
		if node.NextScanAt != nil {
			item.NextScanAt = node.NextScanAt.Format("2006-01-02 15:04:05")
		}
		result = append(result, item)
	}

	return result, nil
}

// ListAvailableSchemas 列出资源中可用的Schema（从数据库实时查询）
func (s *ScanServiceNew) ListAvailableSchemas(engineID, tenantID uint, token string) ([]*models.SchemaInfo, error) {
	// 1. 获取资源（从System读取，包含connection_status）
	resource, err := s.resourceService.GetResourceByID(engineID, tenantID, token)
	if err != nil {
		return nil, err
	}

	// 2. 尝试实际连接（快速超时3秒）
	scan, err := plugins.NewScanner(resource)
	if err != nil {
		// 连接失败：触发System刷新状态（异步，不阻塞）
		s.resourceService.TriggerConnectionCheck(engineID)

		// 返回失败，附带缓存的状态信息
		if resource.ConnectionStatus == "offline" && resource.CheckMessage != "" {
			return nil, fmt.Errorf("资源离线: %s", resource.CheckMessage)
		}
		return nil, fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	// 3. 获取Schema列表
	schemasInfo, err := scan.ListSchemas()
	if err != nil {
		// 连接成功但查询失败，也触发刷新
		s.resourceService.TriggerConnectionCheck(engineID)
		return nil, err
	}

	// 4. 成功：如果之前是offline，触发刷新状态为online
	if resource.ConnectionStatus == "offline" {
		s.resourceService.TriggerConnectionCheck(engineID)
	}

	// 转换并返回
	var result []*models.SchemaInfo
	for _, info := range schemasInfo {
		result = append(result, &models.SchemaInfo{
			Name: info.Name,
		})
	}

	return result, nil
}

func (s *ScanServiceNew) ListObjectStorageNodes(engineID, tenantID uint, path, token string) ([]*models.ObjectNode, error) {
	resource, err := s.resourceService.GetResourceByID(engineID, tenantID, token)
	if err != nil {
		return nil, err
	}

	if !isObjectStorageType(strings.ToLower(resource.EngineType)) {
		return nil, fmt.Errorf("resource %s is not object storage", resource.EngineType)
	}

	// 重构后：直接传入Resource对象，由插件系统管理连接
	scan, err := plugins.NewScanner(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	objectScanner, ok := scan.(plugins.ObjectStorageScanner)
	if !ok {
		return nil, fmt.Errorf("resource %s is not object storage", resource.EngineType)
	}

	nodes, err := objectScanner.ListNodes(path)
	if err != nil {
		return nil, err
	}

	var result []*models.ObjectNode
	for _, node := range nodes {
		item := &models.ObjectNode{
			Name:        node.Name,
			Path:        node.Path,
			Type:        node.Type,
			SizeBytes:   node.SizeBytes,
			FileType:    node.FileType,
			ObjectCount: node.ObjectCount,
		}
		if node.LastModified != nil {
			item.LastModified = node.LastModified.Format("2006-01-02 15:04:05")
		}
		result = append(result, item)
	}

	return result, nil
}

// updateScanLogFailed 更新扫描日志为失败
func (s *ScanServiceNew) updateScanLogFailed(scanLog *models.ScanLog, errorMsg string) {
	now := time.Now()
	scanLog.Status = "failed"
	scanLog.ErrorMessage = errorMsg
	scanLog.CompletedAt = &now
	if scanLog.StartedAt != nil {
		scanLog.DurationMs = now.Sub(*scanLog.StartedAt).Milliseconds()
	}
	s.db.Save(scanLog)
}

// GetTablesByResource 获取资源下所有的表（用于Transfer模块）
// 返回所有已扫描的表的元数据项
func (s *ScanServiceNew) GetTablesByResource(engineID, tenantID uint) ([]models.MetaItem, error) {
	// 查询该资源下的所有 meta_item（表）
	var items []models.MetaItem

	// 先获取该资源下的所有节点ID（schemas/prefixes）
	var nodeIDs []uint
	err := s.db.Model(&models.MetaNode{}).
		Where("tenant_id = ? AND res_id = ?", tenantID, engineID).
		Pluck("id", &nodeIDs).Error
	if err != nil {
		return nil, err
	}

	if len(nodeIDs) == 0 {
		return []models.MetaItem{}, nil
	}

	// 查询这些节点下的所有 items（表）
	// 注意：meta_item 表中的字段名是 node_id，不是 parent_node_id
	// 使用 Select 明确选择字段，确保 FullName 被加载
	err = s.db.Select("*").
		Where("tenant_id = ? AND node_id IN (?) AND deleted_at IS NULL", tenantID, nodeIDs).
		Order("name").
		Find(&items).Error
	if err != nil {
		return nil, err
	}

	return items, nil
}

// TableFieldInfo 表字段详细信息
type TableFieldInfo struct {
	Name         string `json:"name"`
	DataType     string `json:"data_type,omitempty"`
	ColumnType   string `json:"column_type,omitempty"`
	DefaultValue string `json:"default_value,omitempty"`
	Comment      string `json:"comment,omitempty"`
	IsNullable   bool   `json:"is_nullable,omitempty"`
	IsPrimaryKey bool   `json:"is_primary_key,omitempty"`
	IsUniqueKey  bool   `json:"is_unique_key,omitempty"`
}

// GetTableFields 获取表的字段名列表（用于Transfer模块字段映射）
func (s *ScanServiceNew) GetTableFields(engineID uint, tableName string, tenantID uint) ([]string, error) {
	fieldInfos, err := s.GetTableFieldDetails(engineID, tableName, tenantID)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(fieldInfos))
	for _, field := range fieldInfos {
		if field.Name != "" {
			names = append(names, field.Name)
		}
	}
	return names, nil
}

// GetTableFieldDetails 获取表字段详细信息（支持空间字段识别）
func (s *ScanServiceNew) GetTableFieldDetails(engineID uint, tableName string, tenantID uint) ([]TableFieldInfo, error) {
	// 1. 先获取该资源下的所有节点ID（schema nodes）
	var nodeIDs []uint
	err := s.db.Model(&models.MetaNode{}).
		Where("tenant_id = ? AND res_id = ?", tenantID, engineID).
		Pluck("id", &nodeIDs).Error
	if err != nil {
		return nil, err
	}

	if len(nodeIDs) == 0 {
		return []TableFieldInfo{}, nil
	}

	// 2. 查询指定表名的 meta_item
	// 支持两种格式：带 schema 的全名（如 "products.items"）和不带 schema 的表名（如 "items"）
	var item models.MetaItem
	err = s.db.Where("tenant_id = ? AND node_id IN (?) AND (name = ? OR full_name = ?) AND deleted_at IS NULL",
		tenantID, nodeIDs, tableName, tableName).
		First(&item).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return []TableFieldInfo{}, fmt.Errorf("table '%s' not found in metadata", tableName)
		}
		return nil, err
	}

	// 3. 从 Attributes 中提取字段列表
	// 字段信息存储在 attributes.fields 中，格式为 []map[string]interface{}
	fieldsData, ok := item.Attributes["fields"]
	if !ok {
		return []TableFieldInfo{}, nil
	}

	fieldsList, ok := fieldsData.([]interface{})
	if !ok {
		return []TableFieldInfo{}, fmt.Errorf("invalid fields format in metadata")
	}

	// 4. 构造字段详细信息
	fieldInfos := make([]TableFieldInfo, 0, len(fieldsList))
	for _, fieldData := range fieldsList {
		fieldMap, ok := fieldData.(map[string]interface{})
		if !ok {
			continue
		}

		info := TableFieldInfo{
			Name:         toString(fieldMap["name"]),
			DataType:     toString(fieldMap["data_type"]),
			ColumnType:   toString(fieldMap["column_type"]),
			DefaultValue: toString(fieldMap["default_value"]),
			Comment:      toString(fieldMap["comment"]),
			IsNullable:   toBool(fieldMap["is_nullable"]),
			IsPrimaryKey: toBool(fieldMap["is_primary_key"]),
			IsUniqueKey:  toBool(fieldMap["is_unique_key"]),
		}

		fieldInfos = append(fieldInfos, info)
	}

	return fieldInfos, nil
}

func toString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case fmt.Stringer:
		return v.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toBool(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		lower := strings.ToLower(v)
		return lower == "true" || lower == "1" || lower == "yes"
	case float64:
		return v != 0
	case int:
		return v != 0
	case int64:
		return v != 0
	default:
		return false
	}
}

// ========== 新增：用于 Manager 模块的元数据查询接口 ==========

// GetMetadataTree 获取资源的完整元数据树（顶层节点+子节点+项）
func (s *ScanServiceNew) GetMetadataTree(tenantID, engineID uint) (*models.MetadataTreeResponse, error) {
	// 查询顶层节点
	var topNodes []models.MetaNode
	if err := s.db.Where("tenant_id = ? AND res_id = ? AND parent_node_id IS NULL", tenantID, engineID).
		Find(&topNodes).Error; err != nil {
		return nil, fmt.Errorf("failed to query top nodes: %w", err)
	}

	// 查询子节点
	var childNodes []models.MetaNode
	if err := s.db.Where("tenant_id = ? AND res_id = ? AND parent_node_id IS NOT NULL", tenantID, engineID).
		Find(&childNodes).Error; err != nil {
		return nil, fmt.Errorf("failed to query child nodes: %w", err)
	}

	// 查询所有项
	var items []models.MetaItem
	if err := s.db.Where("tenant_id = ? AND res_id = ?", tenantID, engineID).
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("failed to query items: %w", err)
	}

	// 转换为 Lite 模型
	topNodesLite := make([]models.MetaNodeLite, len(topNodes))
	for i, node := range topNodes {
		topNodesLite[i] = convertToMetaNodeLite(node)
	}

	childNodesLite := make([]models.MetaNodeLite, len(childNodes))
	for i, node := range childNodes {
		childNodesLite[i] = convertToMetaNodeLite(node)
	}

	itemsLite := make([]models.MetaItemLite, len(items))
	for i, item := range items {
		itemsLite[i] = convertToMetaItemLite(item)
	}

	return &models.MetadataTreeResponse{
		TopNodes:   topNodesLite,
		ChildNodes: childNodesLite,
		Items:      itemsLite,
	}, nil
}

// GetNodeByPath 按路径查询节点（支持 Schema/Bucket/Prefix）
func (s *ScanServiceNew) GetNodeByPath(tenantID, engineID uint, nodePath string) (*models.MetaNodeLite, error) {
	var node models.MetaNode

	// 尝试按 full_name 查询
	err := s.db.Where("tenant_id = ? AND res_id = ? AND full_name = ?", tenantID, engineID, nodePath).
		First(&node).Error
	if err == nil {
		result := convertToMetaNodeLite(node)
		return &result, nil
	}

	// 尝试按 name 查询顶层节点
	err = s.db.Where("tenant_id = ? AND res_id = ? AND name = ? AND parent_node_id IS NULL", tenantID, engineID, nodePath).
		First(&node).Error
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	result := convertToMetaNodeLite(node)
	return &result, nil
}

// GetItemByPath 按路径查询项目（对象存储）
func (s *ScanServiceNew) GetItemByPath(tenantID, engineID uint, bucketName, objectPath string) (*models.MetaItemLite, error) {
	var item models.MetaItem

	// 构建完整路径
	fullPath := bucketName
	if objectPath != "" {
		fullPath = bucketName + "/" + strings.TrimPrefix(objectPath, "/")
	}

	// 尝试多种路径匹配方式
	err := s.db.Where("tenant_id = ? AND res_id = ? AND item_type = ?", tenantID, engineID, "object").
		Where("(attributes->>'bucket' = ? AND (attributes->>'path' = ? OR attributes->>'relative_path' = ? OR full_name = ?))",
			bucketName, fullPath, objectPath, fullPath).
		First(&item).Error

	if err != nil {
		return nil, fmt.Errorf("item not found: %w", err)
	}

	result := convertToMetaItemLite(item)
	return &result, nil
}

// GetNodeChildren 获取节点的子节点
func (s *ScanServiceNew) GetNodeChildren(tenantID, nodeID uint) ([]models.MetaNodeLite, error) {
	var nodes []models.MetaNode

	// 先查询节点是否存在并验证租户
	var parentNode models.MetaNode
	if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, nodeID).First(&parentNode).Error; err != nil {
		return nil, fmt.Errorf("parent node not found: %w", err)
	}

	if err := s.db.Where("tenant_id = ? AND parent_node_id = ?", tenantID, nodeID).
		Order("name").
		Find(&nodes).Error; err != nil {
		return nil, fmt.Errorf("failed to query child nodes: %w", err)
	}

	result := make([]models.MetaNodeLite, len(nodes))
	for i, node := range nodes {
		result[i] = convertToMetaNodeLite(node)
	}

	return result, nil
}

// GetNodeItems 获取节点下的项目
func (s *ScanServiceNew) GetNodeItems(tenantID, nodeID uint) ([]models.MetaItemLite, error) {
	var items []models.MetaItem

	// 先查询节点是否存在并验证租户
	var parentNode models.MetaNode
	if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, nodeID).First(&parentNode).Error; err != nil {
		return nil, fmt.Errorf("parent node not found: %w", err)
	}

	if err := s.db.Where("tenant_id = ? AND node_id = ?", tenantID, nodeID).
		Order("name").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("failed to query node items: %w", err)
	}

	result := make([]models.MetaItemLite, len(items))
	for i, item := range items {
		result[i] = convertToMetaItemLite(item)
	}

	return result, nil
}

// GetTableSpatialMetadata 获取表的空间元数据（MVT专用）
func (s *ScanServiceNew) GetTableSpatialMetadata(tenantID, engineID uint, schema, table string) (*models.SpatialMetadataResponse, error) {
	var item models.MetaItem

	// 查询表元数据
	err := s.db.Where("tenant_id = ? AND res_id = ? AND item_type = ? AND name = ?", tenantID, engineID, "table", table).
		Where("attributes->>'schema' = ?", schema).
		First(&item).Error

	if err != nil {
		return nil, fmt.Errorf("table metadata not found: %w", err)
	}

	// 提取空间元数据
	spatialMeta := &models.SpatialMetadataResponse{
		Fields: []models.FieldInfo{},
	}

	// 提取 spatial_metadata
	if spatialData, ok := item.Attributes["spatial_metadata"].(map[string]interface{}); ok {
		if geomCol, ok := spatialData["geometry_column"].(string); ok {
			spatialMeta.GeometryColumn = geomCol
		}
		if srid, ok := spatialData["srid"].(float64); ok {
			spatialMeta.SRID = int(srid)
		}
		if extentSRID, ok := spatialData["extent_srid"].(float64); ok {
			spatialMeta.ExtentSRID = int(extentSRID)
		}
		if extent, ok := spatialData["extent"].([]interface{}); ok {
			spatialMeta.Extent = make([]float64, len(extent))
			for i, v := range extent {
				if f, ok := v.(float64); ok {
					spatialMeta.Extent[i] = f
				}
			}
		}
	}

	// 提取 table_metadata
	if tableMeta, ok := item.Attributes["table_metadata"].(map[string]interface{}); ok {
		if pk, ok := tableMeta["primary_key"].(string); ok {
			spatialMeta.PrimaryKey = pk
		}
	}

	// 提取字段信息
	if fields, ok := item.Attributes["fields"].([]interface{}); ok {
		for _, f := range fields {
			if fieldMap, ok := f.(map[string]interface{}); ok {
				fieldInfo := models.FieldInfo{
					Name:         toString(fieldMap["name"]),
					DataType:     toString(fieldMap["data_type"]),
					IsPrimaryKey: toBool(fieldMap["is_primary_key"]),
				}
				spatialMeta.Fields = append(spatialMeta.Fields, fieldInfo)
			}
		}
	}

	return spatialMeta, nil
}

// GetMetaNodeByID 获取单个节点详情
func (s *ScanServiceNew) GetMetaNodeByID(tenantID, nodeID uint) (*models.MetaNodeLite, error) {
	var node models.MetaNode

	if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, nodeID).First(&node).Error; err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	result := convertToMetaNodeLite(node)
	return &result, nil
}

// convertToMetaNodeLite 转换为 MetaNodeLite
func convertToMetaNodeLite(node models.MetaNode) models.MetaNodeLite {
	var lastScanAt *string
	if node.LastScanAt != nil {
		formatted := node.LastScanAt.Format(time.RFC3339)
		lastScanAt = &formatted
	}

	return models.MetaNodeLite{
		ID:             node.ID,
		TenantID:       node.TenantID,
		ResID:          node.EngineID,
		ParentNodeID:   node.ParentNodeID,
		NodeType:       node.NodeType,
		Name:           node.Name,
		FullName:       node.FullName,
		Depth:          node.Depth,
		Path:           node.Path,
		ScanStatus:     node.ScanStatus,
		LastScanAt:     lastScanAt,
		ItemCount:      node.ItemCount,
		TotalSizeBytes: node.TotalSizeBytes,
		Attributes:     node.Attributes,
	}
}

// convertToMetaItemLite 转换为 MetaItemLite
func convertToMetaItemLite(item models.MetaItem) models.MetaItemLite {
	var lastModifiedAt *string
	if item.LastModifiedAt != nil {
		formatted := item.LastModifiedAt.Format(time.RFC3339)
		lastModifiedAt = &formatted
	}

	return models.MetaItemLite{
		ID:              item.ID,
		TenantID:        item.TenantID,
		ResID:           item.EngineID,
		NodeID:          item.NodeID,
		ItemType:        item.ItemType,
		Name:            item.Name,
		FullName:        item.FullName,
		RowCount:        item.RowCount,
		SizeBytes:       item.SizeBytes,
		ObjectSizeBytes: item.ObjectSizeBytes,
		LastModifiedAt:  lastModifiedAt,
		Attributes:      item.Attributes,
	}
}

