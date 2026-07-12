package scanruntime

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestMergeDatabaseTableInfoPreservesListFactsAndNormalizesFields(t *testing.T) {
	rowCount := int64(12)
	sizeBytes := int64(2048)
	base := datatype.TableInfo{
		Name:      "orders",
		Kind:      "view",
		Comment:   "from list",
		RowCount:  &rowCount,
		SizeBytes: &sizeBytes,
		Native: map[string]interface{}{
			"engine": "MergeTree",
		},
	}
	described := datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true},
			{Name: "name", NativeType: "varchar"},
		},
	}

	merged := mergeDatabaseTableInfo(base, described)

	if merged.Name != "orders" || merged.Kind != "view" || merged.Comment != "from list" {
		t.Fatalf("merged identity = %#v", merged)
	}
	if merged.RowCount == nil || *merged.RowCount != rowCount {
		t.Fatalf("RowCount = %#v, want %d", merged.RowCount, rowCount)
	}
	if merged.SizeBytes == nil || *merged.SizeBytes != sizeBytes {
		t.Fatalf("SizeBytes = %#v, want %d", merged.SizeBytes, sizeBytes)
	}
	if merged.Native["engine"] != "MergeTree" {
		t.Fatalf("Native = %#v", merged.Native)
	}
	if len(merged.Fields) != 2 ||
		merged.Fields[0].NativeType != "bigint" ||
		merged.Fields[0].Type != datatype.FieldTypeBigInt ||
		merged.Fields[1].Type != datatype.FieldTypeString {
		t.Fatalf("Fields = %#v", merged.Fields)
	}
	if len(merged.PrimaryKey) != 1 || merged.PrimaryKey[0] != "id" {
		t.Fatalf("PrimaryKey = %#v", merged.PrimaryKey)
	}
}

func TestMergeDatabaseTableInfoKeepsResolvedRowCountOverDescribedEstimate(t *testing.T) {
	resolvedRowCount := int64(50)
	describedEstimate := int64(0)
	base := datatype.TableInfo{
		Name:     "sm_buffer_demo",
		Kind:     "table",
		RowCount: &resolvedRowCount,
	}
	described := datatype.TableInfo{
		Name:     "sm_buffer_demo",
		Kind:     "table",
		RowCount: &describedEstimate,
		Fields: []datatype.FieldInfo{
			{Name: "smgeometry", NativeType: "geometry"},
		},
	}

	merged := mergeDatabaseTableInfo(base, described)

	if merged.RowCount == nil || *merged.RowCount != resolvedRowCount {
		t.Fatalf("RowCount = %#v, want resolved %d", merged.RowCount, resolvedRowCount)
	}
}
