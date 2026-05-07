package metaquery

import "github.com/addp/meta/internal/models"

func ToMetaNodeLite(node models.MetaNode) models.MetaNodeLite {
	var scannedAt *string
	if node.ScannedAt != nil {
		formatted := node.ScannedAt.UTC().Format("2006-01-02T15:04:05Z")
		scannedAt = &formatted
	}

	return models.MetaNodeLite{
		ID:             node.ID,
		TenantID:       node.TenantID,
		EngineID:       node.EngineID,
		ParentNodeID:   node.ParentNodeID,
		NodeType:       node.NodeType,
		Name:           node.Name,
		FullName:       node.FullName,
		Depth:          node.Depth,
		Path:           node.Path,
		ScanStatus:     node.ScanStatus,
		ScannedAt:      scannedAt,
		ItemCount:      node.ItemCount,
		TotalSizeBytes: node.TotalSizeBytes,
		Attributes:     node.Attributes,
	}
}

func ToMetaItemLite(item models.MetaItem) models.MetaItemLite {
	var dataUpdatedAt *string
	if item.DataUpdatedAt != nil {
		formatted := item.DataUpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
		dataUpdatedAt = &formatted
	}

	return models.MetaItemLite{
		ID:            item.ID,
		TenantID:      item.TenantID,
		EngineID:      item.EngineID,
		NodeID:        item.NodeID,
		ItemType:      item.ItemType,
		Name:          item.Name,
		FullName:      item.FullName,
		RowCount:      item.RowCount,
		SizeBytes:     item.SizeBytes,
		DataUpdatedAt: dataUpdatedAt,
		Attributes:    item.Attributes,
	}
}
