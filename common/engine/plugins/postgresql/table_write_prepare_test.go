package postgresql

import (
	"testing"

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
		name  string
		field plugin.FieldInfo
		want  string
	}{
		{name: "standard spatial attributes map", field: plugin.FieldInfo{Name: "geom", Type: "geometry", Attributes: map[string]interface{}{"geometry_type": "MultiPolygon", "srid": 4326}}, want: "GEOMETRY(MultiPolygon,4326)"},
		{name: "standard spatial dimension z", field: plugin.FieldInfo{Name: "geom", Type: "geometry", Attributes: map[string]interface{}{"geometry_type": "Point", "srid": 4326, "dimension": 3}}, want: "GEOMETRY(PointZ,4326)"},
		{name: "common int", field: plugin.FieldInfo{Name: "id", Type: "int"}, want: "INTEGER"},
		{name: "unknown defaults text", field: plugin.FieldInfo{Name: "x", Type: "unknown"}, want: "TEXT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := postgresSQLTypeForField(tt.field); got != tt.want {
				t.Fatalf("postgresSQLTypeForField() = %q, want %q", got, tt.want)
			}
		})
	}
}
