package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type MetadataService struct {
	metadataRepo *repository.MetadataRepository
	resourceRepo *repository.ResourceRepository
	systemClient *commonClient.SystemClient
	previews     *PreviewRegistry
	content      *ObjectContentRegistry
}

var ErrResourceAccessDenied = errors.New("resource not accessible for current tenant")

func NewMetadataService(metadataRepo *repository.MetadataRepository, resourceRepo *repository.ResourceRepository, systemClient *commonClient.SystemClient, previewRegistry *PreviewRegistry, contentRegistry *ObjectContentRegistry) *MetadataService {
	pr := previewRegistry
	if pr == nil {
		pr = NewPreviewRegistry()
	}
	cr := contentRegistry
	if cr == nil {
		cr = NewObjectContentRegistry()
	}
	return &MetadataService{
		metadataRepo: metadataRepo,
		resourceRepo: resourceRepo,
		systemClient: systemClient,
		previews:     pr,
		content:      cr,
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

// ListExplorerResources 返回可用于数据探查的存储引擎列表
func (s *MetadataService) ListExplorerResources(tenantID *uint) ([]models.ExplorerResource, error) {
	resources, err := s.listActiveResources(tenantID)
	if err != nil {
		return nil, err
	}

	result := make([]models.ExplorerResource, 0, len(resources))
	for _, res := range resources {
		result = append(result, models.ExplorerResource{
			ID:           res.ID,
			Name:         res.Name,
			ResourceType: res.ResourceType,
			Description:  res.Description,
		})
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// GetResourceTree 获取单个资源的 Schema/目录树
func (s *MetadataService) GetResourceTree(resourceID uint, tenantID *uint) (*models.DataExplorerResource, error) {
	resource, err := s.getResourceForTenant(resourceID, tenantID)
	if err != nil {
		return nil, err
	}

	return s.buildResourceTree(resource)
}

// GetLegacyResourceTree 兼容旧接口的全量资源树
func (s *MetadataService) GetLegacyResourceTree(tenantID *uint) ([]models.DataExplorerResource, error) {
	resources, err := s.listActiveResources(tenantID)
	if err != nil {
		return nil, err
	}

	result := make([]models.DataExplorerResource, 0, len(resources))
	for i := range resources {
		tree, err := s.buildResourceTree(&resources[i])
		if err != nil {
			return nil, err
		}
		if tree != nil {
			result = append(result, *tree)
		}
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (s *MetadataService) buildResourceTree(resource *models.Resource) (*models.DataExplorerResource, error) {
	topNodes, childNodes, items, err := s.metadataRepo.ListScannedNodesAndItems(resource.ID)
	if err != nil {
		if errors.Is(err, repository.ErrMetadataSchemaMissing) {
			logger.L().Warn("数据探查: metadata schema 尚未初始化，返回空树", "resource_id", resource.ID)
			return &models.DataExplorerResource{
				ID:           resource.ID,
				Name:         resource.Name,
				ResourceType: resource.ResourceType,
				Schemas:      []models.DataExplorerSchema{},
			}, nil
		}
		return nil, err
	}

	childrenByParent := make(map[uint][]*models.MetaNodeLite)
	for i := range childNodes {
		node := &childNodes[i]
		if node.ParentNodeID != nil {
			parentID := *node.ParentNodeID
			childrenByParent[parentID] = append(childrenByParent[parentID], node)
		}
	}

	itemsByNode := make(map[uint][]*models.MetaItemLite)
	for i := range items {
		item := &items[i]
		itemsByNode[item.NodeID] = append(itemsByNode[item.NodeID], item)
	}

	resourceType := strings.ToLower(resource.ResourceType)
	var schemasForResource []models.DataExplorerSchema

	if isObjectStorageType(resourceType) {
		for i := range topNodes {
			bucket := &topNodes[i]
			children := buildObjectStorageTree(bucket, childrenByParent, itemsByNode)
			if len(children) == 0 {
				continue
			}
			schemasForResource = append(schemasForResource, models.DataExplorerSchema{
				Name:   bucket.Name,
				Tables: children,
			})
		}
	} else {
		for i := range topNodes {
			schemaNode := &topNodes[i]
			if strings.ToLower(schemaNode.NodeType) != "schema" {
				continue
			}
			itemList := itemsByNode[schemaNode.ID]
			if len(itemList) == 0 {
				continue
			}

			tables := make([]models.DataExplorerTable, 0, len(itemList))
			for _, item := range itemList {
				if strings.ToLower(item.ItemType) != "table" {
					continue
				}
				fullName := item.FullName
				if fullName == "" {
					fullName = fmt.Sprintf("%s.%s", schemaNode.Name, item.Name)
				}
				tables = append(tables, models.DataExplorerTable{
					ID:       item.ID,
					Name:     item.Name,
					FullName: fullName,
					Type:     "table",
				})
			}
			if len(tables) == 0 {
				continue
			}
			sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
			schemasForResource = append(schemasForResource, models.DataExplorerSchema{
				Name:   schemaNode.Name,
				Tables: tables,
			})
		}
	}

	return &models.DataExplorerResource{
		ID:           resource.ID,
		Name:         resource.Name,
		ResourceType: resource.ResourceType,
		Schemas:      schemasForResource,
	}, nil
}

func buildObjectStorageTree(node *models.MetaNodeLite, childNodes map[uint][]*models.MetaNodeLite, items map[uint][]*models.MetaItemLite) []models.DataExplorerTable {
	children := childNodes[node.ID]

	var entries []models.DataExplorerTable

	for _, child := range children {
		entry := models.DataExplorerTable{
			ID:          child.ID,
			Name:        child.Name,
			FullName:    child.FullName,
			Type:        "directory",
			Depth:       child.Depth,
			SizeBytes:   child.TotalSizeBytes,
			ObjectCount: int64(child.ItemCount),
		}
		entry.Children = buildObjectStorageTree(child, childNodes, items)
		entries = append(entries, entry)
	}

	for _, item := range items[node.ID] {
		if strings.ToLower(item.ItemType) != "object" {
			continue
		}
		size := int64(0)
		if item.ObjectSizeBytes != nil {
			size = *item.ObjectSizeBytes
		} else if item.SizeBytes != nil {
			size = *item.SizeBytes
		}
		fullName := item.FullName
		if fullName == "" {
			if v, ok := item.Attributes["relative_path"].(string); ok && v != "" {
				fullName = v
			}
		}
		entry := models.DataExplorerTable{
			ID:        item.ID,
			Name:      item.Name,
			FullName:  fullName,
			Type:      "object",
			SizeBytes: size,
		}
		entries = append(entries, entry)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Type == entries[j].Type {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Type == "directory"
	})

	return entries
}

func (s *MetadataService) listActiveResources(tenantID *uint) ([]models.Resource, error) {
	var tenantFilter uint
	if tenantID != nil {
		tenantFilter = *tenantID
	}

	if s.systemClient != nil {
		sysResources, err := s.systemClient.ListResources("", tenantFilter)
		if err != nil {
			logger.L().Warn("数据探查: System API 获取资源列表失败，回退数据库查询", "error", err)
		} else {
			resources := make([]models.Resource, 0, len(sysResources))
			for i := range sysResources {
				res := sysResources[i]
				if !res.IsActive {
					continue
				}
				converted := convertResource(&res)
				if converted == nil {
					continue
				}
				if !resourceAccessible(converted, tenantID) {
					continue
				}
				resources = append(resources, *converted)
			}
			logger.L().Info("数据探查: System API 获取资源列表成功", "resource_total", len(resources))
			return resources, nil
		}
	}

	resources, err := s.resourceRepo.ListAllActive(tenantID)
	if err != nil {
		logger.L().Error("数据探查: 数据库获取资源列表失败", "error", err)
		return nil, err
	}
	logger.L().Info("数据探查: 数据库获取资源列表成功", "resource_total", len(resources))
	return resources, nil
}

// PreviewTable 获取表数据预览
// 当 tableName 为空时，返回 schema/bucket 的统计信息和子节点列表
func (s *MetadataService) PreviewTable(resourceID uint, schemaName, tableName string, page, pageSize int, tenantID *uint) (*models.TablePreview, error) {
	resource, err := s.getResourceForTenant(resourceID, tenantID)
	if err != nil {
		return nil, err
	}

	if s.previews == nil {
		return nil, fmt.Errorf("preview registry not initialized")
	}

	req := &PreviewRequest{
		Resource: resource,
		Schema:   schemaName,
		Table:    tableName,
		Page:     page,
		PageSize: pageSize,
		TenantID: tenantID,
	}

	provider, err := s.previews.Resolve(req)
	if err != nil {
		return nil, fmt.Errorf("no preview plugin available: %w", err)
	}

	ctx := context.Background()
	result, err := provider.Preview(ctx, req)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func resourceAccessible(resource *models.Resource, tenantID *uint) bool {
	if resource == nil || !resource.IsActive {
		return false
	}
	if tenantID == nil {
		return true
	}
	if resource.TenantID == nil {
		return false
	}
	return *resource.TenantID == *tenantID
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

func (s *MetadataService) getResourceForTenant(resourceID uint, tenantID *uint) (*models.Resource, error) {
	resource, err := s.getResource(resourceID)
	if err != nil {
		return nil, err
	}
	if !resourceAccessible(resource, tenantID) {
		return nil, ErrResourceAccessDenied
	}
	return resource, nil
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
