package plugin

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestItemMetadataFieldsPrefersTableInfo(t *testing.T) {
	metadata := &ItemMetadata{
		Fields: []datatype.FieldInfo{{Name: "legacy"}},
		Table: &datatype.TableInfo{
			Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}},
		},
	}

	fields := ItemMetadataFields(metadata)
	if len(fields) != 1 || fields[0].Name != "id" {
		t.Fatalf("ItemMetadataFields() = %#v", fields)
	}
	fields[0].Name = "changed"
	if metadata.Table.Fields[0].Name != "id" {
		t.Fatalf("ItemMetadataFields returned mutable table fields")
	}
}

func TestItemMetadataFieldsFallsBackToFields(t *testing.T) {
	metadata := &ItemMetadata{
		Kind:   "collection",
		Fields: []datatype.FieldInfo{{Name: "_id", Type: datatype.FieldTypeString}},
	}

	fields := ItemMetadataFields(metadata)
	if len(fields) != 1 || fields[0].Name != "_id" {
		t.Fatalf("ItemMetadataFields() = %#v", fields)
	}
	if info := ItemMetadataTableInfo(metadata); info != nil {
		t.Fatalf("ItemMetadataTableInfo() = %#v, want nil for field-only metadata", info)
	}
}

func TestItemMetadataGraphInfoReturnsClone(t *testing.T) {
	count := int64(3)
	metadata := &ItemMetadata{
		Kind: "graph",
		Graph: &datatype.GraphInfo{
			NodeShapes: []datatype.GraphNodeShapeInfo{{
				Name:  "Person",
				Count: &count,
			}},
		},
	}

	info := ItemMetadataGraphInfo(metadata)
	if info == nil || len(info.NodeShapes) != 1 || info.NodeShapes[0].Name != "Person" {
		t.Fatalf("ItemMetadataGraphInfo() = %#v", info)
	}
	info.NodeShapes[0].Name = "Changed"
	if metadata.Graph.NodeShapes[0].Name != "Person" {
		t.Fatalf("ItemMetadataGraphInfo returned mutable graph info")
	}
}
