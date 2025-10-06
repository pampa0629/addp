package service

import (
	"fmt"
	"log"
	"time"

	"github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanner"
	"gorm.io/gorm"
)

// ScanService Level 2 深度扫描服务
type ScanService struct {
	db           *gorm.DB
	systemClient *client.SystemClient
}

func NewScanService(db *gorm.DB, systemClient *client.SystemClient) *ScanService {
	return &ScanService{
		db:           db,
		systemClient: systemClient,
	}
}

// DeepScanDatabase 深度扫描数据库 (Level 2: 表 + 字段)
func (s *ScanService) DeepScanDatabase(databaseID, tenantID uint) error {
	// 获取数据库元数据
	var metaDB models.MetadataDatabase
	if err := s.db.First(&metaDB, databaseID).Error; err != nil {
		return fmt.Errorf("database not found: %w", err)
	}

	// 检查租户权限
	if metaDB.TenantID != tenantID {
		return fmt.Errorf("permission denied: database belongs to different tenant")
	}

	// 获取数据源
	var datasource models.MetadataDatasource
	if err := s.db.First(&datasource, metaDB.DatasourceID).Error; err != nil {
		return fmt.Errorf("datasource not found: %w", err)
	}

	// 创建扫描日志
	syncLog := &models.MetadataSyncLog{
		DatasourceID:   datasource.ID,
		TenantID:       tenantID,
		SyncType:       "deep",
		SyncLevel:      "field",
		TargetDatabase: metaDB.DatabaseName,
		Status:         "running",
		StartedAt:      ptrTime(time.Now()),
	}
	if err := s.db.Create(syncLog).Error; err != nil {
		return fmt.Errorf("failed to create sync log: %w", err)
	}

	// 获取资源连接信息
	resource, err := s.systemClient.GetResource(datasource.ResourceID)
	if err != nil {
		s.updateScanLogFailed(syncLog, err.Error())
		return fmt.Errorf("failed to get resource: %w", err)
	}

	// 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		s.updateScanLogFailed(syncLog, err.Error())
		return fmt.Errorf("failed to build connection string: %w", err)
	}

	// 创建扫描器
	scan, err := scanner.NewScanner(resource.ResourceType, connStr)
	if err != nil {
		s.updateScanLogFailed(syncLog, err.Error())
		return fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	// 扫描表列表
	tables, err := scan.ScanTables(metaDB.DatabaseName)
	if err != nil {
		s.updateScanLogFailed(syncLog, err.Error())
		return fmt.Errorf("failed to scan tables: %w", err)
	}

	tablesScanned := 0
	fieldsScanned := 0

	// 保存表和字段元数据
	for _, table := range tables {
		// 保存表元数据
		metaTable := &models.MetadataTable{
			DatabaseID:     metaDB.ID,
			TenantID:       tenantID,
			Name:           table.Name,
			TableType:      table.Type,
			TableSchema:    table.Schema,
			Engine:         table.Engine,
			RowCount:       table.RowCount,
			DataSizeBytes:  table.DataSize,
			IndexSizeBytes: table.IndexSize,
			TableComment:   table.Comment,
			IsScanned:      true,
			LastScanAt:     ptrTime(time.Now()),
		}

		// 检查是否已存在
		var existing models.MetadataTable
		result := s.db.Where("database_id = ? AND table_name = ?", metaDB.ID, table.Name).First(&existing)

		var tableID uint
		if result.Error == gorm.ErrRecordNotFound {
			// 新建
			if err := s.db.Create(metaTable).Error; err != nil {
				log.Printf("Failed to create table metadata: %v", err)
				continue
			}
			tableID = metaTable.ID
		} else {
			// 更新
			if err := s.db.Model(&existing).Updates(metaTable).Error; err != nil {
				log.Printf("Failed to update table metadata: %v", err)
				continue
			}
			tableID = existing.ID
		}

		tablesScanned++

		// 扫描字段列表
		fields, err := scan.ScanFields(metaDB.DatabaseName, table.Name)
		if err != nil {
			log.Printf("Failed to scan fields for table %s: %v", table.Name, err)
			continue
		}

		// 删除旧字段记录
		s.db.Where("table_id = ?", tableID).Delete(&models.MetadataField{})

		// 保存字段元数据
		for _, field := range fields {
			metaField := &models.MetadataField{
				TableID:       tableID,
				TenantID:      tenantID,
				Name:          field.Name,
				FieldPosition: field.Position,
				DataType:      field.DataType,
				ColumnType:    field.ColumnType,
				IsNullable:    field.IsNullable,
				ColumnKey:     field.ColumnKey,
				ColumnDefault: field.DefaultValue,
				Extra:         field.Extra,
				FieldComment:  field.Comment,
			}

			if err := s.db.Create(metaField).Error; err != nil {
				log.Printf("Failed to create field metadata: %v", err)
			} else {
				fieldsScanned++
			}
		}
	}

	// 更新数据库元数据
	metaDB.IsScanned = true
	metaDB.LastScanAt = ptrTime(time.Now())
	s.db.Save(&metaDB)

	// 更新扫描日志
	now := time.Now()
	syncLog.Status = "success"
	syncLog.CompletedAt = &now
	syncLog.DurationSeconds = int(now.Sub(*syncLog.StartedAt).Seconds())
	syncLog.TablesScanned = tablesScanned
	syncLog.FieldsScanned = fieldsScanned
	s.db.Save(syncLog)

	log.Printf("Successfully scanned database %s, found %d tables and %d fields", metaDB.DatabaseName, tablesScanned, fieldsScanned)
	return nil
}

// DeepScanTable 深度扫描单个表 (Level 2: 仅字段)

// DeepScanTable 深度扫描单个表 (Level 2: 仅字段)
func (s *ScanService) DeepScanTable(tableID, tenantID uint) error {
	// 获取表元数据
	var metaTable models.MetadataTable
	if err := s.db.First(&metaTable, tableID).Error; err != nil {
		return fmt.Errorf("table not found: %w", err)
	}

	// 检查租户权限
	if metaTable.TenantID != tenantID {
		return fmt.Errorf("permission denied: table belongs to different tenant")
	}

	// 获取数据库元数据
	var metaDB models.MetadataDatabase
	if err := s.db.First(&metaDB, metaTable.DatabaseID).Error; err != nil {
		return fmt.Errorf("database not found: %w", err)
	}

	// 获取数据源
	var datasource models.MetadataDatasource
	if err := s.db.First(&datasource, metaDB.DatasourceID).Error; err != nil {
		return fmt.Errorf("datasource not found: %w", err)
	}

	// 获取资源连接信息
	resource, err := s.systemClient.GetResource(datasource.ResourceID)
	if err != nil {
		return fmt.Errorf("failed to get resource: %w", err)
	}

	// 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}

	// 创建扫描器
	scan, err := scanner.NewScanner(resource.ResourceType, connStr)
	if err != nil {
		return fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	// 扫描字段列表
	fields, err := scan.ScanFields(metaDB.DatabaseName, metaTable.Name)
	if err != nil {
		return fmt.Errorf("failed to scan fields: %w", err)
	}

	// 删除旧字段记录
	s.db.Where("table_id = ?", tableID).Delete(&models.MetadataField{})

	// 保存字段元数据
	for _, field := range fields {
		metaField := &models.MetadataField{
			TableID:       tableID,
			TenantID:      tenantID,
			Name:          field.Name,
			FieldPosition: field.Position,
			DataType:      field.DataType,
			ColumnType:    field.ColumnType,
			IsNullable:    field.IsNullable,
			ColumnKey:     field.ColumnKey,
			ColumnDefault: field.DefaultValue,
			Extra:         field.Extra,
			FieldComment:  field.Comment,
		}

		if err := s.db.Create(metaField).Error; err != nil {
			log.Printf("Failed to create field metadata: %v", err)
		}
	}

	// 更新表元数据
	metaTable.IsScanned = true
	metaTable.LastScanAt = ptrTime(time.Now())
	s.db.Save(&metaTable)

	log.Printf("Successfully scanned table %s, found %d fields", metaTable.Name, len(fields))
	return nil
}

// GetSchemas 获取数据源的Schema列表
func (s *ScanService) GetSchemas(resourceID, tenantID uint) ([]models.SchemaInfo, error) {
	// 获取资源连接信息
	resource, err := s.systemClient.GetResource(resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource: %w", err)
	}

	// 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 创建扫描器
	scan, err := scanner.NewScanner(resource.ResourceType, connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	// 扫描数据库列表 (schemas)
	databases, err := scan.ScanDatabases()
	if err != nil {
		return nil, fmt.Errorf("failed to scan databases: %w", err)
	}

	// 转换为SchemaInfo格式
	var schemas []models.SchemaInfo
	for _, db := range databases {
		// 扫描每个数据库的表列表
		tables, err := scan.ScanTables(db.Name)
		if err != nil {
			log.Printf("Failed to scan tables for database %s: %v", db.Name, err)
			continue
		}

		tableNames := make([]string, len(tables))
		for i, table := range tables {
			tableNames[i] = table.Name
		}

		schemas = append(schemas, models.SchemaInfo{
			Name:   db.Name,
			Tables: tableNames,
		})
	}

	return schemas, nil
}

// ScanMetadata 扫描元数据(新版本,支持前端UI)
func (s *ScanService) ScanMetadata(req *models.ScanRequest, tenantID uint) (*models.ScanResult, error) {
	startTime := time.Now()

	// 创建扫描日志
	syncLog := &models.MetadataSyncLog{
		DatasourceID: 0, // 暂时为0,后面创建datasource后更新
		TenantID:     tenantID,
		SyncType:     req.Trigger,
		SyncLevel:    req.Depth,
		Status:       "running",
		StartedAt:    &startTime,
	}
	if err := s.db.Create(syncLog).Error; err != nil {
		return nil, fmt.Errorf("failed to create sync log: %w", err)
	}

	// 获取或创建数据源记录
	var datasource models.MetadataDatasource
	result := s.db.Where("resource_id = ? AND tenant_id = ?", req.ResourceID, tenantID).First(&datasource)

	if result.Error == gorm.ErrRecordNotFound {
		// 新建数据源记录
		datasource = models.MetadataDatasource{
			ResourceID:   req.ResourceID,
			TenantID:     tenantID,
			SyncStatus:   "syncing",
			SyncLevel:    req.Depth,
			LastSyncAt:   &startTime,
		}
		if err := s.db.Create(&datasource).Error; err != nil {
			s.updateScanLogFailed(syncLog, err.Error())
			return nil, fmt.Errorf("failed to create datasource: %w", err)
		}
	} else if result.Error != nil {
		s.updateScanLogFailed(syncLog, result.Error.Error())
		return nil, result.Error
	}

	// 更新syncLog的datasource_id
	syncLog.DatasourceID = datasource.ID
	s.db.Save(syncLog)

	// 获取资源连接信息
	resource, err := s.systemClient.GetResource(req.ResourceID)
	if err != nil {
		s.updateScanLogFailed(syncLog, err.Error())
		return nil, fmt.Errorf("failed to get resource: %w", err)
	}

	// 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		s.updateScanLogFailed(syncLog, err.Error())
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 创建扫描器
	scan, err := scanner.NewScanner(resource.ResourceType, connStr)
	if err != nil {
		s.updateScanLogFailed(syncLog, err.Error())
		return nil, fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	schemasScanned := 0
	tablesScanned := 0
	fieldsScanned := 0

	// 扫描每个Schema
	for _, schemaReq := range req.Schemas {
		schemasScanned++

		// 获取或创建数据库记录
		var metaDB models.MetadataDatabase
		dbResult := s.db.Where("datasource_id = ? AND database_name = ?", datasource.ID, schemaReq.Name).First(&metaDB)

		if dbResult.Error == gorm.ErrRecordNotFound {
			metaDB = models.MetadataDatabase{
				DatasourceID: datasource.ID,
				TenantID:     tenantID,
				DatabaseName: schemaReq.Name,
				IsScanned:    false,
			}
			if err := s.db.Create(&metaDB).Error; err != nil {
				log.Printf("Failed to create database metadata: %v", err)
				continue
			}
		}

		// 扫描表
		tables, err := scan.ScanTables(schemaReq.Name)
		if err != nil {
			log.Printf("Failed to scan tables for schema %s: %v", schemaReq.Name, err)
			continue
		}

		// 确定要扫描的表
		tablesToScan := tables
		if schemaReq.ScanMode == "select" && len(schemaReq.SelectedTables) > 0 {
			// 过滤出选中的表
			selectedMap := make(map[string]bool)
			for _, t := range schemaReq.SelectedTables {
				selectedMap[t] = true
			}

			filteredTables := []scanner.TableInfo{}
			for _, table := range tables {
				if selectedMap[table.Name] {
					filteredTables = append(filteredTables, table)
				}
			}
			tablesToScan = filteredTables
		}

		// 扫描表
		for _, table := range tablesToScan {
			tablesScanned++

			// 创建或更新表记录
			metaTable := &models.MetadataTable{
				DatabaseID:     metaDB.ID,
				TenantID:       tenantID,
				Name:           table.Name,
				TableType:      table.Type,
				TableSchema:    table.Schema,
				Engine:         table.Engine,
				RowCount:       table.RowCount,
				DataSizeBytes:  table.DataSize,
				IndexSizeBytes: table.IndexSize,
				TableComment:   table.Comment,
			}

			var existing models.MetadataTable
			tableResult := s.db.Where("database_id = ? AND table_name = ?", metaDB.ID, table.Name).First(&existing)

			var tableID uint
			if tableResult.Error == gorm.ErrRecordNotFound {
				if err := s.db.Create(metaTable).Error; err != nil {
					log.Printf("Failed to create table metadata: %v", err)
					continue
				}
				tableID = metaTable.ID
			} else {
				if err := s.db.Model(&existing).Updates(metaTable).Error; err != nil {
					log.Printf("Failed to update table metadata: %v", err)
					continue
				}
				tableID = existing.ID
			}

			// 如果是深度或完全扫描,扫描字段
			if req.Depth == "deep" || req.Depth == "full" {
				fields, err := scan.ScanFields(schemaReq.Name, table.Name)
				if err != nil {
					log.Printf("Failed to scan fields for table %s: %v", table.Name, err)
					continue
				}

				// 删除旧字段
				s.db.Where("table_id = ?", tableID).Delete(&models.MetadataField{})

				// 保存字段
				for _, field := range fields {
					metaField := &models.MetadataField{
						TableID:       tableID,
						TenantID:      tenantID,
						Name:          field.Name,
						FieldPosition: field.Position,
						DataType:      field.DataType,
						ColumnType:    field.ColumnType,
						IsNullable:    field.IsNullable,
						ColumnKey:     field.ColumnKey,
						ColumnDefault: field.DefaultValue,
						Extra:         field.Extra,
						FieldComment:  field.Comment,
					}

					if err := s.db.Create(metaField).Error; err != nil {
						log.Printf("Failed to create field metadata: %v", err)
					} else {
						fieldsScanned++
					}
				}

				// 更新表状态
				s.db.Model(&models.MetadataTable{}).Where("id = ?", tableID).Updates(map[string]interface{}{
					"is_scanned":   true,
					"last_scan_at": time.Now(),
				})
			}
		}

		// 更新数据库扫描状态
		metaDB.IsScanned = true
		now := time.Now()
		metaDB.LastScanAt = &now
		metaDB.TableCount = tablesScanned
		s.db.Save(&metaDB)
	}

	// 更新数据源状态
	datasource.SyncStatus = "success"
	now := time.Now()
	datasource.LastSyncAt = &now
	s.db.Save(&datasource)

	// 更新扫描日志
	endTime := time.Now()
	syncLog.Status = "success"
	syncLog.CompletedAt = &endTime
	syncLog.DurationSeconds = int(endTime.Sub(startTime).Seconds())
	syncLog.DatabasesScanned = schemasScanned
	syncLog.TablesScanned = tablesScanned
	syncLog.FieldsScanned = fieldsScanned
	s.db.Save(syncLog)

	return &models.ScanResult{
		Status:          "success",
		Message:         "元数据扫描成功",
		SchemasScanned:  schemasScanned,
		TablesScanned:   tablesScanned,
		FieldsScanned:   fieldsScanned,
		DurationSeconds: int(endTime.Sub(startTime).Seconds()),
		StartedAt:       startTime.Format("2006-01-02 15:04:05"),
	}, nil
}

// updateScanLogFailed 更新扫描日志为失败状态
func (s *ScanService) updateScanLogFailed(syncLog *models.MetadataSyncLog, errorMsg string) {
	now := time.Now()
	syncLog.Status = "failed"
	syncLog.CompletedAt = &now
	syncLog.DurationSeconds = int(now.Sub(*syncLog.StartedAt).Seconds())
	syncLog.ErrorMessage = errorMsg
	s.db.Save(syncLog)
}
