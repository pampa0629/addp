package gaussdb

import (
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/engine/plugin"
)

func TestGaussDBReadOnlySurface(t *testing.T) {
	p := &Plugin{}
	if p.Type() != "gaussdb" || p.DisplayName() != "GaussDB" || p.DefaultPort() != 5432 {
		t.Fatal("independent identity or native default port lost")
	}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatal(err)
	}
	caps := p.Capabilities()
	if caps.EngineType != "gaussdb" || !caps.Storage.Store.BatchRead || !caps.Storage.Store.TableReadSession || !caps.Compute.Query.ReadSession || (caps.Compute.Query.Parameters == nil || !caps.Compute.Query.Parameters.Supported) {
		t.Fatalf("read capabilities: %+v", caps)
	}
	if caps.Storage.Store.BatchWrite || caps.Storage.Store.TableWriteSession || caps.Storage.Facts.Indexes || caps.Storage.Facts.Constraints || caps.Storage.Facts.Partitioning || caps.Storage.Facts.SpatialFacts {
		t.Fatal("uncertified capabilities exposed")
	}
	for name, exposed := range map[string]bool{
		"write":     implements[plugin.BatchWritableProvider](p),
		"delete":    implements[plugin.ResourceDeleteProvider](p),
		"result":    implements[plugin.TableResultProvider](p),
		"watermark": implements[plugin.BoundedWatermarkReadProvider](p),
		"spatial":   implements[plugin.SpatialFeatureReadProvider](p),
	} {
		if exposed {
			t.Fatal("uncertified Provider promoted: " + name)
		}
	}
}
func implements[T any](p any) bool { _, ok := p.(T); return ok }

func TestGaussDBDriverCoexistence(t *testing.T) {
	if _, present := os.LookupEnv("GOENV_REGIST_ALL"); present {
		t.Fatal("GOENV_REGIST_ALL must not be required")
	}
	for name, want := range map[string]string{"gaussdb": "github.com/HuaweiCloudDeveloper/gaussdb-go/stdlib", "postgres": "github.com/lib/pq"} {
		db, err := sql.Open(name, "host=127.0.0.1 port=1 connect_timeout=1")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if got := reflect.TypeOf(db.Driver()).Elem().PkgPath(); got != want {
			t.Fatalf("driver %s owner %s", name, got)
		}
	}
}

func TestGaussDBConnectionSpec(t *testing.T) {
	p := &Plugin{}
	info := plugin.ConnectionInfo{"host": "127.0.0.1", "database": "business", "user": "reader", "password": "Aa9@space ' \\ test"}
	if err := p.ValidateConnectionInfo(info); err != nil {
		t.Fatal(err)
	}
	dsn, err := p.BuildDSN(info)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dsn, "sslmode=disable") || !strings.Contains(dsn, "port=5432") {
		t.Fatal("native DSN defaults differ")
	}
	if _, exists := info["sslmode"]; exists {
		t.Fatal("BuildDSN mutated input")
	}
	delete(info, "password")
	if err := p.ValidateConnectionInfo(info); err == nil {
		t.Fatal("missing password accepted")
	}
	if !reflect.DeepEqual(p.SensitiveFields(), []string{"password"}) {
		t.Fatal("password sensitivity lost")
	}
}
func TestGaussDBRejectsWriteBeforeConnecting(t *testing.T) {
	p := &Plugin{}
	if _, err := p.PrepareQuery(t.Context(), nil, plugin.QueryRequest{Query: "DELETE FROM source"}); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err := p.ExecuteSQL(t.Context(), nil, "DELETE FROM source", plugin.QueryOptions{}); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal(err)
	}
}
func TestGaussDBCertifiedKernelIdentity(t *testing.T) {
	for _, v := range []string{"gaussdb (GaussDB Kernel 507.0.T1 build abb74b87) compiled at 2026-09-15", "gaussdb (GaussDB Kernel 507.0.T1 build c522022b)"} {
		if !certifiedVersion.MatchString(v) {
			t.Fatal("admitted kernel rejected: " + v)
		}
	}
	for _, v := range []string{"PostgreSQL 15.1", "openGauss 7.0", "gaussdb (GaussDB Kernel 507.0.T10 build abb74b87)", "fake gaussdb (GaussDB Kernel 507.0.T1 build abb74b87)", "gaussdb (GaussDB Kernel 508.0.T1 build abb74b87)"} {
		if certifiedVersion.MatchString(v) {
			t.Fatal("unadmitted kernel accepted: " + v)
		}
	}
}
