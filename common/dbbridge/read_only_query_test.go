package dbbridge

import (
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
)

func TestSupportsReadOnlySQLExecution(t *testing.T) {
	tests := []struct {
		engineType string
		want       bool
	}{
		{engineType: "postgresql", want: true},
		{engineType: "PostgreSQL", want: true},
		{engineType: "mysql", want: true},
		{engineType: "oceanbase", want: true},
		{engineType: "oracle", want: true},
		{engineType: "doris", want: true},
		{engineType: "tidb", want: true},
		{engineType: "clickhouse", want: false},
		{engineType: "spark", want: true},
		{engineType: "mongodb", want: false},
	}
	for _, test := range tests {
		t.Run(test.engineType, func(t *testing.T) {
			if got := SupportsReadOnlySQLExecution(test.engineType); got != test.want {
				t.Fatalf("SupportsReadOnlySQLExecution(%q) = %v, want %v", test.engineType, got, test.want)
			}
		})
	}
}

func TestReadOnlySQLExecutionUsesProviderBoundary(t *testing.T) {
	tests := []struct {
		engineType string
		want       plugin.ControlledReadOnlySQLBoundary
	}{
		{engineType: "postgresql", want: plugin.ControlledReadOnlySQLBoundaryDatabaseTransaction},
		{engineType: "mysql", want: plugin.ControlledReadOnlySQLBoundaryDatabaseTransaction},
		{engineType: "tidb", want: plugin.ControlledReadOnlySQLBoundaryValidatedStatement},
		{engineType: "spark", want: plugin.ControlledReadOnlySQLBoundaryValidatedStatement},
	}
	for _, test := range tests {
		t.Run(test.engineType, func(t *testing.T) {
			registered, err := plugin.Get(test.engineType)
			if err != nil {
				t.Fatal(err)
			}
			controlled, ok := registered.(plugin.ControlledReadOnlySQLProvider)
			if !ok {
				t.Fatalf("engine %s does not implement ControlledReadOnlySQLProvider", test.engineType)
			}
			if got := controlled.ControlledReadOnlySQLBoundary(); got != test.want {
				t.Fatalf("engine %s boundary = %q, want %q", test.engineType, got, test.want)
			}
		})
	}
}

func TestBindSQLExecutionParametersUsesNativeDriverPlaceholders(t *testing.T) {
	tests := []struct {
		dialect   string
		wantQuery string
	}{
		{dialect: commonquery.DialectPostgreSQL, wantQuery: "SELECT * FROM members WHERE status = $1 AND score > $2"},
		{dialect: commonquery.DialectOracle, wantQuery: "SELECT * FROM members WHERE status = :1 AND score > :2"},
		{dialect: commonquery.DialectMySQL, wantQuery: "SELECT * FROM members WHERE status = ? AND score > ?"},
	}
	for _, test := range tests {
		t.Run(test.dialect, func(t *testing.T) {
			query, args, err := bindSQLExecutionParameters(test.dialect,
				"SELECT * FROM members WHERE status = :status AND score > :score",
				map[string]interface{}{"status": "active", "score": 10},
			)
			if err != nil {
				t.Fatal(err)
			}
			if query != test.wantQuery {
				t.Fatalf("query = %q, want %q", query, test.wantQuery)
			}
			if !reflect.DeepEqual(args, []interface{}{"active", 10}) {
				t.Fatalf("args = %#v", args)
			}
		})
	}
}
