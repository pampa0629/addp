package service

import (
	"fmt"
	"path"
	"strings"

	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// ObjectMetadataService 负责已扫描对象 metadata 查询。
type ObjectMetadataService struct {
	db *gorm.DB
}

func NewObjectMetadataService(db *gorm.DB) *ObjectMetadataService {
	return &ObjectMetadataService{db: db}
}

func (s *ObjectMetadataService) GetObjectMetadata(tenantID, engineID uint, objectKey string) (*models.MetaItem, error) {
	bucket, relativePath := metapath.SplitObjectPath(objectKey)
	if bucket == "" {
		return nil, fmt.Errorf("invalid object key: %s", objectKey)
	}

	var bucketNode models.MetaNode
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND node_type = ? AND name = ?",
		tenantID, engineID, "bucket", bucket).First(&bucketNode).Error; err != nil {
		return nil, fmt.Errorf("bucket not found: %w", err)
	}

	parentNode := bucketNode
	objectName := path.Base(relativePath)
	if objectName == "" {
		objectName = relativePath
	}
	if relativePath != "" && relativePath != objectName {
		prefixPath := path.Dir(relativePath)
		for _, segment := range strings.Split(prefixPath, "/") {
			if segment == "" || segment == "." {
				continue
			}
			var prefixNode models.MetaNode
			if err := s.db.Where("tenant_id = ? AND engine_id = ? AND parent_node_id = ? AND node_type = ? AND name = ?",
				tenantID, engineID, parentNode.ID, "prefix", segment).First(&prefixNode).Error; err != nil {
				return nil, fmt.Errorf("prefix not found: %s", segment)
			}
			parentNode = prefixNode
		}
	}

	var item models.MetaItem
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND name = ?",
		tenantID, engineID, parentNode.ID, "object", objectName).First(&item).Error; err != nil {
		return nil, fmt.Errorf("object metadata not found: %w", err)
	}
	return &item, nil
}
