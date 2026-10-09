package service

import (
	"context"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/models"
)

// IndexTechnicalMetadata submits the registered DataItem facts without reading source content.
func (s *IndexerService) IndexTechnicalMetadata(ctx context.Context, resource *commonModels.Engine, tenantID uint, parent *models.MetaNode, item *models.MetaItem) bool {
	if s.contentIndex == nil || resource == nil || item == nil {
		return false
	}
	if err := s.contentIndex.WithTenantID(tenantID).UpsertDocument(ctx, technicalMetadataDocument(resource, parent, item)); err != nil {
		s.log.Warn("提交 DataItem 技术元数据投影失败", "fingerprint", item.Fingerprint, "item_type", item.ItemType, "error", err)
		return false
	}
	return true
}

func technicalMetadataDocument(resource *commonModels.Engine, parent *models.MetaNode, item *models.MetaItem) commonClient.ManagerContentDocument {
	doc := commonClient.ManagerContentDocument{
		DocumentID: item.Fingerprint, PayloadKind: commonClient.ManagerContentPayloadTechnicalMetadata,
		EngineID: resource.ID, EngineName: resource.Name, EngineType: resource.EngineType,
		DataItemType: item.ItemType, Name: item.Name, FullName: item.FullName,
		RowCount: item.RowCount, SizeBytes: item.SizeBytes, DataUpdatedAt: item.DataUpdatedAt,
		ContentType:  commonJSON.String(item.Attributes, "storage", "content_type"),
		DocumentType: commonJSON.String(item.Attributes, "item", "format"), ProjectionTime: time.Now().UTC(),
	}
	loc := resourcetree.LocatorFromFullName(resource.ID, resource.EngineType, item.ItemType, item.FullName, &item.ID)
	if parent != nil && (parent.NodeType == "schema" || parent.NodeType == "database") {
		doc.Schema = parent.FullName
		if doc.Schema == "" {
			doc.Schema = parent.Name
		}
		// A namespace name may contain dots. Use registered segments rather than splitting full_name.
		if loc != nil {
			loc.Path = []string{doc.Schema, item.Name}
		}
	}
	switch item.ItemType {
	case "table", "view", "collection":
		doc.TableKind = item.ItemType
	case "index", "keyspace", "topic":
		if loc != nil {
			loc.Path = []string{item.Name}
		}
	case "object", "file":
		doc.Bucket = commonJSON.String(item.Attributes, "storage", "bucket")
		doc.Path = commonJSON.String(item.Attributes, "storage", "path")
	}
	if loc != nil {
		doc.Locator = loc.ToURI()
	}
	if table := datatype.TableInfoFromPayload(commonJSON.Section(item.Attributes, "type_info.table"), item.Name); table != nil {
		doc.Fields = managerContentFields(table.Fields)
		doc.Description = table.Comment
		if table.Kind != "" {
			doc.TableKind = table.Kind
		}
	}
	return doc
}

func managerContentFields(fields []datatype.FieldInfo) []commonClient.ManagerContentField {
	result := make([]commonClient.ManagerContentField, 0, len(fields))
	for _, field := range fields {
		result = append(result, commonClient.ManagerContentField{
			Name: field.Name, DataType: string(field.Type), ColumnType: string(field.Type),
			Comment: field.Comment, IsNullable: field.Nullable, IsPrimaryKey: field.PrimaryKey,
		})
	}
	return result
}
