package service

import (
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// A request-local committed technical snapshot. It holds no authorization result.
type metadataNodeLookup func(tenant, engine, id uint) (models.MetaNode, error)

func loadMetadataIdentityNodes(tx *gorm.DB, tenant uint, items []models.MetaItem) (metadataNodeLookup, error) {
	nodes := map[uint]models.MetaNode{}
	seen := map[uint]bool{}
	engines := map[uint]bool{}
	pending := []uint{}
	for _, item := range items {
		engines[item.EngineID] = true
		if item.NodeID != 0 && !seen[item.NodeID] {
			seen[item.NodeID] = true
			pending = append(pending, item.NodeID)
		}
	}
	engineIDs := make([]uint, 0, len(engines))
	for id := range engines {
		engineIDs = append(engineIDs, id)
	}
	for depth := 0; len(pending) > 0 && depth < 64; depth++ {
		if err := tx.Statement.Context.Err(); err != nil {
			return nil, err
		}
		var level []models.MetaNode
		// Each caller bounds its item batch; the node frontier is no larger.
		err := tx.Select("id", "tenant_id", "engine_id", "parent_node_id", "name", "node_type", "full_name").Where("tenant_id=? AND engine_id IN ? AND id IN ? AND deleted_at IS NULL", tenant, engineIDs, pending).Find(&level).Error
		if err != nil {
			return nil, err
		}
		pending = nil
		for _, n := range level {
			nodes[n.ID] = n
			if n.ParentNodeID != nil && *n.ParentNodeID != 0 && !seen[*n.ParentNodeID] {
				seen[*n.ParentNodeID] = true
				pending = append(pending, *n.ParentNodeID)
			}
		}
	}
	return func(owner, engine, id uint) (models.MetaNode, error) {
		n, ok := nodes[id]
		if !ok || owner != tenant || n.TenantID != owner || n.EngineID != engine {
			return models.MetaNode{}, gorm.ErrRecordNotFound
		}
		return n, nil
	}, nil
}
