package format

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestApplyFieldSelectionToTableDescribeResult(t *testing.T) {
	result := &datatype.TableDescribeResult{
		Table: &datatype.TableInfo{
			Fields: []datatype.FieldInfo{
				{Name: "id", Type: datatype.FieldTypeInt, PrimaryKey: true},
				{Name: "name", Type: datatype.FieldTypeString},
				{Name: "geom", Type: datatype.FieldTypeGeometry},
			},
			PrimaryKey: []string{"id"},
		},
		Spatial: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 2),
	}

	selected, err := ApplyFieldSelectionToTableDescribeResult(result, &FieldSelectionOptions{Include: []string{"name", "id"}})
	if err != nil {
		t.Fatalf("ApplyFieldSelectionToTableDescribeResult() error = %v", err)
	}
	if len(selected.Table.Fields) != 2 || selected.Table.Fields[0].Name != "name" || selected.Table.Fields[1].Name != "id" {
		t.Fatalf("fields = %#v, want name/id", selected.Table.Fields)
	}
	if len(selected.Table.PrimaryKey) != 1 || selected.Table.PrimaryKey[0] != "id" {
		t.Fatalf("primary key = %#v, want id", selected.Table.PrimaryKey)
	}
	if selected.Spatial != nil {
		t.Fatalf("spatial = %#v, want nil when geometry field is excluded", selected.Spatial)
	}
	if result.Spatial == nil || result.Spatial.PrimaryGeometryName() != "geom" {
		t.Fatalf("original spatial info changed: %#v", result.Spatial)
	}
}

func TestApplyFieldSelectionToTableDescribeResultMissingFieldPolicies(t *testing.T) {
	result := &datatype.TableDescribeResult{
		Table: &datatype.TableInfo{
			Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}},
		},
	}

	if _, err := ApplyFieldSelectionToTableDescribeResult(result, &FieldSelectionOptions{Include: []string{"missing"}}); err == nil {
		t.Fatalf("missing field with default policy should error")
	}
	selected, err := ApplyFieldSelectionToTableDescribeResult(result, &FieldSelectionOptions{
		Include:            []string{"missing"},
		MissingFieldPolicy: MissingFieldIgnore,
	})
	if err != nil {
		t.Fatalf("missing field with ignore policy error = %v", err)
	}
	if len(selected.Table.Fields) != 0 {
		t.Fatalf("fields = %#v, want empty selection", selected.Table.Fields)
	}
}
