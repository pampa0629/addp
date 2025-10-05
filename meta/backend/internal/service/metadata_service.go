package service

import (
	"fmt"

	"github.com/addp/common/client"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// MetadataService 元数据查询服务
type MetadataService struct {
	db           *gorm.DB
	systemClient *client.SystemClient
}

func NewMetadataService(db *gorm.DB, systemClient *client.SystemClient) *MetadataService {
	return &MetadataService{
		db:           db,
		systemClient: systemClient,
	}
}

// ListDatasources 获取数据源列表（包含资源信息）
func (s *MetadataService) ListDatasources(tenantID uint) ([]models.DatasourceWithResource, error) {
	var datasources []models.MetadataDatasource
	query := s.db.Where("tenant_id = ?", tenantID).Order("created_at DESC")
	if err := query.Find(&datasources).Error; err != nil {
		return nil, err
	}

	// 构建带资源信息的响应
	result := make([]models.DatasourceWithResource, 0, len(datasources))
	for _, ds := range datasources {
		dto := models.DatasourceWithResource{
			ID:           ds.ID,
			ResourceID:   ds.ResourceID,
			TenantID:     ds.TenantID,
			SyncStatus:   ds.SyncStatus,
			LastSyncAt:   ds.LastSyncAt,
			SyncLevel:    ds.SyncLevel,
			ErrorMessage: ds.ErrorMessage,
			CreatedAt:    ds.CreatedAt,
			UpdatedAt:    ds.UpdatedAt,
		}

		// 从 System 获取资源信息
		if resource, err := s.systemClient.GetResource(ds.ResourceID); err == nil {
			dto.DatasourceName = resource.ResourceName
			dto.DatasourceType = resource.ResourceType
		}

		result = append(result, dto)
	}

	return result, nil
}

// GetDatasource 获取数据源详情（包含资源信息）
func (s *MetadataService) GetDatasource(id, tenantID uint) (*models.DatasourceWithResource, error) {
	var datasource models.MetadataDatasource
	if err := s.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&datasource).Error; err != nil {
		return nil, err
	}

	dto := &models.DatasourceWithResource{
		ID:           datasource.ID,
		ResourceID:   datasource.ResourceID,
		TenantID:     datasource.TenantID,
		SyncStatus:   datasource.SyncStatus,
		LastSyncAt:   datasource.LastSyncAt,
		SyncLevel:    datasource.SyncLevel,
		ErrorMessage: datasource.ErrorMessage,
		CreatedAt:    datasource.CreatedAt,
		UpdatedAt:    datasource.UpdatedAt,
	}

	// 从 System 获取资源信息
	if resource, err := s.systemClient.GetResource(datasource.ResourceID); err == nil {
		dto.DatasourceName = resource.ResourceName
		dto.DatasourceType = resource.ResourceType
	}

	return dto, nil
}

// ListDatabases 获取数据库列表
func (s *MetadataService) ListDatabases(datasourceID, tenantID uint) ([]models.MetadataDatabase, error) {
	var databases []models.MetadataDatabase
	query := s.db.Where("datasource_id = ? AND tenant_id = ?", datasourceID, tenantID).Order("database_name")
	if err := query.Find(&databases).Error; err != nil {
		return nil, err
	}
	return databases, nil
}

// GetDatabase 获取数据库详情
func (s *MetadataService) GetDatabase(id, tenantID uint) (*models.MetadataDatabase, error) {
	var database models.MetadataDatabase
	if err := s.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&database).Error; err != nil {
		return nil, err
	}
	return &database, nil
}

// ListTables 获取表列表
func (s *MetadataService) ListTables(databaseID, tenantID uint) ([]models.MetadataTable, error) {
	var tables []models.MetadataTable
	query := s.db.Where("database_id = ? AND tenant_id = ?", databaseID, tenantID).Order("table_name")
	if err := query.Find(&tables).Error; err != nil {
		return nil, err
	}
	return tables, nil
}

// GetTable 获取表详情
func (s *MetadataService) GetTable(id, tenantID uint) (*models.MetadataTable, error) {
	var table models.MetadataTable
	if err := s.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&table).Error; err != nil {
		return nil, err
	}
	return &table, nil
}

// ListFields 获取字段列表
func (s *MetadataService) ListFields(tableID, tenantID uint) ([]models.MetadataField, error) {
	var fields []models.MetadataField
	query := s.db.Where("table_id = ? AND tenant_id = ?", tableID, tenantID).Order("field_position")
	if err := query.Find(&fields).Error; err != nil {
		return nil, err
	}
	return fields, nil
}

// GetField 获取字段详情
func (s *MetadataService) GetField(id, tenantID uint) (*models.MetadataField, error) {
	var field models.MetadataField
	if err := s.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&field).Error; err != nil {
		return nil, err
	}
	return &field, nil
}

// ListSyncLogs 获取同步日志列表
func (s *MetadataService) ListSyncLogs(datasourceID, tenantID uint, limit int) ([]models.MetadataSyncLog, error) {
	var logs []models.MetadataSyncLog
	query := s.db.Where("tenant_id = ?", tenantID)
	if datasourceID > 0 {
		query = query.Where("datasource_id = ?", datasourceID)
	}
	query = query.Order("started_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

// SearchTables 搜索表
func (s *MetadataService) SearchTables(tenantID uint, keyword string) ([]models.MetadataTable, error) {
	var tables []models.MetadataTable
	query := s.db.Where("tenant_id = ?", tenantID)
	if keyword != "" {
		query = query.Where("table_name LIKE ? OR table_comment LIKE ?",
			"%"+keyword+"%", "%"+keyword+"%")
	}
	if err := query.Limit(100).Find(&tables).Error; err != nil {
		return nil, err
	}
	return tables, nil
}

// SearchFields 搜索字段
func (s *MetadataService) SearchFields(tenantID uint, keyword string) ([]models.MetadataField, error) {
	var fields []models.MetadataField
	query := s.db.Where("tenant_id = ?", tenantID)
	if keyword != "" {
		query = query.Where("field_name LIKE ? OR field_comment LIKE ?",
			"%"+keyword+"%", "%"+keyword+"%")
	}
	if err := query.Limit(100).Find(&fields).Error; err != nil {
		return nil, err
	}
	return fields, nil
}

// GetMetadataStats 获取元数据统计信息
func (s *MetadataService) GetMetadataStats(tenantID uint) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// 数据源数量
	var datasourceCount int64
	if err := s.db.Model(&models.MetadataDatasource{}).Where("tenant_id = ?", tenantID).Count(&datasourceCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count datasources: %w", err)
	}
	stats["datasource_count"] = datasourceCount

	// 数据库数量
	var databaseCount int64
	if err := s.db.Model(&models.MetadataDatabase{}).Where("tenant_id = ?", tenantID).Count(&databaseCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count databases: %w", err)
	}
	stats["database_count"] = databaseCount

	// 表数量
	var tableCount int64
	if err := s.db.Model(&models.MetadataTable{}).Where("tenant_id = ?", tenantID).Count(&tableCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count tables: %w", err)
	}
	stats["table_count"] = tableCount

	// 字段数量
	var fieldCount int64
	if err := s.db.Model(&models.MetadataField{}).Where("tenant_id = ?", tenantID).Count(&fieldCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count fields: %w", err)
	}
	stats["field_count"] = fieldCount

	// 已扫描数据库数量
	var scannedDatabaseCount int64
	if err := s.db.Model(&models.MetadataDatabase{}).Where("tenant_id = ? AND is_scanned = ?", tenantID, true).Count(&scannedDatabaseCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count scanned databases: %w", err)
	}
	stats["scanned_database_count"] = scannedDatabaseCount

	return stats, nil
}
