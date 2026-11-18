package repository

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanresource"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
func (r *ScanRepository) SoftDeleteMissingObjectItems(ctx context.Context, root *models.MetaNode, observed map[string]bool) error {
	const scope = `WITH RECURSIVE scope(id) AS (
		SELECT id FROM meta.meta_node WHERE id = ? AND tenant_id = ? AND engine_id = ? AND deleted_at IS NULL
		UNION SELECT n.id FROM meta.meta_node n JOIN scope s ON n.parent_node_id = s.id
		WHERE n.tenant_id = ? AND n.engine_id = ? AND n.deleted_at IS NULL
	) SELECT id FROM scope`
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing []models.MetaItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "fingerprint").Where("tenant_id = ? AND engine_id = ?", root.TenantID, root.EngineID).
			Where("node_id IN ("+scope+")", root.ID, root.TenantID, root.EngineID, root.TenantID, root.EngineID).
			Order("id").Find(&existing).Error; err != nil {
			return err
		}
		var missing []string
		for _, item := range existing {
			if !observed[item.Fingerprint] {
				missing = append(missing, item.Fingerprint)
			}
		}
		now := time.Now()
		for batch := range slices.Chunk(missing, commonclient.ManagerContentBatchLimit) {
			var prior []models.ObjectIndexDeletion
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("fingerprint IN ?", batch).Order("fingerprint").Find(&prior).Error; err != nil {
				return err
			}
			pending := make(map[string]string, len(prior))
			for _, deletion := range prior {
				if !deletion.Confirmed {
					pending[deletion.Fingerprint] = deletion.OperationID
				}
			}
			operationID := uuid.NewString()
			var deletions []models.ObjectIndexDeletion
			for _, fingerprint := range batch {
				id := pending[fingerprint]
				if id == "" {
					id = operationID
					deletions = append(deletions, models.ObjectIndexDeletion{
						Fingerprint: fingerprint, TenantID: root.TenantID, EngineID: root.EngineID, OperationID: id,
					})
				}
				if err := tx.Model(&models.MetaItem{}).Where("fingerprint = ?", fingerprint).
					Updates(map[string]any{"deleted_at": now, "index_delete_operation_id": id}).Error; err != nil {
					return err
				}
			}
			if len(deletions) > 0 {
				if err := tx.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "fingerprint"}},
					DoUpdates: clause.AssignmentColumns([]string{"operation_id", "confirmed", "restored"}),
				}).Create(&deletions).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
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
