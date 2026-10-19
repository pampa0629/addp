package jsonformat

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestNullableGeometryDescribeAndStreaming(t *testing.T) {
	point := `{"id":"kept","shape":"0101000000000000000000F03F0000000000000040"}`
	empty := `{"id":"kept","shape":null}`
	missing := `{"id":"kept"}`
	geometryField := "shape"
	for _, tc := range []struct {
		name, records         string
		hasGeometry, nullable bool
	}{
		{"point-null", point + "," + empty, true, true},
		{"null-point", empty + "," + point, true, true},
		{"point-point", point + "," + point, true, false},
		{"null-null", empty + "," + empty, false, false},
		{"point-missing", point + "," + missing, true, true},
		{"missing-point", missing + "," + point, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := `[` + tc.records + `]`
			plugin := NewPlugin(nil)
			info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), nil)
			if err != nil {
				t.Fatal(err)
			}
			if info.Table.RowCount == nil || *info.Table.RowCount != 2 {
				t.Fatalf("row count = %v, want 2", info.Table.RowCount)
			}
			check := func(fields []datatype.FieldInfo) {
				t.Helper()
				count := 0
				for _, field := range fields {
					if field.Name != geometryField {
						continue
					}
					count++
					if tc.hasGeometry && (field.Type != datatype.FieldTypeGeometry || field.Nullable != tc.nullable) {
						t.Errorf("geometry field = %#v, want geometry nullable=%v", field, tc.nullable)
					}
					if !tc.hasGeometry && field.Type == datatype.FieldTypeGeometry {
						t.Errorf("invented geometry field: %#v", field)
					}
				}
				if count > 1 || (tc.hasGeometry && count != 1) {
					t.Errorf("geometry field count = %d: %#v", count, fields)
				}
			}
			check(info.Table.Fields)
			if (info.Spatial != nil) != tc.hasGeometry {
				t.Fatalf("unexpected spatial info: %#v", info.Spatial)
			}
			for _, batchSize := range []int{1, 2} {
				reader, err := plugin.OpenTableReader(context.Background(), strings.NewReader(data), nil)
				if err != nil {
					t.Fatal(err)
				}
				total, geometries := 0, 0
				for {
					rows, err := reader.ReadRows(context.Background(), batchSize)
					if err != nil {
						t.Fatal(err)
					}
					if len(rows) == 0 {
						break
					}
					total += len(rows)
					for _, row := range rows {
						if row[geometryField] != nil {
							geometries++
						}
						if row["id"] != "kept" {
							t.Fatalf("record lost: %#v", row)
						}
					}
				}
				if total != 2 {
					t.Errorf("batch size %d read %d records", batchSize, total)
				}
				wantGeometries := 0
				if tc.hasGeometry {
					wantGeometries = 1
				}
				if tc.name == "point-point" {
					wantGeometries = 2
				}
				if geometries != wantGeometries {
					t.Errorf("geometry count = %d, want %d", geometries, wantGeometries)
				}
				check(reader.Fields())
				if (reader.(format.TableSpatialInfoProvider).SpatialInfo() != nil) != tc.hasGeometry {
					t.Errorf("unexpected reader spatial info: %#v", reader.(format.TableSpatialInfoProvider).SpatialInfo())
				}
				if err := reader.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
