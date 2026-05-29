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

func TestItemMetadataDocumentInfoReturnsClone(t *testing.T) {
	sizeBytes := int64(128)
	metadata := &ItemMetadata{
		Kind: "document",
		Document: &datatype.DocumentInfo{
			Title:     "Plan",
			SizeBytes: &sizeBytes,
		},
	}

	info := ItemMetadataDocumentInfo(metadata)
	if info == nil || info.Title != "Plan" || info.SizeBytes == nil || *info.SizeBytes != 128 {
		t.Fatalf("ItemMetadataDocumentInfo() = %#v", info)
	}
	info.Title = "Changed"
	*info.SizeBytes = 256
	if metadata.Document.Title != "Plan" || *metadata.Document.SizeBytes != 128 {
		t.Fatalf("ItemMetadataDocumentInfo returned mutable document info")
	}
}

func TestItemMetadataMediaInfoReturnsClone(t *testing.T) {
	durationMS := int64(1234)
	sizeBytes := int64(4096)
	metadata := &ItemMetadata{
		Kind: "media",
		Media: &datatype.MediaInfo{
			Kind:       datatype.MediaKindImage,
			Width:      800,
			Height:     600,
			DurationMS: &durationMS,
			SizeBytes:  &sizeBytes,
		},
	}

	info := ItemMetadataMediaInfo(metadata)
	if info == nil || info.Kind != datatype.MediaKindImage || info.Width != 800 || info.Height != 600 {
		t.Fatalf("ItemMetadataMediaInfo() = %#v", info)
	}
	*info.DurationMS = 5678
	*info.SizeBytes = 8192
	if *metadata.Media.DurationMS != 1234 || *metadata.Media.SizeBytes != 4096 {
		t.Fatalf("ItemMetadataMediaInfo returned mutable media info")
	}
}

func TestItemMetadataContainerInfoReturnsClone(t *testing.T) {
	rowCount := int64(9)
	columnCount := 2
	metadata := &ItemMetadata{
		Kind: "container",
		Container: &datatype.ContainerInfo{
			ChildCount:    1,
			ResourceCount: 1,
			Children: []datatype.ContainerChildInfo{{
				Name:        "Sheet1",
				ChildKind:   "sheet",
				DataType:    datatype.DataTypeTable,
				RowCount:    &rowCount,
				ColumnCount: &columnCount,
				Native:      map[string]interface{}{"sheet_index": 0},
			}},
		},
	}

	info := ItemMetadataContainerInfo(metadata)
	if info == nil || info.ChildCount != 1 || len(info.Children) != 1 || info.Children[0].Name != "Sheet1" {
		t.Fatalf("ItemMetadataContainerInfo() = %#v", info)
	}
	info.Children[0].Name = "Changed"
	*info.Children[0].RowCount = 10
	info.Children[0].Native["sheet_index"] = 1
	if metadata.Container.Children[0].Name != "Sheet1" || *metadata.Container.Children[0].RowCount != 9 {
		t.Fatalf("ItemMetadataContainerInfo returned mutable child")
	}
	if metadata.Container.Children[0].Native["sheet_index"] != 0 {
		t.Fatalf("ItemMetadataContainerInfo returned mutable native")
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
