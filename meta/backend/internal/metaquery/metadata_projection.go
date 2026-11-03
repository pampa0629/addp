package metaquery

import (
	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
)

// MetadataOnlyItem is a structural projection, never a copy of stored attributes.
// Content, statistics, comments and expressions belong to separately guarded reads.
func MetadataOnlyItem(item models.MetaItemLite) models.MetaItemLite {
	safe := models.MetaItemLite{ID: item.ID, TenantID: item.TenantID, EngineID: item.EngineID, NodeID: item.NodeID,
		ItemType: item.ItemType, Name: item.Name, FullName: item.FullName, Fingerprint: item.Fingerprint,
		ScannedAt: item.ScannedAt, ScannedDepth: item.ScannedDepth,
		Attributes: map[string]interface{}{"schema_version": 1}}
	identity := map[string]interface{}{}
	for _, key := range []string{"data_type", "format", "layout"} {
		if value := commonJSON.String(item.Attributes, "item", key); value != "" {
			identity[key] = value
		}
	}
	if len(identity) > 0 {
		safe.Attributes["item"] = identity
	}
	if table := tableInfoFromMetaAttributes(item.Attributes); table != nil {
		safe.Attributes["type_info"] = map[string]interface{}{"table": datatype.TableInfoPayload(&datatype.TableInfo{
			Name: table.Name, Kind: table.Kind, Fields: metadataOnlyFields(table.Fields), PrimaryKey: append([]string(nil), table.PrimaryKey...),
		})}
	}
	// Container names and kinds are structure. Never copy child row counts,
	// sizes, bounds, native expressions or arbitrary child attributes.
	if raw := commonJSON.Section(item.Attributes, "type_info.container"); raw != nil {
		container := map[string]interface{}{}
		if children, ok := raw["children"].([]interface{}); ok {
			structural := make([]interface{}, 0, len(children))
			for _, child := range children {
				if obj, ok := child.(map[string]interface{}); ok {
					c := map[string]interface{}{}
					for _, key := range []string{"name", "child_kind", "data_type"} {
						if value, ok := obj[key].(string); ok {
							c[key] = value
						}
					}
					structural = append(structural, c)
				}
			}
			container["children"] = structural
		}
		if value, ok := raw["default_child"].(string); ok {
			container["default_child"] = value
		}
		if len(container) > 0 {
			if safe.Attributes["type_info"] == nil {
				safe.Attributes["type_info"] = map[string]interface{}{}
			}
			safe.Attributes["type_info"].(map[string]interface{})["container"] = container
		}
	}
	if spatial := spatialInfoFromMetaAttributes(item.Attributes); spatial != nil {
		// Declaration facts only; extent/capture location are derived from content.
		declaration := &datatype.SpatialInfo{SRID: spatial.SRID, CRSRef: spatial.CRSRef,
			CRSDefinitions:  append([]datatype.CRSDefinition(nil), spatial.CRSDefinitions...),
			GeometryColumns: append([]datatype.GeometryColumnInfo(nil), spatial.GeometryColumns...), PrimaryGeometryColumn: spatial.PrimaryGeometryColumn}
		safe.Attributes["capabilities"] = map[string]interface{}{"spatial": datatype.SpatialInfoPayload(declaration)}
	}
	return safe
}

func metadataOnlyFields(fields []datatype.FieldInfo) []datatype.FieldInfo {
	result := make([]datatype.FieldInfo, len(fields))
	for i, f := range fields {
		result[i] = datatype.FieldInfo{Name: f.Name, Path: append([]string(nil), f.Path...), Type: f.Type, ElementType: f.ElementType,
			NativeType: f.NativeType, Nullable: f.Nullable, PrimaryKey: f.PrimaryKey, Size: f.Size, Precision: f.Precision, Scale: f.Scale,
			OrdinalPosition: f.OrdinalPosition, Generated: f.Generated, Fields: metadataOnlyFields(f.Fields)}
		if f.Element != nil {
			element := metadataOnlyFields([]datatype.FieldInfo{*f.Element})[0]
			result[i].Element = &element
		}
	}
	return result
}

func MetadataOnlyNode(node models.MetaNodeLite, count int, hasChildren bool) models.MetaNodeLite {
	return models.MetaNodeLite{ID: node.ID, TenantID: node.TenantID, EngineID: node.EngineID, ParentNodeID: node.ParentNodeID,
		NodeType: node.NodeType, Name: node.Name, FullName: node.FullName, Depth: node.Depth,
		ItemCount: count, HasChildren: hasChildren}
}
