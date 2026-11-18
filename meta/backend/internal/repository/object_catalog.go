package repository

import (
	"context"
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

// SoftDeleteMissingObjectItems reconciles the complete inventory of a bucket or prefix.
func (r *ScanRepository) SoftDeleteMissingObjectItems(ctx context.Context, root *models.MetaNode, observed map[string]bool) ([]string, error) {
	const scope = `WITH RECURSIVE scope(id) AS (
		SELECT id FROM meta.meta_node WHERE id = ? AND tenant_id = ? AND engine_id = ? AND deleted_at IS NULL
		UNION SELECT n.id FROM meta.meta_node n JOIN scope s ON n.parent_node_id = s.id
		WHERE n.tenant_id = ? AND n.engine_id = ? AND n.deleted_at IS NULL
	) SELECT id FROM scope`
	var deleted []string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing []models.MetaItem
		if err := tx.Select("id", "fingerprint").Where("tenant_id = ? AND engine_id = ?", root.TenantID, root.EngineID).
			Where("node_id IN ("+scope+")", root.ID, root.TenantID, root.EngineID, root.TenantID, root.EngineID).
			Find(&existing).Error; err != nil {
			return err
		}
		for _, item := range existing {
			if observed[item.Fingerprint] {
				continue
			}
			result := tx.Delete(&item)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 0 {
				deleted = append(deleted, item.Fingerprint)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
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
