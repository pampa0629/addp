package oceanbase

import (
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
)

func TestConnectionSpecUsesTenantQualifiedUserAsIdentity(t *testing.T) {
	p := &Plugin{}
	if got, want := p.DefaultPort(), 2881; got != want {
		t.Fatalf("DefaultPort() = %d, want %d", got, want)
	}
	if got, want := p.ConnectionIdentityFields(), []string{"host", "port", "database", "user"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ConnectionIdentityFields() = %#v, want %#v", got, want)
	}
	user, ok := p.ConnectionSpec().Field("user")
	if !ok || user.Default != "root@test" {
		t.Fatalf("user field = %#v, want default root@test", user)
	}
	if err := p.ConnectionSpec().Validate(); err != nil {
		t.Fatalf("ConnectionSpec().Validate() error = %v", err)
	}
}

func TestBuildDSNPreservesOceanBaseAccountAndDialect(t *testing.T) {
	p := &Plugin{}
	dsn, err := p.BuildDSN(plugin.ConnectionInfo{
		"host": "oceanbase.example", "port": 2881, "database": "business",
		"user": "app@tenant_a", "password": "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"app@tenant_a:secret@tcp(oceanbase.example:2881)/business", "interpolateParams=true", "parseTime=true"} {
		if !strings.Contains(dsn, fragment) {
			t.Fatalf("BuildDSN() = %q, want fragment %q", dsn, fragment)
		}
	}
	if got := p.SQLDialect(); got != commonquery.DialectMySQL {
		t.Fatalf("SQLDialect() = %q, want %q", got, commonquery.DialectMySQL)
	}
}

func TestCatalogFactsBoundaryAndTypeMapping(t *testing.T) {
	if oceanBaseCatalogFactsDialect.IncludeEngine {
		t.Fatal("OceanBase must not expose MySQL table engine metadata")
	}
	for _, schema := range []string{"information_schema", "mysql", "oceanbase"} {
		if !oceanBaseCatalogFactsDialect.IsSystemSchema(schema) {
			t.Fatalf("system schema %q is not filtered", schema)
		}
	}
	if oceanBaseCatalogFactsDialect.IsSystemSchema("business") {
		t.Fatal("user database business must not be filtered")
	}
	tests := map[string]datatype.FieldType{
		"tinyint(1)":    datatype.FieldTypeBool,
		"bigint":        datatype.FieldTypeBigInt,
		"decimal(18,2)": datatype.FieldTypeDecimal,
		"varchar(255)":  datatype.FieldTypeString,
		"json":          datatype.FieldTypeJSON,
	}
	for nativeType, want := range tests {
		if got := oceanBaseCatalogFieldType(nativeType); got != want {
			t.Fatalf("oceanBaseCatalogFieldType(%q) = %q, want %q", nativeType, got, want)
		}
	}
}

func TestCapabilitiesMatchImplementedProviders(t *testing.T) {
	p := &Plugin{}
	caps := p.Capabilities()
	if !caps.Storage.Store.TableWritePrepare || !caps.Storage.Store.TableWriteSession {
		t.Fatalf("OceanBase must declare native table prepare/session write capabilities: %#v", caps.Storage.Store)
	}
	if !caps.Storage.Store.Delete {
		t.Fatalf("OceanBase replace writes require resource delete capability: %#v", caps.Storage.Store)
	}
	if caps.Storage.Store.TableUpsert == nil || !caps.Storage.Store.TableUpsert.Supported || !caps.Storage.Store.TableUpsert.Idempotent {
		t.Fatalf("OceanBase must declare idempotent table upsert capability: %#v", caps.Storage.Store)
	}
	if caps.Storage.Store.BatchWrite || caps.Storage.Facts.SpatialFacts {
		t.Fatalf("OceanBase capabilities overclaim unsupported providers: %#v", caps.Storage)
	}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatal(err)
	}
	if !p.SupportsControlledReadOnlySQL() || !p.SupportsParameterizedQueries() {
		t.Fatal("OceanBase SQL runtime must declare controlled read-only and parameter binding")
	}
	if got := caps.Compute.Query.IdentifierQuotes["sql"]; got != "`" {
		t.Fatalf("OceanBase SQL identifier quote = %q, want backtick", got)
	}
}

func TestGenerateSampleQueryRequiresCurrentCatalogFields(t *testing.T) {
	p := &Plugin{}
	path := plugin.TabularItemPath(7, plugin.EngineCatalogTermDatabase, "analytics", "orders")
	query, language := p.GenerateSampleQuery(nil, nil, plugin.SampleQueryOptions{Path: path})
	if language != "sql" || query != "" {
		t.Fatalf("GenerateSampleQuery() = %q, %q", query, language)
	}
}

func TestTableWriteProviderRejectsSpatialFields(t *testing.T) {
	p := &Plugin{}
	err := p.PrepareTableWrite(nil, nil, plugin.EngineCatalogPath{}, plugin.TableWriteOptions{
		Fields: []datatype.FieldInfo{{Name: "shape", Type: datatype.FieldTypeGeometry}},
	})
	if err == nil || !strings.Contains(err.Error(), "does not support spatial fields") {
		t.Fatalf("PrepareTableWrite() spatial error = %v", err)
	}
}
