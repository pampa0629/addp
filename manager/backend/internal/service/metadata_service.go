package service

import (
	"fmt"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type MetadataService struct {
	metadataRepo *repository.MetadataRepository
	resourceRepo *repository.ResourceRepository
}

func NewMetadataService(metadataRepo *repository.MetadataRepository, resourceRepo *repository.ResourceRepository) *MetadataService {
	return &MetadataService{
		metadataRepo: metadataRepo,
		resourceRepo: resourceRepo,
	}
}

// ScanResource 扫描资源的元数据（轻量级）
func (s *MetadataService) ScanResource(resourceID uint) (*models.MetadataScanResult, error) {
	// 获取资源信息
	resource, err := s.resourceRepo.GetByID(resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource: %w", err)
	}

	resourceType, ok := resource.ConnectionInfo["resource_type"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid resource_type in resource")
	}

	var result models.MetadataScanResult

	switch resourceType {
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
		return nil, fmt.Errorf("unsupported resource type: %s", resourceType)
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
	resource, err := s.resourceRepo.GetByID(table.ResourceID)
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
