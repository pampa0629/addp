package metaquery

import (
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestFieldsFromMetaItemReadsTypeInfoTableFields(t *testing.T) {
	t.Parallel()

	fields, err := FieldsFromMetaItem(models.MetaItem{
		Attributes: models.JSONMap{
			"fields": []interface{}{
				map[string]interface{}{"name": "legacy_id", "data_type": "text"},
			},
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{
					"fields": []interface{}{
						map[string]interface{}{"name": "id", "data_type": "integer", "is_primary_key": true},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("FieldsFromMetaItem() error = %v", err)
	}
	if len(fields) != 1 || fields[0].Name != "id" || fields[0].DataType != "integer" || !fields[0].IsPrimaryKey {
		t.Fatalf("fields = %#v, want partitioned id field", fields)
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
					"table_metadata": map[string]interface{}{
						"primary_key": []interface{}{"id"},
					},
					"fields": []interface{}{
						map[string]interface{}{"name": "id", "data_type": "integer", "is_primary_key": true},
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
