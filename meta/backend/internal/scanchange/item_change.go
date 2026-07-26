package scanchange

import (
	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/models"
)

func ShouldUpdateTable(existingItem *models.MetaItem, tableInfo datatype.TableInfo) bool {
	if existingItem == nil {
		return true
	}
	if tableInfo.UpdatedAt != nil && existingItem.DataUpdatedAt != nil {
		return tableInfo.UpdatedAt.After(*existingItem.DataUpdatedAt)
	}
	if tableInfo.RowCount != nil && existingItem.RowCount != nil && *existingItem.RowCount != *tableInfo.RowCount {
		return true
	}
	existingTableInfo := datatype.TableInfoFromPayload(commonJSON.Section(existingItem.Attributes, "type_info.table"), "")
	if tableInfo.EstimatedRowCount != nil {
		if existingTableInfo == nil || existingTableInfo.EstimatedRowCount == nil || *existingTableInfo.EstimatedRowCount != *tableInfo.EstimatedRowCount {
			return true
		}
	}
	if tableInfo.SizeBytes != nil && existingItem.SizeBytes != nil && *existingItem.SizeBytes != *tableInfo.SizeBytes {
		return true
	}
	return (existingItem.RowCount == nil && tableInfo.RowCount != nil && *tableInfo.RowCount != 0) ||
		(existingItem.SizeBytes == nil && tableInfo.SizeBytes != nil && *tableInfo.SizeBytes != 0)
}

func ShouldUpdateDynamicSchemaItem(existingItem *models.MetaItem, estimatedDocumentCount *int64, sizeBytes int64) bool {
	if existingItem == nil {
		return true
	}
	existingTableInfo := datatype.TableInfoFromPayload(commonJSON.Section(existingItem.Attributes, "type_info.table"), "")
	if estimatedDocumentCount != nil {
		if existingTableInfo == nil || existingTableInfo.EstimatedRowCount == nil || *existingTableInfo.EstimatedRowCount != *estimatedDocumentCount {
			return true
		}
	}
	if existingItem.SizeBytes != nil && sizeBytes > 0 {
		oldSize := *existingItem.SizeBytes
		if oldSize == 0 {
			return sizeBytes != 0
		}
		change := float64(abs64(sizeBytes-oldSize)) / float64(oldSize)
		return change > 0.1
	}
	return false
}

func ShouldUpdateStorageResource(existingItem *models.MetaItem, resource metacatalog.StorageResource) bool {
	if existingItem == nil {
		return true
	}
	if existingItem.DataUpdatedAt != nil && resource.LastModified != nil && !existingItem.DataUpdatedAt.Equal(*resource.LastModified) {
		return true
	}
	if existingItem.SizeBytes != nil && *existingItem.SizeBytes != resource.SizeBytes {
		return true
	}
	return (existingItem.DataUpdatedAt == nil && resource.LastModified != nil) ||
		(existingItem.SizeBytes == nil && resource.SizeBytes != 0)
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
