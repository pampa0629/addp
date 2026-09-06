package dbbridge

import (
	"reflect"
	"testing"

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

func TestReadOnlyTransactionStrategyUsesOracleStatement(t *testing.T) {
	if options := readOnlyTxOptions("oracle"); options != nil {
		t.Fatalf("Oracle transaction options = %#v, want nil so the driver can begin a normal transaction", options)
	}
	if !requiresSQLReadOnlyStatement("oracle") {
		t.Fatal("Oracle must apply read-only mode with SET TRANSACTION READ ONLY")
	}

	options := readOnlyTxOptions("postgresql")
	if options == nil || !options.ReadOnly {
		t.Fatalf("PostgreSQL transaction options = %#v, want ReadOnly=true", options)
	}
	if requiresSQLReadOnlyStatement("postgresql") {
		t.Fatal("PostgreSQL must not use the Oracle-specific read-only statement")
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
