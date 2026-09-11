package opengauss

import (
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
)

func TestProtocolIdentityDeclaresOpenGaussSystemSchemas(t *testing.T) {
	t.Parallel()

	identity := (&Plugin{}).protocolIdentity()
	if !reflect.DeepEqual(identity.AdditionalSystemSchemas, openGaussSystemSchemas) {
		t.Fatalf("additional system schemas = %#v, want %#v", identity.AdditionalSystemSchemas, openGaussSystemSchemas)
	}

	for _, schema := range []string{"coverage", "dbe_perf"} {
		found := false
		for _, declared := range identity.AdditionalSystemSchemas {
			if declared == schema {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("openGauss system schema %q must be excluded from the catalog", schema)
		}
	}
}

func TestConnectionSpecUsesOpenGaussDefaults(t *testing.T) {
	p := &Plugin{}
	if got, want := p.DefaultPort(), 5432; got != want {
		t.Fatalf("DefaultPort() = %d, want %d", got, want)
	}
	if got, want := p.ConnectionIdentityFields(), []string{"host", "port", "database"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ConnectionIdentityFields() = %#v, want %#v", got, want)
	}
	user, ok := p.ConnectionSpec().Field("user")
	if !ok || user.Default != "gaussdb" {
		t.Fatalf("user field = %#v, want default gaussdb", user)
	}
	password, ok := p.ConnectionSpec().Field("password")
	if !ok || !password.Required || !password.Sensitive {
		t.Fatalf("password field = %#v, want required sensitive credential", password)
	}
	if err := p.ConnectionSpec().Validate(); err != nil {
		t.Fatalf("ConnectionSpec().Validate() error = %v", err)
	}
}

func TestBuildDSNUsesPostgreSQLWireProtocol(t *testing.T) {
	p := &Plugin{}
	dsn, err := p.BuildDSN(plugin.ConnectionInfo{
		"host": "opengauss.example", "port": 5432, "database": "business",
		"user": "gaussdb", "password": "secret", "sslmode": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"host=opengauss.example", "port=5432", "dbname=business", "user=gaussdb", "password=secret", "sslmode=disable"} {
		if !strings.Contains(dsn, fragment) {
			t.Fatalf("BuildDSN() = %q, want fragment %q", dsn, fragment)
		}
	}
	if got := p.SQLDialect(); got != commonquery.DialectPostgreSQL {
		t.Fatalf("SQLDialect() = %q, want %q", got, commonquery.DialectPostgreSQL)
	}
	if got := p.Capabilities().Compute.Query.Parameters.Types; !plugin.Contains(got, "relation") {
		t.Fatalf("query parameter types = %v, want relation", got)
	}
}

func TestCapabilitiesExposeOnlyCertifiedOpenGaussProviders(t *testing.T) {
	p := &Plugin{}
	caps := p.Capabilities()
	if caps.EngineType != "opengauss" || p.Type() != "opengauss" || p.DisplayName() != "openGauss" {
		t.Fatalf("openGauss identity mismatch: type=%q display=%q caps=%q", p.Type(), p.DisplayName(), caps.EngineType)
	}
	if caps.Storage == nil || caps.Storage.Store == nil || caps.Storage.Facts == nil {
		t.Fatalf("openGauss storage capabilities are incomplete: %#v", caps.Storage)
	}
	store := caps.Storage.Store
	if !store.BatchRead || !store.TableReadSession || !store.TableWritePrepare || !store.TableWriteSession || !store.BoundedWatermarkRead || !store.Delete {
		t.Fatalf("openGauss certified table capabilities are incomplete: %#v", store)
	}
	if store.TableUpsert == nil || !store.TableUpsert.Supported || !store.TableUpsert.Idempotent {
		t.Fatalf("openGauss must declare idempotent table upsert: %#v", store.TableUpsert)
	}
	if store.BatchWrite || store.TableSpatialEncoding != nil || store.TableReadSpatialTransform || store.PartitionedTableChangeApply != nil || caps.Storage.Facts.SpatialFacts {
		t.Fatalf("openGauss overclaims uncertified spatial/batch/CDC providers: %#v", caps.Storage)
	}
	if caps.Compute == nil || caps.Compute.Query == nil || !caps.Compute.Query.ReadSession || !caps.Compute.Query.SupportsExplain || !caps.Compute.Query.SupportsCancel {
		t.Fatalf("openGauss query capabilities are incomplete: %#v", caps.Compute)
	}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatal(err)
	}
	if got := p.ControlledReadOnlySQLBoundary(); got != plugin.ControlledReadOnlySQLBoundaryDatabaseTransaction {
		t.Fatalf("openGauss controlled read-only boundary = %q, want database transaction", got)
	}
	if !p.SupportsParameterizedQueries() {
		t.Fatal("openGauss must declare parameter binding")
	}
}

func TestPreparedQueryKeepsOpenGaussProviderIdentity(t *testing.T) {
	p := &Plugin{}
	prepared, err := p.PrepareQuery(t.Context(), plugin.ConnectionInfo{}, plugin.QueryRequest{
		Language: "sql",
		Query:    `SELECT * FROM public.items WHERE city = $1`,
		Options: plugin.QueryOptions{
			ReadOnly: true,
			Args:     []interface{}{"长沙市"},
		},
	})
	if err != nil {
		t.Fatalf("PrepareQuery() error = %v", err)
	}
	if _, _, err := plugin.ConsumeSQLPreparedQuery(prepared, p); err != nil {
		t.Fatalf("prepared query is not owned by openGauss provider: %v", err)
	}
}

func TestWriteProvidersRejectUnadvertisedSpatialDataBeforeConnecting(t *testing.T) {
	p := &Plugin{}
	fields := []datatype.FieldInfo{{Name: "shape", Type: datatype.FieldTypeGeometry}}
	spatialInfo := datatype.NewSingleGeometrySpatialInfo("shape", "Point", 4326, 0)
	assertSpatialError := func(name string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "does not support spatial fields") {
			t.Fatalf("%s spatial error = %v", name, err)
		}
	}

	assertSpatialError("PrepareTableWrite", p.PrepareTableWrite(
		t.Context(), nil, plugin.EngineCatalogPath{}, plugin.TableWriteOptions{Fields: fields},
	))
	_, err := p.OpenTableWriteSession(
		t.Context(), nil, plugin.EngineCatalogPath{}, plugin.TableWriteSessionOptions{SpatialInfo: spatialInfo},
	)
	assertSpatialError("OpenTableWriteSession", err)
	assertSpatialError("PrepareTableUpsert", p.PrepareTableUpsert(
		t.Context(), nil, plugin.EngineCatalogPath{}, plugin.TableUpsertOptions{SpatialInfo: spatialInfo},
	))
	assertSpatialError("UpsertBatch", p.UpsertBatch(
		t.Context(), nil, plugin.EngineCatalogPath{},
		&plugin.BatchData{Fields: fields}, plugin.TableUpsertOptions{},
	))
}
