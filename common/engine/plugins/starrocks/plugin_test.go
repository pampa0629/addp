package starrocks

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/engine/plugin"
	mysql "github.com/go-sql-driver/mysql"
)

func TestReadOnlySurfaceAndConnectionIdentity(t *testing.T) {
	p := &Plugin{}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatal(err)
	}
	caps := p.Capabilities()
	if p.Type() != "starrocks" || p.DefaultPort() != 9030 || !caps.Compute.Query.ReadOnly || !caps.Storage.Store.BatchRead || !caps.Compute.Query.Parameters.Supported {
		t.Fatal("independent read-only identity lost")
	}
	if caps.Storage.Store.TableReadSession || caps.Compute.Query.ReadSession || caps.Compute.Query.SupportsCancel || caps.Storage.Store.BatchWrite || caps.Storage.Facts.Constraints || caps.Storage.Facts.SpatialFacts {
		t.Fatal("uncertified capability exposed")
	}
	if !reflect.DeepEqual(caps.Storage.Catalog.NodeKinds, []string{"namespace", "table"}) {
		t.Fatal("uncertified catalog kind exposed")
	}
	info := plugin.ConnectionInfo{"host": "::1", "database": "中文库", "user": "reader", "password": "s@fe:/' value"}
	dsn, err := p.BuildDSN(info)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil || parsed.Addr != "[::1]:9030" || parsed.Passwd != info["password"] || parsed.DBName != "中文库" || parsed.InterpolateParams || parsed.MultiStatements {
		t.Fatal("DSN lost identity or enabled unsafe execution")
	}
	delete(info, "password")
	if _, err := p.BuildDSN(info); err == nil {
		t.Fatal("password must be explicit")
	}
	if _, err := p.PrepareQuery(t.Context(), nil, plugin.QueryRequest{Query: "DELETE FROM source"}); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err := p.ExecuteSQL(t.Context(), nil, "DELETE FROM source", plugin.QueryOptions{}); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal(err)
	}
	for _, statement := range []string{"DELETE FROM source", "SELECT * FROM source INTO OUTFILE '/tmp/x'", "SELECT * FROM source; DELETE FROM source"} {
		if _, err := p.ExecuteSQL(t.Context(), nil, statement, plugin.QueryOptions{ReadOnly: true}); err == nil {
			t.Fatal("side effect accepted")
		}
	}
}
func TestPrimaryKeyComesFromNativeDDL(t *testing.T) {
	for _, target := range []struct {
		ddl  string
		keys []string
	}{
		{"CREATE TABLE t(...)\nPRIMARY KEY(`name`,`id`)\nDISTRIBUTED BY HASH(`id`)", []string{"name", "id"}},
		{"CREATE TABLE t(...)\nPRIMARY KEY(`with,comma`, `with``tick`, `with\nnewline`)\n", []string{"with,comma", "with`tick", "with\nnewline"}},
		{"CREATE TABLE t(...)\nDUPLICATE KEY(`id`)\n", nil},
		{"CREATE TABLE t(...)\nAGGREGATE KEY(`id`)\n", nil},
	} {
		keys, err := primaryKey(target.ddl)
		if err != nil || !reflect.DeepEqual(keys, target.keys) {
			t.Fatalf("native keys=%v want=%v error=%v", keys, target.keys, err)
		}
	}
	for _, ddl := range []string{"PRIMARY KEY(id)", "PRIMARY KEY(`id`,)", "PRIMARY KEY(`id`,`id`)", "PRIMARY KEY(`unterminated)", "PRIMARY KEY(`a`)\nPRIMARY KEY(`b`)"} {
		if _, err := primaryKey(ddl); err == nil {
			t.Fatal("invalid native key accepted: " + ddl)
		}
	}
}
func TestCertifiedVersionAndReservedNamespaces(t *testing.T) {
	for _, v := range []string{"3.5.21-85fccf8", "3.5.21-aabb1122"} {
		if !certifiedVersion.MatchString(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"3.5.210-85fccf8", "5.1.0", "3.6.0-85fccf8", "fake 3.5.21-85fccf8"} {
		if certifiedVersion.MatchString(v) {
			t.Fatal(v)
		}
	}
	for _, s := range []string{"information_schema", "sys", "_statistics_"} {
		if !isSystemNamespace(strings.ToUpper(s)) {
			t.Fatal(s)
		}
	}
	if isSystemNamespace("sys_business") {
		t.Fatal("business namespace hidden")
	}
}
