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
						map[string]interface{}{"name": "id", "type": "integer", "primary_key": true, "nullable": false},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("FieldsFromMetaItem() error = %v", err)
	}
	if len(fields) != 1 || fields[0].Name != "id" || fields[0].Type != "integer" || !fields[0].IsPrimaryKey {
		t.Fatalf("fields = %#v, want partitioned id field", fields)
	}
}

func TestFieldsFromMetaItemMergesCapabilitiesSpatial(t *testing.T) {
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
	if geom.Name != "SmGeometry" || geom.Type != "geometry" || !geom.IsSpatial || geom.GeometryType != "Polygon" || geom.SRID != 2360 {
		t.Fatalf("geometry field = %#v, want spatial SmGeometry Polygon SRID 2360", geom)
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
							"geometry_type": "POLYGON",
						},
					},
					"primary_geometry_column": "shape",
				},
			},
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{
					"primary_key": []interface{}{"id"},
					"fields": []interface{}{
						map[string]interface{}{"name": "id", "type": "integer", "primary_key": true},
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
	if len(meta.Fields) != 1 || meta.Fields[0].Name != "id" {
		t.Fatalf("fields = %#v, want partitioned fields", meta.Fields)
	}
}
