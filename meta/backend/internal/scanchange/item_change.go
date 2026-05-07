package scanchange

import (
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/models"
)

func ShouldUpdateTable(existingItem *models.MetaItem, tableInfo format.ScannerTableInfo) bool {
	if existingItem == nil {
		return true
	}
	if tableInfo.LastModified != nil && existingItem.DataUpdatedAt != nil {
		return tableInfo.LastModified.After(*existingItem.DataUpdatedAt)
	}
	if existingItem.RowCount != nil && *existingItem.RowCount != tableInfo.RowCount {
		return true
	}
	if existingItem.SizeBytes != nil && *existingItem.SizeBytes != tableInfo.SizeBytes {
		return true
	}
	return (existingItem.RowCount == nil && tableInfo.RowCount != 0) ||
		(existingItem.SizeBytes == nil && tableInfo.SizeBytes != 0)
}

func ShouldUpdateCollection(existingItem *models.MetaItem, newInfo plugin.CollectionInfo) bool {
	if existingItem == nil {
		return true
	}
	if existingItem.RowCount != nil && *existingItem.RowCount != newInfo.DocumentCount {
		return true
	}
	if existingItem.SizeBytes != nil && newInfo.SizeBytes > 0 {
		oldSize := *existingItem.SizeBytes
		if oldSize == 0 {
			return newInfo.SizeBytes != 0
		}
		change := float64(abs64(newInfo.SizeBytes-oldSize)) / float64(oldSize)
		return change > 0.1
	}
	return false
}

func ShouldUpdateObject(existingItem *models.MetaItem, objectInfo format.ObjectMetadata) bool {
	if existingItem == nil {
		return true
	}
	if existingItem.DataUpdatedAt != nil && objectInfo.LastModified != nil && !existingItem.DataUpdatedAt.Equal(*objectInfo.LastModified) {
		return true
	}
	if existingItem.SizeBytes != nil && *existingItem.SizeBytes != objectInfo.SizeBytes {
		return true
	}
	return (existingItem.DataUpdatedAt == nil && objectInfo.LastModified != nil) ||
		(existingItem.SizeBytes == nil && objectInfo.SizeBytes != 0)
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
