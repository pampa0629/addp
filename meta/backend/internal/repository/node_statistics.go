package repository

import "gorm.io/gorm"

// NodeStatistics is a read-time projection, never a persisted scan result.
type NodeStatistics struct {
	NodeID         uint
	ItemCount      int
	TotalSizeBytes int64
	HasChildren    bool
}

// QueryNodeStatistics aggregates all requested subtrees in one statement.
// Parent edges, not path strings, define membership. UNION also terminates a
// malformed cycle without counting a node twice in the same requested subtree.
func QueryNodeStatistics(db *gorm.DB, tenantID, engineID uint, nodeIDs []uint) ([]NodeStatistics, error) {
	if len(nodeIDs) == 0 {
		return []NodeStatistics{}, nil
	}
	var result []NodeStatistics
	err := db.Raw(nodeStatisticsSQL, tenantID, engineID, nodeIDs, tenantID, engineID, tenantID, engineID).Scan(&result).Error
	return result, err
}

const nodeStatisticsSQL = `WITH RECURSIVE subtree(root_id, id) AS (
		SELECT id, id FROM meta.meta_node
		WHERE tenant_id = ? AND engine_id = ? AND id IN ? AND deleted_at IS NULL
		UNION
		SELECT s.root_id, n.id
		FROM subtree s JOIN meta.meta_node n
		  ON n.parent_node_id = s.id
		WHERE n.tenant_id = ? AND n.engine_id = ? AND n.deleted_at IS NULL
	)
	SELECT s.root_id AS node_id, COUNT(i.id) AS item_count,
	       COALESCE(SUM(i.size_bytes), 0) AS total_size_bytes,
	       MAX(CASE WHEN s.id <> s.root_id OR i.id IS NOT NULL THEN 1 ELSE 0 END) = 1 AS has_children
	FROM subtree s LEFT JOIN meta.meta_item i
	  ON i.node_id = s.id AND i.tenant_id = ? AND i.engine_id = ? AND i.deleted_at IS NULL
	GROUP BY s.root_id`
