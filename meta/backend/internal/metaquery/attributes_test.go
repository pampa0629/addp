package metaquery

import (
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestFieldsFromMetaItemReadsTypeInfoTableFields(t *testing.T) {
	t.Parallel()

	fields, err := FieldsFromMetaItem(models.MetaItem{
		Attributes: models.JSONMap{
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{
					"fields": []interface{}{
						map[string]interface{}{"name": "id", "type": "int", "primary_key": true, "nullable": false},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("FieldsFromMetaItem() error = %v", err)
	}
	if len(fields) != 1 || fields[0].Name != "id" || fields[0].Type != "int" || !fields[0].PrimaryKey {
		t.Fatalf("fields = %#v, want partitioned id field", fields)
	}
}

func TestFieldsFromMetaItemDoesNotMergeCapabilitiesSpatial(t *testing.T) {
	t.Parallel()

	fields, err := FieldsFromMetaItem(models.MetaItem{
		Attributes: models.JSONMap{
			"capabilities": map[string]interface{}{
				"spatial": map[string]interface{}{
					"geometry_columns": []interface{}{
						map[string]interface{}{
							"name":          "SmGeometry",
							"geometry_type": "Polygon",
							"srid":          float64(2360),
						},
					},
					"primary_geometry_column": "SmGeometry",
				},
			},
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{
					"fields": []interface{}{
						map[string]interface{}{"name": "id", "type": "int"},
						map[string]interface{}{"name": "SmGeometry", "type": "geometry"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("FieldsFromMetaItem() error = %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("fields = %#v, want 2 fields", fields)
	}
	geom := fields[1]
	if geom.Name != "SmGeometry" || geom.Type != "geometry" {
		t.Fatalf("geometry field = %#v, want type_info field only", geom)
	}
}

func TestSpatialMetadataFromItemReadsCapabilitiesSpatial(t *testing.T) {
	t.Parallel()

	meta, err := SpatialMetadataFromItem(models.MetaItem{
		Attributes: models.JSONMap{
			"capabilities": map[string]interface{}{
				"spatial": map[string]interface{}{
					"geometry_columns": []interface{}{
						map[string]interface{}{
							"name":          "shape",
							"srid":          float64(4326),
							"crs_ref":       "EPSG:4326",
							"geometry_type": "POLYGON",
						},
					},
					"crs_definitions": []interface{}{
						map[string]interface{}{
							"id":                  "EPSG:4326",
							"definition_encoding": "wkt",
							"definition":          "GEOGCS[...]",
							"source":              "postgis_spatial_ref_sys",
						},
					},
					"primary_geometry_column": "shape",
				},
			},
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{
					"primary_key": []interface{}{"id"},
					"fields": []interface{}{
						map[string]interface{}{"name": "id", "type": "int", "primary_key": true},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("SpatialMetadataFromItem() error = %v", err)
	}
	if meta.GeometryColumn != "shape" || meta.SRID != 4326 || meta.PrimaryKey != "id" {
		t.Fatalf("spatial metadata = %#v, want partitioned spatial metadata", meta)
	}
	if meta.CRSRef != "EPSG:4326" || meta.CRSDefinition == nil || meta.CRSDefinition.DefinitionEncoding != "wkt" {
		t.Fatalf("crs metadata = %q/%#v, want EPSG:4326 wkt", meta.CRSRef, meta.CRSDefinition)
	}
	if len(meta.GeometryTypes) != 1 || meta.GeometryTypes[0] != "POLYGON" {
		t.Fatalf("geometry types = %#v, want POLYGON", meta.GeometryTypes)
	}
	if len(meta.Fields) != 1 || meta.Fields[0].Name != "id" || !meta.Fields[0].PrimaryKey {
		t.Fatalf("fields = %#v, want partitioned fields", meta.Fields)
	}
}

func TestSpatialMetadataFromItemReadsObjectSpatialReference(t *testing.T) {
	t.Parallel()

	meta, err := SpatialMetadataFromItem(models.MetaItem{
		Attributes: models.JSONMap{
			"capabilities": map[string]interface{}{
				"spatial": map[string]interface{}{
					"srid":   float64(4326),
					"extent": []interface{}{120.0, 30.0, 121.0, 31.0},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("SpatialMetadataFromItem() error = %v", err)
	}
	if meta.SRID != 4326 {
		t.Fatalf("srid = %d, want 4326", meta.SRID)
	}
	if meta.GeometryColumn != "" {
		t.Fatalf("non-table spatial should not invent geometry column: %#v", meta)
	}
	if len(meta.Extent) != 4 || meta.Extent[0] != 120 || meta.Extent[3] != 31 {
		t.Fatalf("extent = %#v", meta.Extent)
	}
}
