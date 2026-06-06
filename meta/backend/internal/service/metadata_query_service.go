package service

import (
	"fmt"

	"github.com/addp/meta/internal/metaquery"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// MetadataQueryService 元数据查询服务
// 提供Manager和Transfer模块的元数据查询接口
type MetadataQueryService struct {
	db *gorm.DB
}

// NewMetadataQueryService 创建元数据查询服务
func NewMetadataQueryService(db *gorm.DB) *MetadataQueryService {
	return &MetadataQueryService{
		db: db,
	}
}

func (s *MetadataQueryService) CountItems(tenantID uint) (int64, error) {
	var itemCount int64
	if err := s.db.Table("meta.meta_item").Where("tenant_id = ?", tenantID).Count(&itemCount).Error; err != nil {
		return 0, err
	}
	return itemCount, nil
}

func (s *MetadataQueryService) GetItemSpatialMetadataByID(tenantID, itemID uint) (*models.SpatialMetadataResponse, error) {
	var item models.MetaItem
	if err := s.db.Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, itemID).First(&item).Error; err != nil {
		return nil, fmt.Errorf("metadata snapshot not found: %w", err)
	}
	return metaquery.SpatialMetadataFromItem(item)
}
