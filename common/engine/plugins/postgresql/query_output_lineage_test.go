package postgresql

import (
	"context"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	pgquery "github.com/pganalyze/pg_query_go/v6"
)

func TestResolvePostgresSelectOutputLineageComposesServiceSubquery(t *testing.T) {
	statement := parsePostgresLineageSelect(t, `
		SELECT addp_source.id, addp_source.contact
		FROM (SELECT id, phone AS contact FROM public.people) AS addp_source
		ORDER BY addp_source.id ASC LIMIT 3`)
	sources := []plugin.QueryOutputSource{{
		Path: plugin.TabularItemPath(7, plugin.EngineCatalogTermSchema, "public", "people"),
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt},
			{Name: "phone", Type: datatype.FieldTypeString},
		},
	}}
	catalog := &fakePostgresReadCatalog{resolved: map[postgresRelationReference]postgresResolvedRelation{
		{Schema: "public", Name: "people"}: {OID: 1, Schema: "public", Name: "people", Relkind: "r"},
	}}
	resolved, err := resolvePostgresSelectOutputLineage(context.Background(), catalog, statement, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].OpaqueOutput || resolved[0].IdentityOutput || len(resolved[0].Bindings) != 2 {
		t.Fatalf("lineage = %#v", resolved)
	}
	assertPostgresOutputBinding(t, resolved[0].Bindings[0], "id", "id", plugin.QueryOutputTransformationDirect)
	assertPostgresOutputBinding(t, resolved[0].Bindings[1], "phone", "contact", plugin.QueryOutputTransformationDirect)
}

func TestResolvePostgresSelectOutputLineagePreservesDerivedServiceOutput(t *testing.T) {
	statement := parsePostgresLineageSelect(t, `
		SELECT left(addp_source.contact, 3) AS prefix
		FROM (SELECT phone AS contact FROM public.people) AS addp_source`)
	sources := []plugin.QueryOutputSource{{
		Path:   plugin.TabularItemPath(7, plugin.EngineCatalogTermSchema, "public", "people"),
		Fields: []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeString}},
	}}
	catalog := &fakePostgresReadCatalog{resolved: map[postgresRelationReference]postgresResolvedRelation{
		{Schema: "public", Name: "people"}: {OID: 1, Schema: "public", Name: "people", Relkind: "r"},
	}}
	resolved, err := resolvePostgresSelectOutputLineage(context.Background(), catalog, statement, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].OpaqueOutput || len(resolved[0].Bindings) != 1 {
		t.Fatalf("lineage = %#v", resolved)
	}
	assertPostgresOutputBinding(t, resolved[0].Bindings[0], "phone", "prefix", plugin.QueryOutputTransformationDerived)
}

func parsePostgresLineageSelect(t *testing.T, query string) *pgquery.SelectStmt {
	t.Helper()
	parsed, err := pgquery.Parse(query)
	if err != nil || len(parsed.GetStmts()) != 1 {
		t.Fatalf("parse query: %v", err)
	}
	statement := parsed.GetStmts()[0].GetStmt().GetSelectStmt()
	if statement == nil {
		t.Fatal("query did not parse as SELECT")
	}
	return statement
}

func assertPostgresOutputBinding(t *testing.T, binding plugin.QueryOutputBinding, source, output, transformation string) {
	t.Helper()
	if len(binding.SourcePath) != 1 || binding.SourcePath[0] != source || len(binding.OutputPath) != 1 || binding.OutputPath[0] != output || binding.Transformation != transformation {
		t.Fatalf("binding = %#v", binding)
	}
}
