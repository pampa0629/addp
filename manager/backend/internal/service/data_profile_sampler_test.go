package service

import (
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/manager/internal/models"
)

func TestProfileSamplePagesSpreadsBudgetAcrossSource(t *testing.T) {
	tests := []struct {
		name     string
		total    int64
		pageSize int
		maxRows  int
		want     []int
	}{
		{name: "single page", total: 500, pageSize: 500, maxRows: 10000, want: nil},
		{name: "all pages fit", total: 1500, pageSize: 500, maxRows: 2000, want: []int{2, 3}},
		{name: "spread bounded pages", total: 10000, pageSize: 500, maxRows: 2000, want: []int{7, 13, 20}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := profileSamplePages(tt.total, tt.pageSize, tt.maxRows); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("profileSamplePages() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProfileTargetKeyNormalizesSelection(t *testing.T) {
	left := profileTargetKey(7, " addp://engine/1/item/a ", DataProfileSelection{
		ChildName: " Sheet 1 ", RefPath: "/data.csv/", NestedChildPath: "/nested/table/",
	})
	right := profileTargetKey(7, "addp://engine/1/item/a", DataProfileSelection{
		ChildName: "Sheet 1", RefPath: "data.csv", NestedChildPath: "nested/table",
	})
	if left != right {
		t.Fatalf("normalized target keys differ: %q != %q", left, right)
	}
}

func TestProfileFieldsFromPreviewUsesCanonicalGeometryField(t *testing.T) {
	table := &models.TablePreview{
		Columns: []string{"parcel_shape"},
		Fields: []datatype.FieldInfo{{
			Name:       "parcel_shape",
			Type:       datatype.FieldTypeGeometry,
			NativeType: "geometry",
			Nullable:   true,
		}},
		ColumnMetadata: []models.ColumnMetadata{{
			ColumnName: "parcel_shape",
			Type:       "GEOMETRY(Polygon, 32650)",
			IsNullable: true,
		}},
	}

	fields := profileFieldsFromPreview(table)
	if len(fields) != 1 {
		t.Fatalf("fields = %d, want 1", len(fields))
	}
	field := fields[0]
	if field.Name != "parcel_shape" || field.Type != datatype.FieldTypeGeometry || field.NativeType != "geometry" {
		t.Fatalf("unexpected canonical geometry field: %#v", field)
	}
}
