package service

import (
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
	if err := s.db.Table("meta.meta_item").Where("tenant_id = ? AND deleted_at IS NULL", tenantID).Count(&itemCount).Error; err != nil {
		return 0, err
	}
	return itemCount, nil
}
