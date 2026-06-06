package service

import (
	"context"

	"github.com/addp/common/datatype"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/search"
)

// IndexTableAsset 索引表资产到 Meilisearch
func (s *IndexerService) IndexTableAsset(resource *commonModels.Engine, tenantID uint, schemaName string, tableInfo datatype.TableInfo, fields []datatype.FieldInfo, item *models.MetaItem) {
	if s.indexer == nil || !s.indexer.Enabled() || resource == nil || item == nil {
		return
	}

	metadata := search.NormalizeMap(copyJSONMap(item.Attributes))
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	delete(metadata, "fields")

	fieldRecords := make([]search.FieldRecord, 0, len(fields))
	for _, field := range fields {
		fieldRecords = append(fieldRecords, search.FieldRecord{
			Name:         field.Name,
			DataType:     string(field.Type),
			ColumnType:   string(field.Type),
			Comment:      field.Comment,
			IsNullable:   field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
		})
	}

	record := &search.AssetRecord{
		AssetID:       item.Fingerprint,
		DocumentID:    item.Fingerprint,
		Locator:       metaItemLocator(resource.ID, resource.EngineType, "table", item.FullName, &item.ID),
		TenantID:      tenantID,
		EngineID:      resource.ID,
		EngineName:    resource.Name,
		EngineType:    resource.EngineType,
		AssetType:     "table",
		Name:          item.Name,
		FullName:      item.FullName,
		Schema:        schemaName,
		TableKind:     tableKindForIndex(tableInfo),
		Description:   tableInfo.Comment,
		RowCount:      item.RowCount,
		SizeBytes:     item.SizeBytes,
		Metadata:      metadata,
		Fields:        fieldRecords,
		DataUpdatedAt: item.DataUpdatedAt,
	}

	if err := s.indexer.IndexAsset(context.Background(), record); err != nil {
		s.log.Warn("索引表元数据失败", "fingerprint", item.Fingerprint, "schema", schemaName, "error", err)
	}
}

func tableKindForIndex(tableInfo datatype.TableInfo) string {
	if tableInfo.Kind != "" {
		return tableInfo.Kind
	}
	return "table"
}
