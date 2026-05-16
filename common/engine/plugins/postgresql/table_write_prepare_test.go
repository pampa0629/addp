package postgresql

import (
	"strings"
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
	if err := plugin.ValidatePluginCapabilities(&PostgreSQLPlugin{}); err != nil {
		t.Fatalf("ValidatePluginCapabilities failed: %v", err)
	}
}

func TestPrepareTableWriteRejectsUnsupportedModeBeforeConnection(t *testing.T) {
	err := (&PostgreSQLPlugin{}).PrepareTableWrite(nil, nil, plugin.CatalogPath{}, plugin.TableWriteOptions{Mode: "overwrite"})
	if err == nil {
		t.Fatal("PrepareTableWrite succeeded, want unsupported mode error")
	}
	if !strings.Contains(err.Error(), "supported modes") {
		t.Fatalf("error = %q, want supported modes guidance", err)
	}
}

func TestPostgresSQLTypeForField(t *testing.T) {
	tests := []struct {
		name  string
		field plugin.FieldInfo
		want  string
	}{
		{name: "native type wins", field: plugin.FieldInfo{Name: "id", Type: "string", NativeType: "VARCHAR(32)"}, want: "VARCHAR(32)"},
		{name: "native common type maps", field: plugin.FieldInfo{Name: "name", Type: "string", NativeType: "string"}, want: "TEXT"},
		{name: "native inferred double maps", field: plugin.FieldInfo{Name: "score", Type: "double", NativeType: "double"}, want: "DOUBLE PRECISION"},
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
