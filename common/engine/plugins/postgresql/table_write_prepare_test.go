package postgresql

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestPostgreSQLCapabilitiesDeclareTableWritePrepare(t *testing.T) {
	caps := (&PostgreSQLPlugin{}).Capabilities()
	if caps.Storage == nil || caps.Storage.Store == nil || !caps.Storage.Store.TableWritePrepare {
		t.Fatalf("postgresql capabilities do not declare table_write_prepare: %#v", caps.Storage)
	}
	if !caps.Storage.Store.TableReadSession {
		t.Fatalf("postgresql capabilities do not declare table_read_session: %#v", caps.Storage.Store)
	}
	if !caps.Storage.Store.TableWriteSession {
		t.Fatalf("postgresql capabilities do not declare table_write_session: %#v", caps.Storage.Store)
	}
	if !caps.Storage.Store.Delete {
		t.Fatalf("postgresql capabilities do not declare delete: %#v", caps.Storage.Store)
	}
	if err := plugin.ValidatePluginCapabilities(&PostgreSQLPlugin{}); err != nil {
		t.Fatalf("ValidatePluginCapabilities failed: %v", err)
	}
}

func TestPostgresSQLTypeForField(t *testing.T) {
	tests := []struct {
		name        string
		field       datatype.FieldInfo
		spatialInfo *datatype.SpatialInfo
		want        string
	}{
		{name: "spatial info geometry type and srid", field: datatype.FieldInfo{Name: "geom", Type: datatype.FieldTypeGeometry}, spatialInfo: datatype.NewSingleGeometrySpatialInfo("geom", "MultiPolygon", 4326, 0), want: "GEOMETRY(MultiPolygon,4326)"},
		{name: "spatial info dimension z", field: datatype.FieldInfo{Name: "geom", Type: datatype.FieldTypeGeometry}, spatialInfo: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 3), want: "GEOMETRY(PointZ,4326)"},
		{name: "common int", field: datatype.FieldInfo{Name: "id", Type: "int"}, want: "INTEGER"},
		{name: "unknown defaults text", field: datatype.FieldInfo{Name: "x", Type: "unknown"}, want: "TEXT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := postgresSQLTypeForField(tt.field, tt.spatialInfo); got != tt.want {
				t.Fatalf("postgresSQLTypeForField() = %q, want %q", got, tt.want)
			}
		})
	}
}
