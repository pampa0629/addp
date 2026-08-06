package dbbridge

import (
	"reflect"
	"testing"
)

func TestSupportsReadOnlySQLExecution(t *testing.T) {
	tests := []struct {
		engineType string
		want       bool
	}{
		{engineType: "postgresql", want: true},
		{engineType: "PostgreSQL", want: true},
		{engineType: "mysql", want: true},
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

func TestBindSQLExecutionParametersUsesNativeDriverPlaceholders(t *testing.T) {
	tests := []struct {
		engineType string
		wantQuery  string
	}{
		{engineType: "postgresql", wantQuery: "SELECT * FROM members WHERE status = $1 AND score > $2"},
		{engineType: "mysql", wantQuery: "SELECT * FROM members WHERE status = ? AND score > ?"},
		{engineType: "doris", wantQuery: "SELECT * FROM members WHERE status = ? AND score > ?"},
	}
	for _, test := range tests {
		t.Run(test.engineType, func(t *testing.T) {
			query, args, err := bindSQLExecutionParameters(test.engineType,
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
