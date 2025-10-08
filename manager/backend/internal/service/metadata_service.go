package service

import (
	"fmt"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type MetadataService struct {
	metadataRepo *repository.MetadataRepository
	resourceRepo *repository.ResourceRepository
	systemClient *commonClient.SystemClient
}

func NewMetadataService(metadataRepo *repository.MetadataRepository, resourceRepo *repository.ResourceRepository, systemClient *commonClient.SystemClient) *MetadataService {
	return &MetadataService{
		metadataRepo: metadataRepo,
		resourceRepo: resourceRepo,
		systemClient: systemClient,
	}
}

// ScanResource 扫描资源的元数据（轻量级）
func (s *MetadataService) ScanResource(resourceID uint) (*models.MetadataScanResult, error) {
	// 获取资源信息（优先从 System 服务获取解密后的连接信息）
	resource, err := s.getResource(resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource: %w", err)
	}

	var result models.MetadataScanResult

	switch resource.ResourceType {
	case "postgresql":
		// 扫描数据库表
		tables, err := s.metadataRepo.ScanDatabaseTables(resourceID, resource.ConnectionInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to scan database tables: %w", err)
		}

		// 保存或更新表元数据
		if err := s.metadataRepo.SaveOrUpdateTables(tables); err != nil {
			return nil, fmt.Errorf("failed to save table metadata: %w", err)
		}

		// 获取更新后的列表
		allTables, err := s.metadataRepo.GetManagedTables(resourceID, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get tables: %w", err)
		}

		result.TotalItems = len(allTables)

		managedCount := 0
		items := make([]interface{}, len(allTables))
		for i, table := range allTables {
			if table.IsManaged {
				managedCount++
			}
			items[i] = table
		}

		result.ManagedItems = managedCount
		result.UnmanagedItems = result.TotalItems - managedCount
		result.Items = items

	case "minio":
		// TODO: 对象存储扫描逻辑
		return nil, fmt.Errorf("minio scanning not yet implemented")

	default:
		return nil, fmt.Errorf("unsupported resource type: %s", resource.ResourceType)
	}

	return &result, nil
}

// GetTables 获取资源的表列表
func (s *MetadataService) GetTables(resourceID uint, isManaged *bool) ([]models.ManagedTable, error) {
	return s.metadataRepo.GetManagedTables(resourceID, isManaged)
}

// ManageTable 纳管表（提取详细元数据）
func (s *MetadataService) ManageTable(tableID uint) error {
	// 获取表信息  - 直接通过GetByID获取
	table, err := s.metadataRepo.GetManagedTableByID(tableID)
	if err != nil {
		return fmt.Errorf("table not found: %w", err)
	}

	// 获取资源连接信息
	resource, err := s.getResource(table.ResourceID)
	if err != nil {
		return fmt.Errorf("failed to get resource: %w", err)
	}

	// 标记为已纳管并提取详细元数据
	return s.metadataRepo.MarkTableAsManaged(tableID, resource.ConnectionInfo)
}

// UnmanageTable 取消纳管表
func (s *MetadataService) UnmanageTable(tableID uint) error {
	return s.metadataRepo.UnmarkTableAsManaged(tableID)
}

// GetResourceTree 获取资源- Schema-表树
func (s *MetadataService) GetResourceTree() ([]models.DataExplorerResource, error) {
	resources, err := s.resourceRepo.ListAllActive()
	if err != nil {
		return nil, err
	}

	schemas, tables, err := s.metadataRepo.ListScannedSchemasAndTables()
	if err != nil {
		return nil, err
	}

	// map schemaID -> tables
	tableMap := make(map[uint][]models.DataExplorerTable)
	for _, table := range tables {
		if table.LastScan == nil {
			continue
		}
		tableMap[table.SchemaID] = append(tableMap[table.SchemaID], models.DataExplorerTable{
			ID:       table.ID,
			Name:     table.TableName,
			FullName: table.TableName,
		})
	}

	// map resourceID -> schemas
	schemaMap := make(map[uint][]models.DataExplorerSchema)
	for _, schema := range schemas {
		tablesForSchema := tableMap[schema.ID]
		if len(tablesForSchema) == 0 {
			continue
		}
		for i := range tablesForSchema {
			tablesForSchema[i].FullName = fmt.Sprintf("%s.%s", schema.SchemaName, tablesForSchema[i].Name)
		}
		tableMap[schema.ID] = tablesForSchema
		schemaMap[schema.ResourceID] = append(schemaMap[schema.ResourceID], models.DataExplorerSchema{
			Name:   schema.SchemaName,
			Tables: tablesForSchema,
		})
	}

	var result []models.DataExplorerResource
	for _, res := range resources {
		schemasForResource := schemaMap[res.ID]
		if len(schemasForResource) == 0 {
			continue
		}

		result = append(result, models.DataExplorerResource{
			ID:           res.ID,
			Name:         res.Name,
			ResourceType: res.ResourceType,
			Schemas:      schemasForResource,
		})
	}

	return result, nil
}

// PreviewTable 获取表数据预览
func (s *MetadataService) PreviewTable(resourceID uint, schemaName, tableName string, page, pageSize int) (*models.TablePreview, error) {
	resource, err := s.getResource(resourceID)
	if err != nil {
		return nil, err
	}

	const maxRows = 50
	columns, rows, total, err := s.metadataRepo.QueryTablePreview(resource, schemaName, tableName, page, pageSize, maxRows)
	if err != nil {
		return nil, err
	}

	return &models.TablePreview{
		Columns:  columns,
		Rows:     rows,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// getResource 优先通过 System 服务获取解密后的资源信息，失败时回退到本地数据库
func (s *MetadataService) getResource(resourceID uint) (*models.Resource, error) {
	if s.systemClient != nil {
		if sysResource, err := s.systemClient.GetResource(resourceID); err == nil {
			return convertResource(sysResource), nil
		}
	}
	return s.resourceRepo.GetByID(resourceID)
}

func convertResource(src *commonModels.Resource) *models.Resource {
	if src == nil {
		return nil
	}

	var tenantIDPtr *uint
	if src.TenantID != 0 {
		tenantID := src.TenantID
		tenantIDPtr = &tenantID
	}

	connInfo := make(models.ConnectionInfo, len(src.ConnectionInfo))
	for k, v := range src.ConnectionInfo {
		connInfo[k] = v
	}

	return &models.Resource{
		ID:             src.ID,
		Name:           src.Name,
		ResourceType:   src.ResourceType,
		ConnectionInfo: connInfo,
		Description:    src.Description,
		CreatedBy:      src.CreatedBy,
		TenantID:       tenantIDPtr,
		IsActive:       src.IsActive,
	}
}
