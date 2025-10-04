package service

import (
	"fmt"
	"log"
	"time"

	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanner"
	"github.com/addp/meta/pkg/utils"
	"gorm.io/gorm"
)

// ScanService Level 2 深度扫描服务
type ScanService struct {
	db           *gorm.DB
	systemClient *utils.SystemClient
}

func NewScanService(db *gorm.DB, systemClient *utils.SystemClient) *ScanService {
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
	connStr, err := utils.BuildConnectionString(resource)
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
	connStr, err := utils.BuildConnectionString(resource)
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

// updateScanLogFailed 更新扫描日志为失败状态
func (s *ScanService) updateScanLogFailed(syncLog *models.MetadataSyncLog, errorMsg string) {
	now := time.Now()
	syncLog.Status = "failed"
	syncLog.CompletedAt = &now
	syncLog.DurationSeconds = int(now.Sub(*syncLog.StartedAt).Seconds())
	syncLog.ErrorMessage = errorMsg
	s.db.Save(syncLog)
}
