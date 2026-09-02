package service

import (
	"context"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
)

func (s *IndexerService) IndexTableContent(ctx context.Context, resource *commonModels.Engine, tenantID uint, schemaName string, tableInfo datatype.TableInfo, fields []datatype.FieldInfo, item *models.MetaItem) {
	if s.contentIndex == nil || resource == nil || item == nil {
		return
	}

	fieldRecords := make([]commonClient.ManagerContentField, 0, len(fields))
	for _, field := range fields {
		fieldRecords = append(fieldRecords, commonClient.ManagerContentField{
			Name:         field.Name,
			DataType:     string(field.Type),
			ColumnType:   string(field.Type),
			Comment:      field.Comment,
			IsNullable:   field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
		})
	}

	document := commonClient.ManagerContentDocument{
		DocumentID:     item.Fingerprint,
		PayloadKind:    commonClient.ManagerContentPayloadTechnicalMetadata,
		Locator:        metaItemLocator(resource.ID, resource.EngineType, "table", item.FullName, &item.ID),
		EngineID:       resource.ID,
		EngineName:     resource.Name,
		EngineType:     resource.EngineType,
		DataItemType:   "table",
		Name:           item.Name,
		FullName:       item.FullName,
		Schema:         schemaName,
		TableKind:      tableKindForIndex(tableInfo),
		Description:    tableInfo.Comment,
		RowCount:       item.RowCount,
		SizeBytes:      item.SizeBytes,
		Fields:         fieldRecords,
		DataUpdatedAt:  item.DataUpdatedAt,
		ProjectionTime: time.Now().UTC(),
	}

	if err := s.contentIndex.WithTenantID(tenantID).UpsertDocument(ctx, document); err != nil {
		s.log.Warn("索引表元数据失败", "fingerprint", item.Fingerprint, "schema", schemaName, "error", err)
	}
}

func tableKindForIndex(tableInfo datatype.TableInfo) string {
	if tableInfo.Kind != "" {
		return tableInfo.Kind
	}
	return "table"
}
