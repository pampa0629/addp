package scanflow

import "github.com/addp/meta/internal/models"

type ObjectCatalogNodeAggregate struct {
	Node      *models.MetaNode
	ItemCount int
	TotalSize int64
}

func EnsureObjectCatalogNodeAggregate(stats map[uint]*ObjectCatalogNodeAggregate, node *models.MetaNode) *ObjectCatalogNodeAggregate {
	if agg, ok := stats[node.ID]; ok {
		return agg
	}
	agg := &ObjectCatalogNodeAggregate{Node: node}
	stats[node.ID] = agg
	return agg
}
