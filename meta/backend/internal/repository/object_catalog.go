package repository

import (
	"strings"
	"time"

	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func (r *ScanRepository) EnsureObjectCatalogPrefixPath(
	tenantID, engineID uint,
	bucketNode *models.MetaNode,
	scanPathPrefix string,
) (*models.MetaNode, error) {
	if bucketNode == nil || scanPathPrefix == "" {
		return bucketNode, nil
	}
	prefixSegments := strings.Split(metapath.SanitizeObjectPath(scanPathPrefix), "/")
	currentParent := bucketNode
	for idx, segment := range prefixSegments {
		if segment == "" {
			continue
		}
		fullName := metapath.ComposeNodeFullName(segment, currentParent, "/")
		pathSoFar := strings.Join(prefixSegments[:idx+1], "/")
		attrs := objectPrefixNodeAttributes(bucketNode.Name, pathSoFar+"/")
		childNode, err := r.UpsertNode(tenantID, engineID, currentParent, "prefix", segment, &fullName, attrs)
		if err != nil {
			return nil, err
		}
		currentParent = childNode
	}
	return currentParent, nil
}

func (r *ScanRepository) EnsureObjectCatalogPrefixRelativePath(
	tenantID, engineID uint,
	bucketNode, basePrefixNode *models.MetaNode,
	prefix string,
	scanPathPrefix string,
) (*models.MetaNode, []*models.MetaNode, error) {
	parent := bucketNode
	if basePrefixNode != nil {
		parent = basePrefixNode
	}
	if bucketNode == nil {
		return parent, nil, nil
	}

	parentPrefix := strings.Trim(prefix, "/")
	relative := strings.Trim(parentPrefix, "/")
	if scanPathPrefix != "" && strings.HasPrefix(relative, strings.Trim(scanPathPrefix, "/")) {
		relative = strings.TrimPrefix(relative, strings.Trim(scanPathPrefix, "/"))
		relative = strings.Trim(relative, "/")
	}
	if relative == "" {
		return parent, nil, nil
	}

	current := parent
	created := []*models.MetaNode{}
	segments := strings.Split(relative, "/")
	for idx, segment := range segments {
		if segment == "" {
			continue
		}
		fullName := metapath.ComposeNodeFullName(segment, current, "/")
		pathSoFar := metapath.JoinObjectPathParts(strings.Trim(scanPathPrefix, "/"), strings.Join(segments[:idx+1], "/"))
		attrs := objectPrefixNodeAttributes(bucketNode.Name, pathSoFar+"/")
		childNode, err := r.UpsertNode(tenantID, engineID, current, "prefix", segment, &fullName, attrs)
		if err != nil {
			return nil, nil, err
		}
		current = childNode
		created = append(created, childNode)
	}
	return current, created, nil
}

func objectPrefixNodeAttributes(bucket, path string) models.JSONMap {
	return models.JSONMap{
		"schema_version": 1,
		"storage": map[string]interface{}{
			"bucket": bucket,
			"path":   path,
		},
	}
}

func (r *ScanRepository) SoftDeleteObjectMetaItemsMissingFingerprints(tenantID, engineID uint, bucketName string, scannedFingerprints map[string]bool) ([]models.MetaItem, error) {
	if len(scannedFingerprints) == 0 {
		return nil, nil
	}

	var existingItems []models.MetaItem
	if err := r.db.Where("tenant_id = ? AND engine_id = ? AND item_type IN ?",
		tenantID, engineID, []string{"object", "table"}).
		Where("attributes->'storage'->>'bucket' = ?", bucketName).
		Find(&existingItems).Error; err != nil {
		return nil, err
	}

	deleted := make([]models.MetaItem, 0)
	for _, item := range existingItems {
		if scannedFingerprints[item.Fingerprint] {
			continue
		}
		if err := r.db.Delete(&item).Error; err != nil {
			return deleted, err
		}
		deleted = append(deleted, item)
	}
	return deleted, nil
}

func (r *ScanRepository) FinalizeObjectCatalogPrefixNode(node *models.MetaNode, itemCount int, totalSize int64) error {
	return r.FinalizeObjectCatalogPrefixNodeWithDepth(node, itemCount, totalSize, "")
}

func (r *ScanRepository) FinalizeObjectCatalogPrefixNodeWithDepth(node *models.MetaNode, itemCount int, totalSize int64, scanDepth string) error {
	now := time.Now()
	return r.db.Model(node).Updates(map[string]interface{}{
		"item_count":       itemCount,
		"total_size_bytes": totalSize,
		"scan_status":      "completed",
		"scanned_at":       now,
		"scanned_depth":    mergeScannedDepth(node.ScannedDepth, scanDepth),
	}).Error
}

func (r *ScanRepository) FindItemByFingerprintUnscoped(fingerprint string) (*models.MetaItem, bool, error) {
	var item models.MetaItem
	err := r.db.Unscoped().Where("fingerprint = ?", fingerprint).First(&item).Error
	if err == nil {
		return &item, true, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, false, nil
	}
	return nil, false, err
}
