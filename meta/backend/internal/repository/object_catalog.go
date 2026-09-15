package repository

import (
	"fmt"
	"strings"

	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanresource"
	"gorm.io/gorm"
)

func (r *ScanRepository) EnsureObjectCatalogPrefixPath(
	tenantID, engineID uint,
	bucketNode *models.MetaNode,
	prefix string,
) (*models.MetaNode, error) {
	chain, err := r.EnsureObjectCatalogPrefixChain(tenantID, engineID, bucketNode, prefix)
	if err != nil {
		return nil, err
	}
	return chain[len(chain)-1], nil
}

// EnsureObjectCatalogPrefixChain 构造 bucket 内规范路径的完整父链，不接受扫描起点。
func (r *ScanRepository) EnsureObjectCatalogPrefixChain(
	tenantID, engineID uint,
	bucketNode *models.MetaNode,
	prefix string,
) ([]*models.MetaNode, error) {
	if bucketNode == nil || bucketNode.NodeType != "bucket" || bucketNode.TenantID != tenantID || bucketNode.EngineID != engineID {
		return nil, fmt.Errorf("object prefix chain requires a bucket in tenant %d engine %d", tenantID, engineID)
	}
	chain := []*models.MetaNode{bucketNode}
	prefixSegments := strings.Split(metapath.SanitizeObjectPath(prefix), "/")
	currentParent := bucketNode
	for idx, segment := range prefixSegments {
		if segment == "" {
			continue
		}
		fullName := metapath.ComposeNodeFullName(segment, currentParent, "/")
		pathSoFar := strings.Join(prefixSegments[:idx+1], "/")
		attrs := scanresource.ObjectPrefixNodeAttributes(bucketNode.Name, pathSoFar+"/")
		childNode, err := r.UpsertNode(tenantID, engineID, currentParent, "prefix", segment, &fullName, attrs)
		if err != nil {
			return nil, err
		}
		currentParent = childNode
		chain = append(chain, childNode)
	}
	return chain, nil
}

func (r *ScanRepository) SoftDeleteObjectMetaItemsMissingFingerprints(tenantID, engineID uint, bucketName string, scannedFingerprints map[string]bool) ([]models.MetaItem, error) {
	return r.SoftDeleteObjectMetaItemsMissingFingerprintsInPrefix(tenantID, engineID, bucketName, "", scannedFingerprints)
}

func (r *ScanRepository) SoftDeleteObjectMetaItemsMissingFingerprintsInPrefix(tenantID, engineID uint, bucketName, objectPrefix string, scannedFingerprints map[string]bool) ([]models.MetaItem, error) {
	if len(scannedFingerprints) == 0 {
		return nil, nil
	}

	var existingItems []models.MetaItem
	query := r.db.Where("tenant_id = ? AND engine_id = ? AND item_type IN ?",
		tenantID, engineID, []string{"object", "table"}).
		Where("attributes->'storage'->>'bucket' = ?", bucketName).
		Where("deleted_at IS NULL")
	objectPrefix = strings.Trim(objectPrefix, "/")
	if objectPrefix != "" {
		fullPrefix := strings.Trim(bucketName, "/") + "/" + objectPrefix + "/"
		query = query.Where("full_name = ? OR full_name LIKE ?", strings.TrimSuffix(fullPrefix, "/"), fullPrefix+"%")
	}
	if err := query.Find(&existingItems).Error; err != nil {
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
