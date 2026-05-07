package scanstats

import "github.com/addp/meta/internal/models"

type NodeAggregate struct {
	Node      *models.MetaNode
	ItemCount int
	TotalSize int64
}

func EnsureNodeAggregate(stats map[uint]*NodeAggregate, node *models.MetaNode) *NodeAggregate {
	if agg, ok := stats[node.ID]; ok {
		return agg
	}
	agg := &NodeAggregate{Node: node}
	stats[node.ID] = agg
	return agg
}
