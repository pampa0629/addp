package service

import (
	"io"

	"github.com/addp/common/datatype"
	"github.com/addp/meta/internal/models"
)

func (s *ScanService) CountItems(tenantID uint) (int64, error) {
	return s.metadataQueryService.CountItems(tenantID)
}

// GetObjectMetadata 获取指定对象的元数据。
func (s *ScanService) GetObjectMetadata(tenantID, engineID uint, objectKey string) (*models.MetaItem, error) {
	return s.metadataExtractor.GetObjectMetadata(tenantID, engineID, objectKey)
}

// ExtractObjectMetadataOnDemand 按需提取对象的深度元数据。
func (s *ScanService) ExtractObjectMetadataOnDemand(tenantID, engineID uint, objectKey string, token string, objectReader io.Reader) (map[string]interface{}, error) {
	return s.metadataExtractor.ExtractObjectMetadataOnDemand(tenantID, engineID, objectKey, token, objectReader)
}

// BuildObjectAccessIndexOnDemand 按需建立对象访问索引。
func (s *ScanService) BuildObjectAccessIndexOnDemand(tenantID, engineID uint, objectKey string, objectReader io.Reader) (models.JSONMap, error) {
	return s.metadataExtractor.BuildObjectAccessIndexOnDemand(tenantID, engineID, objectKey, objectReader)
}

// ListItemsByEngine 获取引擎下所有已扫描数据项。
func (s *ScanService) ListItemsByEngine(engineID, tenantID uint) ([]models.MetaItemLite, error) {
	return s.metadataQueryService.ListItemsByEngine(engineID, tenantID)
}

// ListItemsByBranch 获取 catalog 第一层业务分支下所有已扫描数据项。
func (s *ScanService) ListItemsByBranch(engineID, tenantID uint, branch string) ([]models.MetaItemLite, error) {
	return s.metadataQueryService.ListItemsByBranch(engineID, tenantID, branch)
}

// GetItemFieldDetailsByID 按 item_id 获取数据项字段详细信息。
func (s *ScanService) GetItemFieldDetailsByID(tenantID, itemID uint) ([]datatype.FieldInfo, error) {
	return s.metadataQueryService.GetItemFieldDetailsByID(tenantID, itemID)
}

// GetMetadataTree 获取资源的完整元数据树。
func (s *ScanService) GetMetadataTree(tenantID, engineID uint) (*models.MetadataTreeResponse, error) {
	return s.metadataQueryService.GetMetadataTree(tenantID, engineID)
}

// GetNodeByCatalogPath 按 catalog path 查询节点。
func (s *ScanService) GetNodeByCatalogPath(tenantID, engineID uint, catalogPath string) (*models.MetaNodeLite, error) {
	return s.metadataQueryService.GetNodeByCatalogPath(tenantID, engineID, catalogPath)
}

// GetItemByCatalogPath 按 catalog path 查询数据项。
func (s *ScanService) GetItemByCatalogPath(tenantID, engineID uint, catalogPath string) (*models.MetaItemLite, error) {
	return s.metadataQueryService.GetItemByCatalogPath(tenantID, engineID, catalogPath)
}

// GetNodeChildren 获取节点的子节点。
func (s *ScanService) GetNodeChildren(tenantID, nodeID uint) ([]models.MetaNodeLite, error) {
	return s.metadataQueryService.GetNodeChildren(tenantID, nodeID)
}

// GetNodeItems 获取节点下的数据项。
func (s *ScanService) GetNodeItems(tenantID, nodeID uint) ([]models.MetaItemLite, error) {
	return s.metadataQueryService.GetNodeItems(tenantID, nodeID)
}

// GetItemSpatialMetadataByID 按 item_id 获取数据项空间元数据。
func (s *ScanService) GetItemSpatialMetadataByID(tenantID, itemID uint) (*models.SpatialMetadataResponse, error) {
	return s.metadataQueryService.GetItemSpatialMetadataByID(tenantID, itemID)
}

// GetMetaNodeByID 获取单个节点详情。
func (s *ScanService) GetMetaNodeByID(tenantID, nodeID uint) (*models.MetaNodeLite, error) {
	return s.metadataQueryService.GetMetaNodeByID(tenantID, nodeID)
}

// GetItemByID 按 ID 查询数据项。
func (s *ScanService) GetItemByID(tenantID, itemID uint) (*models.MetaItemLite, error) {
	return s.metadataQueryService.GetItemByID(tenantID, itemID)
}
