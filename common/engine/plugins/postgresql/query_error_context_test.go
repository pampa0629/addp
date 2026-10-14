package postgresql

import (
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/lib/pq"
)

func TestPostgresUnrelatedDatabaseErrorCannotBorrowQueryPosition(t *testing.T) {
	cause := &pq.Error{Code: "42703", Position: "8", Message: "private connection initialization error"}
	err := postgresNativeQueryError(cause, "SELECT missing_field FROM public.a", plugin.QueryOptions{ReadOnly: true})
	if plugin.QueryErrorContextOf(err) != nil {
		t.Fatal("unrelated database error acquired user query context")
	}
}

func TestMissingPostgresTableCarriesOnlyASTObjectAndOriginalPosition(t *testing.T) {
	query := "-- 中文😀\nSELECT * FROM \"public\".\"farmla1nd\" LIMIT 10"
	dependencies, err := inspectPostgresSQLReadDependencies(query)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&PostgreSQLPlugin{}).resolvePostgresQueryReadSet(t.Context(), plugin.QueryRequest{EngineID: 7, Query: query}, dependencies, &fakePostgresReadCatalog{})
	context := plugin.QueryErrorContextOf(err)
	if context == nil || context.Object == nil || context.Object.Kind != "table" || context.Object.Name != `"public"."farmla1nd"` || context.Position == nil || context.Position.Line != 2 || context.Position.Column != 15 {
		t.Fatalf("missing source diagnostic: %+v from %v", context, err)
	}
}

func TestPostgresSyntaxPositionIsMappedAcrossUnicodeAndLeadingWhitespace(t *testing.T) {
	_, err := (&PostgreSQLPlugin{}).PrepareQuery(t.Context(), nil, plugin.QueryRequest{EngineID: 7, Language: "sql", Query: " \nSELECT '中文😀',\n FROM public.a", Options: plugin.QueryOptions{ReadOnly: true}})
	context := plugin.QueryErrorContextOf(err)
	if context == nil || context.Object != nil || context.Position == nil || context.Position.Line != 3 || context.Position.Column != 2 {
		t.Fatalf("invalid syntax location: %+v from %v", context, err)
	}
}
