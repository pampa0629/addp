package tidb

import (
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
	commonquery "github.com/addp/common/query"
)

func TestConnectionSpecUsesTiDBDefaults(t *testing.T) {
	p := &Plugin{}
	if got, want := p.DefaultPort(), 4000; got != want {
		t.Fatalf("DefaultPort() = %d, want %d", got, want)
	}
	if got, want := p.ConnectionIdentityFields(), []string{"host", "port", "database", "user"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ConnectionIdentityFields() = %#v, want %#v", got, want)
	}
	user, ok := p.ConnectionSpec().Field("user")
	if !ok || user.Default != "root" {
		t.Fatalf("user field = %#v, want default root", user)
	}
	password, ok := p.ConnectionSpec().Field("password")
	if !ok || password.Required || !password.Sensitive {
		t.Fatalf("password field = %#v, want optional sensitive field", password)
	}
	if err := p.ConnectionSpec().Validate(); err != nil {
		t.Fatalf("ConnectionSpec().Validate() error = %v", err)
	}
}

func TestBuildDSNUsesMySQLProtocolAndTiDBPort(t *testing.T) {
	p := &Plugin{}
	dsn, err := p.BuildDSN(plugin.ConnectionInfo{
		"host": "tidb.example", "database": "business", "user": "app", "password": "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"app:secret@tcp(tidb.example:4000)/business", "interpolateParams=true", "parseTime=true"} {
		if !strings.Contains(dsn, fragment) {
			t.Fatalf("BuildDSN() = %q, want fragment %q", dsn, fragment)
		}
	}
	if got := p.SQLDialect(); got != commonquery.DialectMySQL {
		t.Fatalf("SQLDialect() = %q, want %q", got, commonquery.DialectMySQL)
	}
}

func TestCatalogFactsBoundaryAndTypeMapping(t *testing.T) {
	if tidbCatalogFactsDialect.IncludeEngine {
		t.Fatal("TiDB must not expose compatibility storage-engine metadata as a native engine choice")
	}
	for _, schema := range []string{"information_schema", "INSPECTION_SCHEMA", "metrics_schema", "mysql", "PERFORMANCE_SCHEMA", "sys"} {
		if !tidbCatalogFactsDialect.IsSystemSchema(schema) {
			t.Fatalf("system schema %q is not filtered", schema)
		}
	}
	if tidbCatalogFactsDialect.IsSystemSchema("business") {
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
		if got := tidbCatalogFieldType(nativeType); got != want {
			t.Fatalf("tidbCatalogFieldType(%q) = %q, want %q", nativeType, got, want)
		}
	}
}

func TestCapabilitiesMatchImplementedProviders(t *testing.T) {
	p := &Plugin{}
	if got := p.tableWriter().UpsertValueReference; got != shared.MySQLCompatibleUpsertValueReferenceValuesFunction {
		t.Fatalf("TiDB upsert value reference = %q, want VALUES(column)", got)
	}
	caps := p.Capabilities()
	if !caps.Storage.Store.TableWritePrepare || !caps.Storage.Store.TableWriteSession || !caps.Storage.Store.Delete {
		t.Fatalf("TiDB must declare native table prepare/session/delete capabilities: %#v", caps.Storage.Store)
	}
	if caps.Storage.Store.TableUpsert == nil || !caps.Storage.Store.TableUpsert.Supported || !caps.Storage.Store.TableUpsert.Idempotent {
		t.Fatalf("TiDB must declare idempotent table upsert capability: %#v", caps.Storage.Store)
	}
	if !caps.Storage.Store.BoundedWatermarkRead {
		t.Fatalf("TiDB must declare bounded watermark read capability: %#v", caps.Storage.Store)
	}
	if caps.Storage.Store.BatchWrite || caps.Storage.Store.PartitionedTableChangeApply != nil || caps.Storage.Facts.SpatialFacts {
		t.Fatalf("TiDB capabilities overclaim unsupported providers: %#v", caps.Storage)
	}
	decimal := caps.Limits.TableWrite.Decimal
	if !decimal.RequiresExplicitPrecisionScale || decimal.MaxPrecision == nil || *decimal.MaxPrecision != 65 || decimal.MaxScale == nil || *decimal.MaxScale != 30 {
		t.Fatalf("TiDB capabilities have unexpected decimal write limits: %#v", decimal)
	}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatal(err)
	}
	if got := p.ControlledReadOnlySQLBoundary(); got != plugin.ControlledReadOnlySQLBoundaryValidatedStatement {
		t.Fatalf("TiDB controlled read-only boundary = %q, want validated statement", got)
	}
	if !p.SupportsParameterizedQueries() {
		t.Fatal("TiDB SQL runtime must declare parameter binding")
	}
	if got := caps.Compute.Query.IdentifierQuotes["sql"]; got != "`" {
		t.Fatalf("TiDB SQL identifier quote = %q, want backtick", got)
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
