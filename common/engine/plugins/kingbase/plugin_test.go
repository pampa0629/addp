package kingbase

import (
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
)

func TestPluginIdentityAndConnectionSpec(t *testing.T) {
	p := &Plugin{}
	if p.Type() != "kingbase" || p.DisplayName() != "KingbaseES" || p.EngineOrigin() != "general" {
		t.Fatalf("unexpected identity: %s %s %s", p.Type(), p.DisplayName(), p.EngineOrigin())
	}
	if p.DefaultPort() != 54321 {
		t.Fatalf("DefaultPort() = %d, want 54321", p.DefaultPort())
	}
	if !reflect.DeepEqual(p.RequiredFields(), []string{"host", "database", "user", "password"}) {
		t.Fatalf("RequiredFields() = %#v", p.RequiredFields())
	}
	if !reflect.DeepEqual(p.SensitiveFields(), []string{"password"}) {
		t.Fatalf("SensitiveFields() = %#v", p.SensitiveFields())
	}
	if !reflect.DeepEqual(p.ConnectionIdentityFields(), []string{"host", "port", "database"}) {
		t.Fatalf("ConnectionIdentityFields() = %#v", p.ConnectionIdentityFields())
	}
}

func TestPluginCapabilitiesStayNonSpatial(t *testing.T) {
	p := &Plugin{}
	capabilities := p.Capabilities()
	if p.SQLDialect() != commonquery.DialectPostgreSQL {
		t.Fatalf("SQLDialect() = %q", p.SQLDialect())
	}
	if capabilities.Storage == nil || capabilities.Storage.Store == nil {
		t.Fatalf("storage capabilities are incomplete: %#v", capabilities.Storage)
	}
	store := capabilities.Storage.Store
	if !store.BatchRead || !store.TableReadSession || !store.BoundedWatermarkRead || !store.TableWriteSession || !store.TableWritePrepare || store.TableUpsert == nil || !store.TableUpsert.Supported || !store.TableUpsert.Idempotent || !store.Delete {
		t.Fatalf("unexpected store capabilities: %#v", store)
	}
	if store.TableSpatialEncoding != nil || capabilities.Storage.Facts == nil || capabilities.Storage.Facts.SpatialFacts {
		t.Fatalf("KingbaseES must not declare spatial capabilities: %#v", capabilities.Storage)
	}
	if capabilities.Compute == nil || capabilities.Compute.Query == nil || !capabilities.Compute.Query.Parameters.Supported {
		t.Fatalf("unexpected query capabilities: %#v", capabilities.Compute)
	}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatal(err)
	}
}

func TestBuildDSNUsesKingbaseDefaults(t *testing.T) {
	dsn, err := (&Plugin{}).BuildDSN(plugin.ConnectionInfo{
		"host": "127.0.0.1", "database": "business", "user": "system", "password": "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"host=127.0.0.1", "port=54321", "dbname=business", "user=system", "sslmode=disable"} {
		if !strings.Contains(dsn, fragment) {
			t.Fatalf("BuildDSN() = %q, want %q", dsn, fragment)
		}
	}
}

func TestKingbaseSystemSchemasAreDeclared(t *testing.T) {
	identity := (&Plugin{}).protocolIdentity()
	for _, schema := range []string{"anon", "dbms_job", "kdb_schedule", "sys_catalog", "sysaudit", "xlog_record_read"} {
		if !contains(identity.AdditionalSystemSchemas, schema) {
			t.Fatalf("system schema %q missing from %#v", schema, identity.AdditionalSystemSchemas)
		}
	}
}

func TestKingbaseRejectsSpatialWrites(t *testing.T) {
	spatialInfo := datatype.NewSingleGeometrySpatialInfo("shape", "Point", 4326, 0)
	if err := validateNonSpatialWrite(nil, spatialInfo); err == nil || !strings.Contains(err.Error(), "does not support spatial") {
		t.Fatalf("spatial info error = %v", err)
	}
	if err := validateNonSpatialWrite([]datatype.FieldInfo{{Name: "shape", Type: datatype.FieldTypeGeometry}}, nil); err == nil || !strings.Contains(err.Error(), "does not support spatial") {
		t.Fatalf("spatial field error = %v", err)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
