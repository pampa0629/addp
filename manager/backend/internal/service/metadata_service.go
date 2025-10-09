package service

import (
	"fmt"
	"sort"
	"strings"

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

	topNodes, childNodes, items, err := s.metadataRepo.ListScannedNodesAndItems()
	if err != nil {
		return nil, err
	}

	topNodesByResource := make(map[uint][]*models.MetaNodeLite)
	for i := range topNodes {
		node := &topNodes[i]
		topNodesByResource[node.ResourceID] = append(topNodesByResource[node.ResourceID], node)
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

	var result []models.DataExplorerResource
	for _, res := range resources {
		rootNodes := topNodesByResource[res.ID]
		if len(rootNodes) == 0 {
			continue
		}

		resourceType := strings.ToLower(res.ResourceType)
		var schemasForResource []models.DataExplorerSchema

		if isObjectStorageType(resourceType) {
			for _, bucket := range rootNodes {
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
			for _, schemaNode := range rootNodes {
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

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })

	return result, nil
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

// PreviewTable 获取表数据预览
// 当 tableName 为空时，返回 schema/bucket 的统计信息和子节点列表
func (s *MetadataService) PreviewTable(resourceID uint, schemaName, tableName string, page, pageSize int) (*models.TablePreview, error) {
	resource, err := s.getResource(resourceID)
	if err != nil {
		return nil, err
	}

	// 如果 tableName 为空，表示查看 schema/bucket 节点信息
	if tableName == "" {
		return s.previewSchemaOrBucket(resource, schemaName)
	}

	if isObjectStorageType(resource.ResourceType) {
		return s.previewObjectStorage(resource, schemaName, tableName)
	}

	const maxRows = 50
	columns, rows, total, geometryColumns, err := s.metadataRepo.QueryTablePreview(resource, schemaName, tableName, page, pageSize, maxRows)
	if err != nil {
		return nil, err
	}

	return &models.TablePreview{
		Mode:            "table",
		Columns:         columns,
		Rows:            rows,
		Total:           total,
		Page:            page,
		PageSize:        pageSize,
		GeometryColumns: geometryColumns,
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

// previewSchemaOrBucket 预览 schema 或 bucket 节点信息
// 显示节点统计信息（表/对象数量、总大小）和直接子节点列表
func (s *MetadataService) previewSchemaOrBucket(resource *models.Resource, nodeName string) (*models.TablePreview, error) {
	// 查询节点信息
	node, err := s.metadataRepo.GetNodeByName(resource.ID, nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get node info: %w", err)
	}

	// 根据资源类型确定节点类型
	nodeType := "directory"
	if isObjectStorageType(resource.ResourceType) {
		nodeType = "bucket"
	} else {
		nodeType = "schema"
	}

	// 获取直接子节点（子目录/前缀）和子项（表/对象）
	children := make([]map[string]interface{}, 0)

	// 查询子节点
	childNodes, err := s.metadataRepo.GetChildNodes(node.ID)
	if err == nil {
		for _, child := range childNodes {
			children = append(children, map[string]interface{}{
				"type":       "node",
				"node_type":  child.NodeType,
				"name":       child.Name,
				"full_name":  child.FullName,
				"item_count": child.ItemCount,
				"size_bytes": child.TotalSizeBytes,
			})
		}
	}

	// 查询子项
	items, err := s.metadataRepo.GetNodeItems(node.ID)
	if err == nil {
		for _, item := range items {
			itemMap := map[string]interface{}{
				"type":      "item",
				"item_type": item.ItemType,
				"name":      item.Name,
				"full_name": item.FullName,
			}
			if item.RowCount != nil {
				itemMap["row_count"] = *item.RowCount
			}
			if item.SizeBytes != nil {
				itemMap["size_bytes"] = *item.SizeBytes
			}
			if item.ObjectSizeBytes != nil {
				itemMap["object_size_bytes"] = *item.ObjectSizeBytes
			}
			children = append(children, itemMap)
		}
	}

	return &models.TablePreview{
		Mode:     "node",
		Columns:  []string{},
		Rows:     []map[string]interface{}{},
		Total:    0,
		Page:     1,
		PageSize: 1,
		Object: &models.ObjectPreview{
			NodeType:    nodeType,
			Path:        nodeName,
			ContentType: "application/x-directory",
			ObjectCount: int64(node.ItemCount),
			SizeBytes:   node.TotalSizeBytes,
			Children: func() []models.ObjectPreviewChild {
				result := make([]models.ObjectPreviewChild, 0, len(children))
				for _, child := range children {
					c := models.ObjectPreviewChild{
						Name: child["name"].(string),
						Path: child["full_name"].(string),
					}
					if t, ok := child["node_type"].(string); ok {
						c.Type = t
					} else if t, ok := child["item_type"].(string); ok {
						c.Type = t
					}
					if size, ok := child["size_bytes"].(int64); ok {
						c.SizeBytes = size
					} else if size, ok := child["object_size_bytes"].(int64); ok {
						c.SizeBytes = size
					}
					result = append(result, c)
				}
				return result
			}(),
		},
		GeometryColumns: []string{},
	}, nil
}
