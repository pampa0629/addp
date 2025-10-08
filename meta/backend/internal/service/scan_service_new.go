package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
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
	// 连接到资源数据库
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
		totalSchemas := 0
		totalNodes := 0
		for _, bucket := range buckets {
			var existingSchema models.MetadataSchema
			err := s.db.Where("resource_id = ? AND tenant_id = ? AND schema_name = ?",
				resource.ID, tenantID, bucket).First(&existingSchema).Error

			if err == gorm.ErrRecordNotFound {
				schemas, nodes, _, err := s.scanObjectStorageResource(resource, tenantID, []string{bucket}, nil)
				if err != nil {
					log.Printf("Failed to scan bucket %s: %v", bucket, err)
					continue
				}
				totalSchemas += schemas
				totalNodes += nodes
			}
		}
		return totalSchemas, totalNodes, 0, nil
	}

	// 列出所有Schema
	schemasInfo, err := scan.ListSchemas()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to list schemas: %w", err)
	}

	totalSchemas := 0
	totalTables := 0
	totalFields := 0

	// 检查每个Schema是否已扫描
	for _, schemaInfo := range schemasInfo {
		var existingSchema models.MetadataSchema
		err := s.db.Where("resource_id = ? AND tenant_id = ? AND schema_name = ?",
			resource.ID, tenantID, schemaInfo.Name).First(&existingSchema).Error

		if err == gorm.ErrRecordNotFound {
			// 未扫描，执行扫描
			schemas, tables, fields, err := s.scanSingleSchema(scan, resource.ID, tenantID, schemaInfo.Name)
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
		schemas, tables, err := s.scanObjectStorageWithScanner(resource, tenantID, schemaNames, nil, objectScanner)
		return schemas, tables, 0, err
	}

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
		schemas, tables, fields, err := s.scanSingleSchema(scan, resource.ID, tenantID, schemaName)
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

	schemas, tables, err := s.scanObjectStorageWithScanner(resource, tenantID, objectPaths, fallback, objectScanner)
	if err != nil {
		return 0, 0, 0, err
	}
	return schemas, tables, 0, nil
}

func (s *ScanServiceNew) scanObjectStorageWithScanner(resource *commonModels.Resource, tenantID uint, objectPaths, fallback []string, objectScanner scanner.ObjectStorageScanner) (int, int, error) {
	paths := prepareObjectPaths(objectPaths, fallback, objectScanner)
	if len(paths) == 0 {
		return 0, 0, nil
	}

	uniqueBuckets := map[string]struct{}{}
	totalNodes := 0

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
		uniqueBuckets[bucket] = struct{}{}
		count, err := s.persistObjectMetadataForPath(resource.ID, tenantID, bucket, path, metas)
		if err != nil {
			log.Printf("Failed to persist metadata for path %s: %v", path, err)
			continue
		}
		totalNodes += count
	}

	return len(uniqueBuckets), totalNodes, nil
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

func (s *ScanServiceNew) scanSingleSchema(scan scanner.Scanner, resourceID, tenantID uint, schemaName string) (int, int, int, error) {
	now := time.Now()

	// 创建或更新Schema记录
	var metaSchema models.MetadataSchema
	err := s.db.Where("resource_id = ? AND tenant_id = ? AND schema_name = ?",
		resourceID, tenantID, schemaName).First(&metaSchema).Error

	if err == gorm.ErrRecordNotFound {
		metaSchema = models.MetadataSchema{
			ResourceID: resourceID,
			TenantID:   tenantID,
			SchemaName: schemaName,
			ScanStatus: "扫描中",
			ScanDepth:  "deep",
		}
		if err := s.db.Create(&metaSchema).Error; err != nil {
			return 0, 0, 0, err
		}
	} else {
		metaSchema.ScanStatus = "扫描中"
		s.db.Save(&metaSchema)
	}

	// 扫描表
	tables, err := scan.ScanTables(schemaName)
	if err != nil {
		metaSchema.ScanStatus = "未扫描"
		metaSchema.ErrorMessage = err.Error()
		s.db.Save(&metaSchema)
		return 0, 0, 0, err
	}

	totalTables := 0
	totalFields := 0
	totalSize := int64(0)

	// 保存表和字段
	for _, tableInfo := range tables {
		// 创建或更新表记录
		var metaTable models.MetadataTable
		err := s.db.Where("schema_id = ? AND tenant_id = ? AND table_name = ?",
			metaSchema.ID, tenantID, tableInfo.Name).First(&metaTable).Error

		if err == gorm.ErrRecordNotFound {
			metaTable = models.MetadataTable{
				SchemaID:     metaSchema.ID,
				TenantID:     tenantID,
				Name:         tableInfo.Name,
				TableType:    tableInfo.Type,
				TableComment: tableInfo.Comment,
				RowCount:     tableInfo.RowCount,
				SizeBytes:    tableInfo.SizeBytes,
				LastScanAt:   &now,
			}
			if err := s.db.Create(&metaTable).Error; err != nil {
				log.Printf("Failed to create table %s: %v", tableInfo.Name, err)
				continue
			}
		} else {
			metaTable.Name = tableInfo.Name
			metaTable.TableType = tableInfo.Type
			metaTable.TableComment = tableInfo.Comment
			metaTable.RowCount = tableInfo.RowCount
			metaTable.SizeBytes = tableInfo.SizeBytes
			metaTable.LastScanAt = &now
			s.db.Save(&metaTable)

			// 删除旧字段
			s.db.Where("table_id = ?", metaTable.ID).Delete(&models.MetadataField{})
		}

		totalTables++
		totalSize += tableInfo.SizeBytes

		// 扫描字段
		fields, err := scan.ScanFields(schemaName, tableInfo.Name)
		if err != nil {
			log.Printf("Failed to scan fields for table %s: %v", tableInfo.Name, err)
			continue
		}

		// 保存字段
		for _, fieldInfo := range fields {
			metaField := &models.MetadataField{
				TableID:          metaTable.ID,
				TenantID:         tenantID,
				FieldName:        fieldInfo.Name,
				DataType:         fieldInfo.DataType,
				ColumnType:       fieldInfo.ColumnType,
				IsNullable:       fieldInfo.IsNullable,
				DefaultValue:     fieldInfo.DefaultValue,
				ColumnComment:    fieldInfo.Comment,
				IsPrimaryKey:     fieldInfo.IsPrimaryKey,
				IsUniqueKey:      fieldInfo.IsUniqueKey,
				OrdinalPosition:  fieldInfo.OrdinalPosition,
				CharacterSet:     fieldInfo.CharacterSet,
				Collation:        fieldInfo.Collation,
				NumericPrecision: fieldInfo.NumericPrecision,
				NumericScale:     fieldInfo.NumericScale,
			}
			if err := s.db.Create(metaField).Error; err != nil {
				log.Printf("Failed to create field %s: %v", fieldInfo.Name, err)
				continue
			}
			totalFields++
		}
	}

	// 更新Schema状态
	metaSchema.ScanStatus = "已扫描"
	metaSchema.LastScanAt = &now
	metaSchema.TableCount = totalTables
	metaSchema.TotalSize = totalSize
	metaSchema.ErrorMessage = ""
	s.db.Save(&metaSchema)

	return 1, totalTables, totalFields, nil
}

func (s *ScanServiceNew) persistObjectMetadataForPath(resourceID, tenantID uint, bucket, path string, metas []scanner.ObjectMetadata) (int, error) {
	schema, err := s.ensureObjectSchema(resourceID, tenantID, bucket)
	if err != nil {
		return 0, err
	}

	relativePrefix := ""
	trimmed := sanitizeObjectPath(path)
	if trimmed != "" {
		parts := strings.SplitN(trimmed, "/", 2)
		if len(parts) > 1 {
			relativePrefix = sanitizeObjectPath(parts[1])
		}
	}

	if err := s.clearObjectMetadata(schema.ID, tenantID, relativePrefix); err != nil {
		return 0, err
	}

	now := time.Now()
	inserted := 0

	for _, meta := range metas {
		tableName := meta.RelativePath
		if meta.NodeType == "bucket" && tableName == "" {
			tableName = "__bucket__"
		}
		if tableName == "" {
			continue
		}

		var metaTable models.MetadataTable
		err := s.db.Where("schema_id = ? AND tenant_id = ? AND table_name = ?",
			schema.ID, tenantID, tableName).First(&metaTable).Error

		if err == gorm.ErrRecordNotFound {
			metaTable = models.MetadataTable{
				SchemaID: schema.ID,
				TenantID: tenantID,
				Name:     tableName,
			}
		} else if err != nil {
			return inserted, err
		}

		metaTable.TableType = meta.NodeType
		if meta.NodeType == "object" {
			metaTable.TableComment = meta.FileType
		} else {
			metaTable.TableComment = ""
		}
		metaTable.RowCount = meta.ObjectCount
		metaTable.SizeBytes = meta.SizeBytes
		metaTable.LastScanAt = &now

		if err == gorm.ErrRecordNotFound {
			if err := s.db.Create(&metaTable).Error; err != nil {
				return inserted, err
			}
		} else {
			if err := s.db.Save(&metaTable).Error; err != nil {
				return inserted, err
			}
		}

		inserted++
	}

	if err := s.updateObjectSchemaStats(schema.ID, tenantID); err != nil {
		return inserted, err
	}

	return inserted, nil
}

func (s *ScanServiceNew) ensureObjectSchema(resourceID, tenantID uint, bucket string) (*models.MetadataSchema, error) {
	var schema models.MetadataSchema
	err := s.db.Where("resource_id = ? AND tenant_id = ? AND schema_name = ?",
		resourceID, tenantID, bucket).First(&schema).Error
	if err == gorm.ErrRecordNotFound {
		schema = models.MetadataSchema{
			ResourceID: resourceID,
			TenantID:   tenantID,
			SchemaName: bucket,
			ScanStatus: "扫描中",
			ScanDepth:  "deep",
		}
		if err := s.db.Create(&schema).Error; err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return &schema, nil
}

func (s *ScanServiceNew) clearObjectMetadata(schemaID, tenantID uint, prefix string) error {
	if prefix == "" {
		return s.db.Where("schema_id = ? AND tenant_id = ?", schemaID, tenantID).
			Delete(&models.MetadataTable{}).Error
	}
	clean := strings.TrimSuffix(prefix, "/")
	like := clean + "%"
	return s.db.Where("schema_id = ? AND tenant_id = ? AND (table_name = ? OR table_name LIKE ?)",
		schemaID, tenantID, clean, like).Delete(&models.MetadataTable{}).Error
}

func (s *ScanServiceNew) updateObjectSchemaStats(schemaID, tenantID uint) error {
	var schema models.MetadataSchema
	if err := s.db.First(&schema, schemaID).Error; err != nil {
		return err
	}

	var tableCount int64
	if err := s.db.Model(&models.MetadataTable{}).
		Where("schema_id = ? AND tenant_id = ?", schemaID, tenantID).
		Count(&tableCount).Error; err != nil {
		return err
	}

	var size sql.NullInt64
	if err := s.db.Model(&models.MetadataTable{}).
		Where("schema_id = ? AND tenant_id = ? AND table_type = ?", schemaID, tenantID, "object").
		Select("COALESCE(SUM(size_bytes),0)").
		Scan(&size).Error; err != nil {
		return err
	}

	now := time.Now()
	schema.TableCount = int(tableCount)
	schema.TotalSize = size.Int64
	schema.LastScanAt = &now
	schema.ScanStatus = "已扫描"
	schema.ErrorMessage = ""

	return s.db.Save(&schema).Error
}

// GetSchemasByResource 获取资源的所有Schema
func (s *ScanServiceNew) GetSchemasByResource(resourceID, tenantID uint) ([]*models.SchemaWithStatus, error) {
	var schemas []models.MetadataSchema
	err := s.db.Where("resource_id = ? AND tenant_id = ?", resourceID, tenantID).
		Order("schema_name").Find(&schemas).Error

	if err != nil {
		return nil, err
	}

	var result []*models.SchemaWithStatus
	for _, schema := range schemas {
		item := &models.SchemaWithStatus{
			ID:              schema.ID,
			SchemaName:      schema.SchemaName,
			ScanStatus:      schema.ScanStatus,
			TableCount:      schema.TableCount,
			TotalSizeBytes:  schema.TotalSize,
			AutoScanEnabled: schema.AutoScanEnabled,
			AutoScanCron:    schema.AutoScanCron,
		}
		if schema.LastScanAt != nil {
			item.LastScanAt = schema.LastScanAt.Format("2006-01-02 15:04:05")
		}
		if schema.NextScanAt != nil {
			item.NextScanAt = schema.NextScanAt.Format("2006-01-02 15:04:05")
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
