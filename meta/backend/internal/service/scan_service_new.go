package service

import (
	"encoding/json"
	"fmt"
	"log"
	pathpkg "path"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanner"
	"gorm.io/gorm"
)

// ScanServiceNew 新的统一扫描服务
type ScanServiceNew struct {
	db              *gorm.DB
	systemClient    *client.SystemClient
	resourceService *ResourceService
}

func NewScanServiceNew(db *gorm.DB, systemClient *client.SystemClient, resourceService *ResourceService) *ScanServiceNew {
	if resourceService == nil {
		resourceService = NewResourceService(db, "", "")
	}

	return &ScanServiceNew{
		db:              db,
		systemClient:    systemClient,
		resourceService: resourceService,
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

func sanitizeConnectionInfo(info commonModels.ConnectionInfo) models.JSONMap {
	sanitized := models.JSONMap{}
	if info == nil {
		return sanitized
	}
	for key, value := range info {
		lowerKey := strings.ToLower(key)
		if strings.Contains(lowerKey, "password") ||
			strings.Contains(lowerKey, "secret") ||
			strings.Contains(lowerKey, "token") ||
			strings.Contains(lowerKey, "key") {
			continue
		}
		sanitized[key] = value
	}
	return sanitized
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

func (s *ScanServiceNew) ensureMetaResourceRecord(resource *commonModels.Resource, tenantID uint) (*models.MetaResource, error) {
	var metaRes models.MetaResource
	err := s.db.Where("tenant_id = ? AND resource_id = ?", tenantID, resource.ID).First(&metaRes).Error
	if err == gorm.ErrRecordNotFound {
		metaRes = models.MetaResource{
			TenantID:     tenantID,
			ResourceID:   resource.ID,
			ResourceType: resource.ResourceType,
			Name:         resource.Name,
			Engine:       strings.ToLower(resource.ResourceType),
			Config:       sanitizeConnectionInfo(resource.ConnectionInfo),
			Status:       "active",
			Source:       "system",
		}
		if err := s.db.Create(&metaRes).Error; err != nil {
			return nil, err
		}
		return &metaRes, nil
	} else if err != nil {
		return nil, err
	}

	updates := map[string]interface{}{}
	if metaRes.Name != resource.Name {
		updates["name"] = resource.Name
		metaRes.Name = resource.Name
	}
	if metaRes.ResourceType != resource.ResourceType {
		updates["resource_type"] = resource.ResourceType
		metaRes.ResourceType = resource.ResourceType
	}
	engine := strings.ToLower(resource.ResourceType)
	if metaRes.Engine != engine {
		updates["engine"] = engine
		metaRes.Engine = engine
	}

	sanitized := sanitizeConnectionInfo(resource.ConnectionInfo)
	if len(sanitized) > 0 && !reflect.DeepEqual(metaRes.Config, sanitized) {
		updates["config"] = sanitized
		metaRes.Config = sanitized
	}

	if len(updates) > 0 {
		updates["updated_at"] = time.Now()
		if err := s.db.Model(&metaRes).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	return &metaRes, nil
}

func (s *ScanServiceNew) upsertNode(metaRes *models.MetaResource, parent *models.MetaNode, nodeType, name, fullName string, attrs models.JSONMap) (*models.MetaNode, error) {
	var parentID *uint
	depth := 1
	if parent != nil {
		parentID = &parent.ID
		depth = parent.Depth + 1
	}

	query := s.db.Where("res_id = ? AND tenant_id = ? AND node_type = ? AND name = ?", metaRes.ID, metaRes.TenantID, nodeType, name)
	if parentID == nil {
		query = query.Where("parent_node_id IS NULL")
	} else {
		query = query.Where("parent_node_id = ?", *parentID)
	}

	var node models.MetaNode
	err := query.First(&node).Error
	if err == gorm.ErrRecordNotFound {
		node = models.MetaNode{
			TenantID:     metaRes.TenantID,
			ResID:        metaRes.ID,
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
	metaRes *models.MetaResource,
	node *models.MetaNode,
	itemType, name, fullName string,
	attrs models.JSONMap,
	rowCount, sizeBytes, objectSize *int64,
	lastModified *time.Time,
	schemaVersion int,
) (*models.MetaItem, error) {
	var item models.MetaItem
	err := s.db.Where("tenant_id = ? AND res_id = ? AND node_id = ? AND item_type = ? AND name = ?",
		metaRes.TenantID, metaRes.ID, node.ID, itemType, name).First(&item).Error

	if err == gorm.ErrRecordNotFound {
		item = models.MetaItem{
			TenantID:          metaRes.TenantID,
			ResID:             metaRes.ID,
			NodeID:            node.ID,
			ItemType:          itemType,
			Name:              name,
			FullName:          fullName,
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

	updates := map[string]interface{}{
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
			log.Printf("Failed to scan resource %s: %v", resource.Name, err)
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
	startTime := time.Now()

	// 获取资源
	resource, err := s.resourceService.GetResourceByID(resourceID, tenantID, token)
	if err != nil {
		return nil, err
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

	resourceType := strings.ToLower(resource.ResourceType)

	schemas, tables, fields := 0, 0, 0

	if isObjectStorageType(resourceType) {
		schemas, tables, fields, err = s.scanObjectStorageResource(resource, tenantID, objectPaths, schemaNames)
	} else {
		schemas, tables, fields, err = s.scanResourceSchemas(resource, tenantID, schemaNames, scanLog.ID)
	}

	if err != nil {
		s.updateScanLogFailed(scanLog, err.Error())
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

// scanResource 扫描单个资源的所有未扫描Schema
func (s *ScanServiceNew) scanResource(resource *commonModels.Resource, tenantID uint, scanLogID uint) (int, int, int, error) {
	metaRes, err := s.ensureMetaResourceRecord(resource, tenantID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to ensure meta resource: %w", err)
	}

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
			return 0, 0, 0, nil
		}
		sort.Strings(buckets)

		totalBuckets := 0
		totalObjects := 0

		for _, bucket := range buckets {
			var node models.MetaNode
			err := s.db.Where("tenant_id = ? AND res_id = ? AND node_type = ? AND name = ?",
				metaRes.TenantID, metaRes.ID, "bucket", bucket).First(&node).Error

			if err == gorm.ErrRecordNotFound {
				schemas, objects, err := s.scanObjectStoragePaths(metaRes, objectScanner, []string{bucket})
				if err != nil {
					log.Printf("Failed to scan bucket %s: %v", bucket, err)
					continue
				}
				totalBuckets += schemas
				totalObjects += objects
			}
		}
		return totalBuckets, totalObjects, 0, nil
	}

	schemasInfo, err := scan.ListSchemas()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to list schemas: %w", err)
	}

	totalSchemas := 0
	totalTables := 0
	totalFields := 0

	for _, schemaInfo := range schemasInfo {
		var node models.MetaNode
		err := s.db.Where("tenant_id = ? AND res_id = ? AND node_type = ? AND name = ?",
			metaRes.TenantID, metaRes.ID, "schema", schemaInfo.Name).First(&node).Error
		if err == gorm.ErrRecordNotFound {
			schemas, tables, fields, err := s.scanDatabaseSchema(scan, metaRes, schemaInfo.Name)
			if err != nil {
				log.Printf("Failed to scan schema %s: %v", schemaInfo.Name, err)
				continue
			}
			totalSchemas += schemas
			totalTables += tables
			totalFields += fields
		}
	}

	return totalSchemas, totalTables, totalFields, nil
}

// scanResourceSchemas 扫描资源的指定Schema列表
func (s *ScanServiceNew) scanResourceSchemas(resource *commonModels.Resource, tenantID uint, schemaNames []string, scanLogID uint) (int, int, int, error) {
	metaRes, err := s.ensureMetaResourceRecord(resource, tenantID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to ensure meta resource: %w", err)
	}

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

	for _, schemaName := range schemaNames {
		schemas, tables, fields, err := s.scanDatabaseSchema(scan, metaRes, schemaName)
		if err != nil {
			log.Printf("Failed to scan schema %s: %v", schemaName, err)
			continue
		}
		totalSchemas += schemas
		totalTables += tables
		totalFields += fields
	}

	return totalSchemas, totalTables, totalFields, nil
}

// scanSingleSchema 扫描单个Schema（表+字段）
func (s *ScanServiceNew) scanObjectStorageResource(resource *commonModels.Resource, tenantID uint, objectPaths, fallback []string) (int, int, int, error) {
	metaRes, err := s.ensureMetaResourceRecord(resource, tenantID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to ensure meta resource: %w", err)
	}

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
		return 0, 0, 0, nil
	}

	buckets, objects, err := s.scanObjectStoragePaths(metaRes, objectScanner, paths)
	if err != nil {
		return 0, 0, 0, err
	}

	return buckets, objects, 0, nil
}

func (s *ScanServiceNew) scanObjectStoragePaths(metaRes *models.MetaResource, objectScanner scanner.ObjectStorageScanner, paths []string) (int, int, error) {
	bucketNodes := make(map[string]*models.MetaNode)
	processedBuckets := make(map[string]bool)
	nodeStats := make(map[uint]*nodeAggregate)

	totalBuckets := 0
	totalObjects := 0

	for _, path := range paths {
		metas, err := objectScanner.ScanPath(path)
		if err != nil {
			log.Printf("Failed to scan path %s: %v", path, err)
			continue
		}
		if len(metas) == 0 {
			continue
		}

		bucket := metas[0].Bucket
		if bucket == "" {
			continue
		}

		bucketNode, ok := bucketNodes[bucket]
		if !ok {
			attrs := models.JSONMap{"bucket": bucket}
			bucketNode, err = s.upsertNode(metaRes, nil, "bucket", bucket, bucket, attrs)
			if err != nil {
				return totalBuckets, totalObjects, err
			}
			bucketNodes[bucket] = bucketNode
		}

		if !processedBuckets[bucket] {
			if err := s.resetNodeState(bucketNode, "扫描中"); err != nil {
				return totalBuckets, totalObjects, err
			}
			if err := s.hardDeleteDescendantNodes(bucketNode); err != nil {
				return totalBuckets, totalObjects, err
			}
			if err := s.hardDeleteItemsByNode(bucketNode.ID); err != nil {
				return totalBuckets, totalObjects, err
			}
			processedBuckets[bucket] = true
			totalBuckets++
		}

		objects, err := s.persistObjectMetas(metaRes, bucketNode, metas, nodeStats)
		if err != nil {
			log.Printf("Failed to persist object metadata for bucket %s: %v", bucket, err)
			continue
		}
		totalObjects += objects
	}

	for _, agg := range nodeStats {
		if err := s.finalizeNodeState(agg.node, "已扫描", agg.itemCount, agg.totalSize, ""); err != nil {
			return totalBuckets, totalObjects, err
		}
	}

	for _, bucketNode := range bucketNodes {
		if _, ok := nodeStats[bucketNode.ID]; !ok {
			if err := s.finalizeNodeState(bucketNode, "已扫描", 0, 0, ""); err != nil {
				return totalBuckets, totalObjects, err
			}
		}
	}

	return totalBuckets, totalObjects, nil
}

func (s *ScanServiceNew) persistObjectMetas(metaRes *models.MetaResource, bucketNode *models.MetaNode, metas []scanner.ObjectMetadata, stats map[uint]*nodeAggregate) (int, error) {
	objects := 0

	for _, meta := range metas {
		if meta.NodeType == "bucket" {
			ensureNodeAggregate(stats, bucketNode)
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
				childNode, err := s.upsertNode(metaRes, currentParent, "prefix", segment, fullName, attrs)
				if err != nil {
					return objects, err
				}
				currentParent = childNode
				parentChain = append(parentChain, childNode)
				ensureNodeAggregate(stats, childNode)
			}
		} else {
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

		sizeVal := meta.SizeBytes
		objectSizeVal := meta.SizeBytes
		fullName := composeNodeFullName(objectName, currentParent, "/")
		if _, err := s.upsertItem(metaRes, currentParent, "object", objectName, fullName, attrs, nil, &sizeVal, &objectSizeVal, meta.LastModified, 1); err != nil {
			return objects, err
		}

		objects++
		for _, node := range parentChain {
			agg := ensureNodeAggregate(stats, node)
			agg.itemCount++
			agg.totalSize += meta.SizeBytes
		}
	}

	ensureNodeAggregate(stats, bucketNode)
	return objects, nil
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

func (s *ScanServiceNew) scanDatabaseSchema(scan scanner.Scanner, metaRes *models.MetaResource, schemaName string) (int, int, int, error) {
	schemaNode, err := s.upsertNode(metaRes, nil, "schema", schemaName, "", nil)
	if err != nil {
		return 0, 0, 0, err
	}

	if err := s.resetNodeState(schemaNode, "扫描中"); err != nil {
		return 0, 0, 0, err
	}

	if err := s.hardDeleteItemsByNode(schemaNode.ID); err != nil {
		return 0, 0, 0, err
	}

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
			log.Printf("Failed to scan fields for table %s: %v", tableInfo.Name, err)
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
		if _, err := s.upsertItem(metaRes, schemaNode, "table", tableInfo.Name, fullName, attrs, &rowCount, &sizeBytes, nil, nil, 1); err != nil {
			log.Printf("Failed to persist table %s: %v", tableInfo.Name, err)
			continue
		}

		totalTables++
		totalFields += len(fields)
		totalSize += tableInfo.SizeBytes
	}

	if err := s.finalizeNodeState(schemaNode, "已扫描", totalTables, totalSize, ""); err != nil {
		return 0, totalTables, totalFields, err
	}

	return 1, totalTables, totalFields, nil
}

// GetSchemasByResource 获取资源的所有Schema
func (s *ScanServiceNew) GetSchemasByResource(resourceID, tenantID uint) ([]*models.SchemaWithStatus, error) {
	var metaRes models.MetaResource
	if err := s.db.Where("tenant_id = ? AND resource_id = ?", tenantID, resourceID).First(&metaRes).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return []*models.SchemaWithStatus{}, nil
		}
		return nil, err
	}

	var nodes []models.MetaNode
	if err := s.db.Where("tenant_id = ? AND res_id = ? AND parent_node_id IS NULL", tenantID, metaRes.ID).
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
