package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	pathpkg "path"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanner"
	_ "github.com/addp/meta/internal/scanner/extractors" // 自动注册提取器
	"gorm.io/gorm"
)

// ScanServiceNew 新的统一扫描服务
type ScanServiceNew struct {
	db              *gorm.DB
	resourceService *ResourceService
	log             *slog.Logger
}

// ScanProgressReporter 用于在长时间扫描任务中更新进度
type ScanProgressReporter interface {
	SetTotal(total int)
	Advance(label string, completed, total int, meta map[string]interface{})
	Message(message string)
}

func NewScanServiceNew(db *gorm.DB, resourceService *ResourceService) *ScanServiceNew {
	if resourceService == nil {
		resourceService = NewResourceService(db, "", "")
	}

	return &ScanServiceNew{
		db:              db,
		resourceService: resourceService,
		log:             logger.With("component", "scan_service"),
	}
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

func (s *ScanServiceNew) upsertNode(tenantID, resourceID uint, parent *models.MetaNode, nodeType, name, fullName string, attrs models.JSONMap) (*models.MetaNode, error) {
	var parentID *uint
	depth := 1
	if parent != nil {
		parentID = &parent.ID
		depth = parent.Depth + 1
	}

	query := s.db.Where("res_id = ? AND tenant_id = ? AND node_type = ? AND name = ?", resourceID, tenantID, nodeType, name)
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
			ResID:        resourceID,
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

	updates := map[string]interface{}{}
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
		if err := s.db.Model(&node).Updates(updates).Error; err != nil {
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
	tenantID, resourceID uint,
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
			fingerprint = models.GenerateObjectFingerprint(resourceID, bucket, path)
		} else if schema, ok := attrs["schema_name"].(string); ok {
			// 关系数据库：使用 schema.table
			fingerprint = models.GenerateTableFingerprint(resourceID, schema, name)
		} else {
			// 其他类型：使用 fullName
			fingerprint = models.GenerateItemFingerprint(resourceID, fullName)
		}
	} else {
		// 无attributes，使用fullName作为标识
		fingerprint = models.GenerateItemFingerprint(resourceID, fullName)
	}

	var item models.MetaItem
	// 使用fingerprint查找记录（唯一索引）
	err := s.db.Where("fingerprint = ?", fingerprint).First(&item).Error

	if err == gorm.ErrRecordNotFound {
		// 创建新记录
		item = models.MetaItem{
			TenantID:          tenantID,
			ResID:             resourceID,
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

	// 更新已有记录
	updates := map[string]interface{}{
		"node_id":             node.ID, // 允许node_id变化（数据移动）
		"full_name":           fullName,
		"meta_schema_version": schemaVersion,
		"attributes":          attrs,
		"row_count":           rowCount,
		"size_bytes":          sizeBytes,
		"object_size_bytes":   objectSize,
		"last_modified_at":    lastModified,
		"updated_at":          time.Now(),
	}

	if err := s.db.Model(&item).Updates(updates).Error; err != nil {
		return nil, err
	}

	// 更新内存中的对象
	item.NodeID = node.ID
	item.FullName = fullName
	item.MetaSchemaVersion = schemaVersion
	item.Attributes = attrs
	item.RowCount = rowCount
	item.SizeBytes = sizeBytes
	item.ObjectSizeBytes = objectSize
	item.LastModifiedAt = lastModified

	return &item, nil
}

func buildFieldAttributes(fields []scanner.FieldInfo) []map[string]interface{} {
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
	resources, err := s.resourceService.GetResourcesByTenant(tenantID)
	if err != nil {
		s.updateScanLogFailed(scanLog, err.Error())
		return nil, err
	}

	totalSchemas := 0
	totalTables := 0
	totalFields := 0
	scannedResourceIDs := []uint{}

	// 对每个资源进行扫描
	for _, resource := range resources {
		schemas, tables, fields, err := s.scanResource(resource, tenantID, scanLog.ID)
		if err != nil {
			s.log.Warn("资源扫描失败",
				"resource_id", resource.ID,
				"resource_name", resource.Name,
				"tenant_id", tenantID,
				"scan_log_id", scanLog.ID,
				"error", err,
			)
			continue
		}

		totalSchemas += schemas
		totalTables += tables
		totalFields += fields
		scannedResourceIDs = append(scannedResourceIDs, resource.ID)
	}

	// 更新扫描日志
	completedAt := time.Now()
	scanLog.ResourceID = 0 // 多资源扫描，不关联特定资源
	scanLog.Status = "success"
	scanLog.SchemasScanned = totalSchemas
	scanLog.TablesScanned = totalTables
	scanLog.FieldsScanned = totalFields
	scanLog.CompletedAt = &completedAt
	scanLog.DurationMs = completedAt.Sub(startTime).Milliseconds()
	s.db.Save(scanLog)

	return &models.ScanResponse{
		Status:         "success",
		Message:        fmt.Sprintf("Successfully scanned %d resources", len(scannedResourceIDs)),
		SchemasScanned: totalSchemas,
		TablesScanned:  totalTables,
		FieldsScanned:  totalFields,
		DurationMs:     scanLog.DurationMs,
		StartedAt:      startTime.Format("2006-01-02 15:04:05"),
	}, nil
}

// ScanResource 扫描指定资源
func (s *ScanServiceNew) ScanResource(resourceID, tenantID uint, schemaNames, objectPaths []string, token string) (*models.ScanResponse, error) {
	return s.scanResourceInternal(resourceID, tenantID, schemaNames, objectPaths, token, nil)
}

// ScanResourceWithProgress 扫描指定资源，并通过 reporter 汇报进度
func (s *ScanServiceNew) ScanResourceWithProgress(resourceID, tenantID uint, schemaNames, objectPaths []string, token string, reporter ScanProgressReporter) (*models.ScanResponse, error) {
	return s.scanResourceInternal(resourceID, tenantID, schemaNames, objectPaths, token, reporter)
}

func (s *ScanServiceNew) scanResourceInternal(resourceID, tenantID uint, schemaNames, objectPaths []string, token string, reporter ScanProgressReporter) (*models.ScanResponse, error) {
	startTime := time.Now()

	// 获取资源
	if reporter != nil {
		reporter.Message("正在加载资源连接信息")
	}
	resource, err := s.resourceService.GetResourceByID(resourceID, tenantID, token)
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

	// 创建扫描日志
	schemasJSON, _ := json.Marshal(schemaNames)
	scanLog := &models.ScanLog{
		ResourceID:    resourceID,
		TenantID:      tenantID,
		ScanType:      "manual",
		ScanDepth:     "deep",
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
	)
	if len(schemaNames) > 0 {
		startFields = append(startFields, "target_schemas", schemaNames)
	}
	if len(objectPaths) > 0 {
		startFields = append(startFields, "target_paths", objectPaths)
	}
	s.log.Info("开始扫描资源", startFields...)

	resourceType := strings.ToLower(resource.ResourceType)

	schemas, tables, fields := 0, 0, 0

	if isObjectStorageType(resourceType) {
		schemas, tables, fields, err = s.scanObjectStorageResourceWithReporter(resource, tenantID, objectPaths, schemaNames, reporter)
	} else {
		schemas, tables, fields, err = s.scanResourceSchemasWithReporter(resource, tenantID, schemaNames, scanLog.ID, reporter)
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

	if directRun != nil {
		resultSummary := models.JSONMap{
			"schemas_scanned": schemas,
			"tables_scanned":  tables,
			"fields_scanned":  fields,
			"duration_ms":     scanLog.DurationMs,
			"started_at":      scanLog.StartedAt,
		}
		s.completeImmediateRun(directRun, resultSummary, completedAt)
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

func (s *ScanServiceNew) createImmediateRunRecord(resource *commonModels.Resource, tenantID uint, schemaNames, objectPaths []string, startTime time.Time) (*models.ScanTaskRun, error) {
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
		ResourceID:      resource.ID,
		StorageType:     normalizeStorageType(resource.ResourceType),
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
	}
}

// scanResource 扫描单个资源的所有未扫描Schema
func (s *ScanServiceNew) scanResource(resource *commonModels.Resource, tenantID uint, scanLogID uint) (int, int, int, error) {
	resourceID := resource.ID

	startFields := append(connectionLogFields(resource),
		"scan_log_id", scanLogID,
		"mode", "auto",
	)
	s.log.Info("开始扫描资源", startFields...)

	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to build connection string: %w", err)
	}

	scan, err := scanner.NewScanner(resource.ResourceType, connStr)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	if objectScanner, ok := scan.(scanner.ObjectStorageScanner); ok && isObjectStorageType(strings.ToLower(resource.ResourceType)) {
		buckets := objectScanner.AllowedBuckets()
		if len(buckets) == 0 {
			s.log.Info("对象存储资源未配置可扫描桶，跳过扫描", cloneLogFields(startFields, "allowed_bucket_count", 0)...)
			return 0, 0, 0, nil
		}
		sort.Strings(buckets)

		totalBuckets := 0
		totalObjects := 0

		s.log.Info("对象存储资源扫描开始", cloneLogFields(startFields, "allowed_bucket_count", len(buckets), "allowed_buckets", buckets)...)

		for _, bucket := range buckets {
			var node models.MetaNode
			err := s.db.Where("tenant_id = ? AND res_id = ? AND node_type = ? AND name = ?",
				tenantID, resourceID, "bucket", bucket).First(&node).Error

			if err == gorm.ErrRecordNotFound {
				schemas, objects, err := s.scanObjectStoragePaths(tenantID, resourceID, objectScanner, []string{bucket}, nil)
				if err != nil {
					s.log.Warn("对象存储桶扫描失败",
						"resource_id", resourceID,
						"tenant_id", tenantID,
						"bucket", bucket,
						"error", err,
					)
					continue
				}
				totalBuckets += schemas
				totalObjects += objects
			} else if err != nil {
				s.log.Warn("查询对象存储节点失败",
					"resource_id", resourceID,
					"tenant_id", tenantID,
					"bucket", bucket,
					"error", err,
				)
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

	for _, schemaInfo := range schemasInfo {
		var node models.MetaNode
		err := s.db.Where("tenant_id = ? AND res_id = ? AND node_type = ? AND name = ?",
			tenantID, resourceID, "schema", schemaInfo.Name).First(&node).Error
		if err == gorm.ErrRecordNotFound {
			schemas, tables, fields, err := s.scanDatabaseSchema(scan, tenantID, resourceID, schemaInfo.Name)
			if err != nil {
				s.log.Warn("Schema 扫描失败",
					"resource_id", resourceID,
					"tenant_id", tenantID,
					"schema", schemaInfo.Name,
					"error", err,
				)
				continue
			}
			totalSchemas += schemas
			totalTables += tables
			totalFields += fields
		} else if err != nil {
			s.log.Warn("查询 Schema 节点失败",
				"resource_id", resourceID,
				"tenant_id", tenantID,
				"schema", schemaInfo.Name,
				"error", err,
			)
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
func (s *ScanServiceNew) scanResourceSchemas(resource *commonModels.Resource, tenantID uint, schemaNames []string, scanLogID uint) (int, int, int, error) {
	return s.scanResourceSchemasWithReporter(resource, tenantID, schemaNames, scanLogID, nil)
}

func (s *ScanServiceNew) scanResourceSchemasWithReporter(resource *commonModels.Resource, tenantID uint, schemaNames []string, scanLogID uint, reporter ScanProgressReporter) (int, int, int, error) {
	resourceID := resource.ID

	startFields := append(connectionLogFields(resource),
		"scan_log_id", scanLogID,
		"mode", "manual",
	)
	if len(schemaNames) > 0 {
		startFields = append(startFields, "target_schemas", schemaNames)
	}
	s.log.Info("开始扫描指定 Schema 列表", startFields...)

	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to build connection string: %w", err)
	}

	scan, err := scanner.NewScanner(resource.ResourceType, connStr)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	// 如果未指定Schema，则扫描所有Schema
	if len(schemaNames) == 0 {
		if reporter != nil {
			reporter.Message("未指定 Schema，正在获取完整列表")
		}
		schemasInfo, err := scan.ListSchemas()
		if err != nil {
			return 0, 0, 0, err
		}
		for _, info := range schemasInfo {
			schemaNames = append(schemaNames, info.Name)
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

		schemas, tables, fields, err := s.scanDatabaseSchema(scan, tenantID, resourceID, schemaName)
		if err != nil {
			s.log.Warn("Schema 扫描失败",
				"resource_id", resourceID,
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
func (s *ScanServiceNew) scanObjectStorageResource(resource *commonModels.Resource, tenantID uint, objectPaths, fallback []string) (int, int, int, error) {
	return s.scanObjectStorageResourceWithReporter(resource, tenantID, objectPaths, fallback, nil)
}

func (s *ScanServiceNew) scanObjectStorageResourceWithReporter(resource *commonModels.Resource, tenantID uint, objectPaths, fallback []string, reporter ScanProgressReporter) (int, int, int, error) {
	resourceID := resource.ID

	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to build connection string: %w", err)
	}

	scan, err := scanner.NewScanner(resource.ResourceType, connStr)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	objectScanner, ok := scan.(scanner.ObjectStorageScanner)
	if !ok {
		return 0, 0, 0, fmt.Errorf("resource %s is not object storage", resource.ResourceType)
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

	buckets, objects, err := s.scanObjectStoragePaths(tenantID, resourceID, objectScanner, paths, reporter)
	if err != nil {
		return 0, 0, 0, err
	}

	return buckets, objects, 0, nil
}

func (s *ScanServiceNew) scanObjectStoragePaths(tenantID, resourceID uint, objectScanner scanner.ObjectStorageScanner, paths []string, reporter ScanProgressReporter) (int, int, error) {
	bucketNodes := make(map[string]*models.MetaNode)
	processedBuckets := make(map[string]bool)
	nodeStats := make(map[uint]*nodeAggregate)
	clearedPaths := make(map[string]map[string]bool)

	// 如果scanner支持SetResourceID，设置resourceID用于元数据提取
	if s3Scanner, ok := objectScanner.(interface{ SetResourceID(uint) }); ok {
		s3Scanner.SetResourceID(resourceID)
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
				"resource_id", resourceID,
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

		bucketNode, ok := bucketNodes[bucketName]
		if !ok {
			attrs := models.JSONMap{"bucket": bucketName}
			bucketNode, err = s.upsertNode(tenantID, resourceID, nil, "bucket", bucketName, bucketName, attrs)
			if err != nil {
				return totalBuckets, totalObjects, err
			}
			bucketNodes[bucketName] = bucketNode
			totalBuckets++
		}

		if _, ok := clearedPaths[bucketName]; !ok {
			clearedPaths[bucketName] = make(map[string]bool)
		}

		if !clearedPaths[bucketName][relativePath] {
			if err := s.clearObjectMetadataUnderPath(tenantID, resourceID, bucketNode, bucketName, relativePath); err != nil {
				return totalBuckets, totalObjects, err
			}
			clearedPaths[bucketName][relativePath] = true
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

		objects, err := s.persistObjectMetas(tenantID, resourceID, bucketNode, metas, nodeStats, fullBucket)
		if err != nil {
			s.log.Error("对象存储元数据持久化失败",
				"resource_id", resourceID,
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

	for _, agg := range nodeStats {
		if err := s.finalizeNodeState(agg.node, "已扫描", agg.itemCount, agg.totalSize, ""); err != nil {
			return totalBuckets, totalObjects, err
		}
	}

	return totalBuckets, totalObjects, nil
}

func (s *ScanServiceNew) persistObjectMetas(tenantID, resourceID uint, bucketNode *models.MetaNode, metas []scanner.ObjectMetadata, stats map[uint]*nodeAggregate, includeBucketAggregate bool) (int, error) {
	objects := 0

	for _, meta := range metas {
		if meta.NodeType == "bucket" {
			if includeBucketAggregate {
				ensureNodeAggregate(stats, bucketNode)
			}
			continue
		}

		parentChain := []*models.MetaNode{bucketNode}
		currentParent := bucketNode

		trimmed := sanitizeObjectPath(meta.RelativePath)
		if trimmed != "" {
			segments := strings.Split(trimmed, "/")
			for idx, segment := range segments {
				isLast := idx == len(segments)-1
				if meta.NodeType == "object" && isLast {
					break
				}
				fullName := composeNodeFullName(segment, currentParent, "/")
				attrs := models.JSONMap{
					"bucket": meta.Bucket,
					"path":   strings.Join(segments[:idx+1], "/"),
				}
				childNode, err := s.upsertNode(tenantID, resourceID, currentParent, "prefix", segment, fullName, attrs)
				if err != nil {
					return objects, err
				}
				currentParent = childNode
				parentChain = append(parentChain, childNode)
				ensureNodeAggregate(stats, childNode)
			}
		} else if includeBucketAggregate {
			ensureNodeAggregate(stats, bucketNode)
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

		// 尝试使用插件提取深度元数据
		enhancedAttrs := s.extractEnhancedMetadata(resourceID, meta, attrs)

		sizeVal := meta.SizeBytes
		objectSizeVal := meta.SizeBytes
		fullName := composeNodeFullName(objectName, currentParent, "/")
		if _, err := s.upsertItem(tenantID, resourceID, currentParent, "object", objectName, fullName, enhancedAttrs, nil, &sizeVal, &objectSizeVal, meta.LastModified, 1); err != nil {
			return objects, err
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

// extractEnhancedMetadata 使用插件提取增强的元数据
func (s *ScanServiceNew) extractEnhancedMetadata(resourceID uint, meta scanner.ObjectMetadata, baseAttrs models.JSONMap) models.JSONMap {
	// 如果S3Scanner已经提取了元数据，直接使用
	if meta.ExtractedMetadata != nil && meta.ExtractedMetadata.CustomAttrs != nil {
		// 将提取的CustomAttrs合并到baseAttrs中
		for key, value := range meta.ExtractedMetadata.CustomAttrs {
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
	extractor := scanner.GetExtractor(contentType)
	if extractor == nil {
		// 没有合适的提取器，返回基础属性
		return baseAttrs
	}

	// 标记有提取器可用（但本次扫描未提取）
	baseAttrs["extractor_available"] = true
	baseAttrs["content_type"] = contentType

	return baseAttrs
}

// GetObjectMetadata 获取指定对象的元数据
// 用于Manager模块预览时查询已扫描的元数据
func (s *ScanServiceNew) GetObjectMetadata(tenantID, resourceID uint, objectKey string) (*models.MetaItem, error) {
	// 解析对象路径
	bucket, relativePath := splitObjectPath(objectKey)
	if bucket == "" {
		return nil, fmt.Errorf("invalid object key: %s", objectKey)
	}

	// 查找bucket节点
	var bucketNode models.MetaNode
	err := s.db.Where("tenant_id = ? AND res_id = ? AND node_type = ? AND name = ?",
		tenantID, resourceID, "bucket", bucket).First(&bucketNode).Error
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
				tenantID, resourceID, currentParent.ID, "prefix", segment).First(&prefixNode).Error
			if err != nil {
				return nil, fmt.Errorf("prefix not found: %s", segment)
			}
			currentParent = &prefixNode
		}

		// 在最终的prefix节点下查找对象
		err = s.db.Where("tenant_id = ? AND res_id = ? AND node_id = ? AND item_type = ? AND name = ?",
			tenantID, resourceID, currentParent.ID, "object", objectName).First(&item).Error
	} else {
		// 直接在bucket下查找对象
		err = s.db.Where("tenant_id = ? AND res_id = ? AND node_id = ? AND item_type = ? AND name = ?",
			tenantID, resourceID, bucketNode.ID, "object", objectName).First(&item).Error
	}

	if err != nil {
		return nil, fmt.Errorf("object metadata not found: %w", err)
	}

	return &item, nil
}

// ExtractObjectMetadataOnDemand 按需提取对象的深度元数据
// 当Manager预览时发现元数据未提取，可以调用此方法触发实时提取
func (s *ScanServiceNew) ExtractObjectMetadataOnDemand(tenantID, resourceID uint, objectKey string, token string, objectReader io.Reader) (*scanner.Metadata, error) {
	// 检测内容类型
	contentType := mime.TypeByExtension(pathpkg.Ext(objectKey))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 获取元数据提取器
	extractor := scanner.GetExtractor(contentType)
	if extractor == nil {
		return nil, fmt.Errorf("no extractor available for content type: %s", contentType)
	}

	// 获取对象的基本信息
	item, err := s.GetObjectMetadata(tenantID, resourceID, objectKey)
	if err != nil {
		// 如果元数据不存在，使用默认值
		s.log.Warn("对象元数据不存在，使用默认值", "object_key", objectKey, "error", err)
	}

	// 构建提取输入
	input := scanner.ExtractInput{
		ResourceID:  resourceID,
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

func prepareObjectPaths(paths, fallback []string, scanner scanner.ObjectStorageScanner) []string {
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

func (s *ScanServiceNew) clearObjectMetadataUnderPath(tenantID, resourceID uint, bucketNode *models.MetaNode, bucketName, relativePath string) error {
	clean := sanitizeObjectPath(relativePath)
	if clean == "" {
		if err := s.hardDeleteDescendantNodes(bucketNode); err != nil {
			return err
		}
		return s.hardDeleteItemsByNode(bucketNode.ID)
	}

	var targetNodes []models.MetaNode
	if err := s.db.
		Where("tenant_id = ? AND res_id = ?", tenantID, resourceID).
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
		Where("tenant_id = ? AND res_id = ?", tenantID, resourceID).
		Where("(attributes ->> 'bucket') = ?", bucketName).
		Where("(attributes ->> 'relative_path') = ? OR (attributes ->> 'relative_path') LIKE ?", clean, clean+"/%").
		Delete(&models.MetaItem{}).Error; err != nil {
		return fmt.Errorf("failed to delete object items by relative path: %w", err)
	}

	return nil
}

func (s *ScanServiceNew) scanDatabaseSchema(scan scanner.Scanner, tenantID, resourceID uint, schemaName string) (int, int, int, error) {
	schemaNode, err := s.upsertNode(tenantID, resourceID, nil, "schema", schemaName, "", nil)
	if err != nil {
		return 0, 0, 0, err
	}

	if err := s.resetNodeState(schemaNode, "扫描中"); err != nil {
		return 0, 0, 0, err
	}

	if err := s.hardDeleteItemsByNode(schemaNode.ID); err != nil {
		return 0, 0, 0, err
	}

	s.log.Info("开始扫描 Schema",
		"tenant_id", tenantID,
		"resource_id", resourceID,
		"schema", schemaName,
	)

	tables, err := scan.ScanTables(schemaName)
	if err != nil {
		s.finalizeNodeState(schemaNode, "未扫描", 0, 0, err.Error())
		return 0, 0, 0, err
	}

	totalTables := 0
	totalFields := 0
	var totalSize int64

	for _, tableInfo := range tables {
		fields, err := scan.ScanFields(schemaName, tableInfo.Name)
		if err != nil {
			s.log.Warn("字段扫描失败",
				"resource_id", resourceID,
				"tenant_id", tenantID,
				"schema", schemaName,
				"table", tableInfo.Name,
				"error", err,
			)
			continue
		}

		rowCount := tableInfo.RowCount
		sizeBytes := tableInfo.SizeBytes

		attrs := models.JSONMap{
			"schema":        schemaName,
			"table_type":    tableInfo.Type,
			"table_comment": tableInfo.Comment,
			"fields":        buildFieldAttributes(fields),
		}

		fullName := composeNodeFullName(tableInfo.Name, schemaNode, ".")
		if _, err := s.upsertItem(tenantID, resourceID, schemaNode, "table", tableInfo.Name, fullName, attrs, &rowCount, &sizeBytes, nil, nil, 1); err != nil {
			s.log.Error("表元数据持久化失败",
				"resource_id", resourceID,
				"tenant_id", tenantID,
				"schema", schemaName,
				"table", tableInfo.Name,
				"error", err,
			)
			continue
		}

		totalTables++
		totalFields += len(fields)
		totalSize += tableInfo.SizeBytes
	}

	s.log.Info("Schema 扫描完成",
		"tenant_id", tenantID,
		"resource_id", resourceID,
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
func (s *ScanServiceNew) GetSchemasByResource(resourceID, tenantID uint) ([]*models.SchemaWithStatus, error) {
	var nodes []models.MetaNode
	if err := s.db.Where("tenant_id = ? AND res_id = ? AND parent_node_id IS NULL", tenantID, resourceID).
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
func (s *ScanServiceNew) ListAvailableSchemas(resourceID, tenantID uint, token string) ([]*models.SchemaInfo, error) {
	resource, err := s.resourceService.GetResourceByID(resourceID, tenantID, token)
	if err != nil {
		return nil, err
	}

	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	scan, err := scanner.NewScanner(resource.ResourceType, connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	schemasInfo, err := scan.ListSchemas()
	if err != nil {
		return nil, err
	}

	var result []*models.SchemaInfo
	for _, info := range schemasInfo {
		result = append(result, &models.SchemaInfo{
			Name: info.Name,
		})
	}

	return result, nil
}

func (s *ScanServiceNew) ListObjectStorageNodes(resourceID, tenantID uint, path, token string) ([]*models.ObjectNode, error) {
	resource, err := s.resourceService.GetResourceByID(resourceID, tenantID, token)
	if err != nil {
		return nil, err
	}

	if !isObjectStorageType(strings.ToLower(resource.ResourceType)) {
		return nil, fmt.Errorf("resource %s is not object storage", resource.ResourceType)
	}

	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	scan, err := scanner.NewScanner(resource.ResourceType, connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	objectScanner, ok := scan.(scanner.ObjectStorageScanner)
	if !ok {
		return nil, fmt.Errorf("resource %s is not object storage", resource.ResourceType)
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
