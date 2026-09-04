package postgresql

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestInspectPostgresSQLReadReferencesTracksCTEScope(t *testing.T) {
	dependencies, err := inspectPostgresSQLReadDependencies(`
		WITH recent AS (
			SELECT id FROM sales.orders
		), enriched AS (
			SELECT recent.id FROM recent JOIN inventory.items i ON i.id = recent.id
		)
		SELECT id FROM enriched
	`)
	if err != nil {
		t.Fatalf("inspectPostgresSQLReadDependencies failed: %v", err)
	}
	want := []postgresRelationReference{
		{Schema: "sales", Name: "orders"},
		{Schema: "inventory", Name: "items"},
	}
	if fmt.Sprint(dependencies.Relations) != fmt.Sprint(want) {
		t.Fatalf("references = %#v, want %#v", dependencies.Relations, want)
	}
}

func TestInspectPostgresSQLReadReferencesTracksRecursiveCTE(t *testing.T) {
	dependencies, err := inspectPostgresSQLReadDependencies(`
		WITH RECURSIVE tree AS (
			SELECT id, parent_id FROM org.nodes WHERE parent_id IS NULL
			UNION ALL
			SELECT child.id, child.parent_id
			FROM org.nodes child JOIN tree parent ON child.parent_id = parent.id
		)
		SELECT id FROM tree
	`)
	if err != nil {
		t.Fatalf("inspectPostgresSQLReadDependencies failed: %v", err)
	}
	if len(dependencies.Relations) != 2 || dependencies.Relations[0].Schema != "org" || dependencies.Relations[0].Name != "nodes" || dependencies.Relations[1] != dependencies.Relations[0] {
		t.Fatalf("references = %#v, want two org.nodes syntax references", dependencies.Relations)
	}
}

func TestInspectPostgresSQLReadReferencesPreservesQuotedNames(t *testing.T) {
	dependencies, err := inspectPostgresSQLReadDependencies(`SELECT "Order ID" FROM "Sales Data"."Order Facts"`)
	if err != nil {
		t.Fatalf("inspectPostgresSQLReadDependencies failed: %v", err)
	}
	if len(dependencies.Relations) != 1 || dependencies.Relations[0].Schema != "Sales Data" || dependencies.Relations[0].Name != "Order Facts" {
		t.Fatalf("references = %#v", dependencies.Relations)
	}
}

func TestInspectPostgresSQLReadDependenciesCollectsOrdinaryFunctions(t *testing.T) {
	dependencies, err := inspectPostgresSQLReadDependencies(`
		SELECT count(*), pg_catalog.to_date(btrim(activity_date_raw), 'YYYY-MM-DD')
		FROM outdoor.activities
		WHERE COALESCE(activity_status, '') <> '' AND NULLIF(activity_date_raw, '') IS NOT NULL
	`)
	if err != nil {
		t.Fatalf("inspectPostgresSQLReadDependencies failed: %v", err)
	}
	want := []postgresFunctionReference{
		{Name: "count", ArgumentCount: 0},
		{Schema: "pg_catalog", Name: "to_date", ArgumentCount: 2},
		{Name: "btrim", ArgumentCount: 1},
	}
	if fmt.Sprint(dependencies.Functions) != fmt.Sprint(want) {
		t.Fatalf("functions = %#v, want %#v", dependencies.Functions, want)
	}
}

func TestInspectPostgresSQLReadReferencesRejectsUnprovenSources(t *testing.T) {
	tests := map[string]string{
		"table function": `SELECT * FROM generate_series(1, 3)`,
		"write CTE":      `WITH changed AS (DELETE FROM sales.orders RETURNING id) SELECT id FROM changed`,
		"row locking":    `SELECT * FROM sales.orders FOR UPDATE`,
		"select into":    `SELECT * INTO copied_orders FROM sales.orders`,
	}
	for name, query := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := inspectPostgresSQLReadDependencies(query)
			if !errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
				t.Fatalf("error = %v, want ErrQueryReadSetUnresolved", err)
			}
		})
	}
}

func TestResolvePostgresQueryReadSetExpandsViews(t *testing.T) {
	catalog := &fakePostgresReadCatalog{
		resolved: map[postgresRelationReference]postgresResolvedRelation{
			{Schema: "reporting", Name: "orders_view"}: {OID: 10, Schema: "reporting", Name: "orders_view", Relkind: "v"},
		},
		dependencies: map[int64][]postgresResolvedRelation{
			10: {{OID: 20, Schema: "sales", Name: "orders", Relkind: "r"}},
		},
	}
	req := plugin.QueryRequest{EngineID: 7, Language: "sql", Query: `SELECT id FROM reporting.orders_view`, Options: plugin.QueryOptions{ReadOnly: true}}
	readSet, err := (&PostgreSQLPlugin{}).resolvePostgresQueryReadSet(
		context.Background(), req,
		postgresQueryReadDependencies{Relations: []postgresRelationReference{{Schema: "reporting", Name: "orders_view"}}}, catalog,
	)
	if err != nil {
		t.Fatalf("resolvePostgresQueryReadSet failed: %v", err)
	}
	if len(readSet.Paths) != 2 {
		t.Fatalf("paths = %#v, want view and base table", readSet.Paths)
	}
	assertPostgresReadPath(t, readSet.Paths[0], 7, "reporting", "orders_view", "view")
	assertPostgresReadPath(t, readSet.Paths[1], 7, "sales", "orders", plugin.EngineCatalogKindTable)
}

func TestResolvePostgresQueryReadSetTreatsMaterializedViewAsStoredLeaf(t *testing.T) {
	catalog := &fakePostgresReadCatalog{resolved: map[postgresRelationReference]postgresResolvedRelation{
		{Schema: "reporting", Name: "orders_snapshot"}: {OID: 30, Schema: "reporting", Name: "orders_snapshot", Relkind: "m"},
	}}
	req := plugin.QueryRequest{EngineID: 7, Language: "sql", Query: `SELECT * FROM reporting.orders_snapshot`, Options: plugin.QueryOptions{ReadOnly: true}}
	readSet, err := (&PostgreSQLPlugin{}).resolvePostgresQueryReadSet(
		context.Background(), req,
		postgresQueryReadDependencies{Relations: []postgresRelationReference{{Schema: "reporting", Name: "orders_snapshot"}}}, catalog,
	)
	if err != nil {
		t.Fatalf("resolvePostgresQueryReadSet failed: %v", err)
	}
	if len(readSet.Paths) != 1 {
		t.Fatalf("paths = %#v, want one materialized view", readSet.Paths)
	}
	assertPostgresReadPath(t, readSet.Paths[0], 7, "reporting", "orders_snapshot", "materialized_view")
}

func TestResolvePostgresQueryReadSetRejectsForeignAndSystemRelations(t *testing.T) {
	tests := map[string]postgresResolvedRelation{
		"foreign": {OID: 40, Schema: "public", Name: "external_orders", Relkind: "f"},
		"system":  {OID: 41, Schema: "pg_catalog", Name: "pg_class", Relkind: "r"},
	}
	for name, relation := range tests {
		t.Run(name, func(t *testing.T) {
			reference := postgresRelationReference{Schema: relation.Schema, Name: relation.Name}
			catalog := &fakePostgresReadCatalog{resolved: map[postgresRelationReference]postgresResolvedRelation{reference: relation}}
			req := plugin.QueryRequest{EngineID: 7, Language: "sql", Query: "SELECT", Options: plugin.QueryOptions{ReadOnly: true}}
			_, err := (&PostgreSQLPlugin{}).resolvePostgresQueryReadSet(
				context.Background(), req, postgresQueryReadDependencies{Relations: []postgresRelationReference{reference}}, catalog,
			)
			if !errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
				t.Fatalf("error = %v, want ErrQueryReadSetUnresolved", err)
			}
		})
	}
}

func TestResolvePostgresQueryReadSetAcceptsTransparentFunctions(t *testing.T) {
	reference := postgresRelationReference{Schema: "sales", Name: "orders"}
	functions := []postgresFunctionReference{{Name: "count", ArgumentCount: 0}, {Name: "btrim", ArgumentCount: 1}}
	catalog := &fakePostgresReadCatalog{
		resolved: map[postgresRelationReference]postgresResolvedRelation{
			reference: {OID: 50, Schema: "sales", Name: "orders", Relkind: "r"},
		},
		functionCandidates: map[postgresFunctionReference][]postgresResolvedFunction{
			functions[0]: {{OID: 2803, Schema: "pg_catalog", Name: "count", Language: "internal", Kind: "a", Volatility: "i"}},
			functions[1]: {{OID: 885, Schema: "pg_catalog", Name: "btrim", Language: "internal", Kind: "f", Volatility: "i"}},
		},
	}
	req := plugin.QueryRequest{EngineID: 7, Language: "sql", Query: "SELECT count(*)", Options: plugin.QueryOptions{ReadOnly: true}}
	readSet, err := (&PostgreSQLPlugin{}).resolvePostgresQueryReadSet(
		context.Background(), req,
		postgresQueryReadDependencies{Relations: []postgresRelationReference{reference}, Functions: functions}, catalog,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(readSet.Paths) != 1 {
		t.Fatalf("paths = %#v, want one", readSet.Paths)
	}
}

func TestResolvePostgresQueryReadSetRejectsUntrustedFunctionCandidate(t *testing.T) {
	function := postgresFunctionReference{Name: "mask_or_lookup", ArgumentCount: 1}
	catalog := &fakePostgresReadCatalog{functionCandidates: map[postgresFunctionReference][]postgresResolvedFunction{
		function: {
			{OID: 59, Schema: "pg_catalog", Name: function.Name, Language: "internal", Kind: "f", Volatility: "i"},
			{OID: 60, Schema: "public", Name: function.Name, Language: "sql", Kind: "f", Volatility: "s"},
		},
	}}
	req := plugin.QueryRequest{EngineID: 7, Language: "sql", Query: "SELECT mask_or_lookup(phone)", Options: plugin.QueryOptions{ReadOnly: true}}
	_, err := (&PostgreSQLPlugin{}).resolvePostgresQueryReadSet(
		context.Background(), req, postgresQueryReadDependencies{Functions: []postgresFunctionReference{function}}, catalog,
	)
	if !errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
		t.Fatalf("error = %v, want ErrQueryReadSetUnresolved", err)
	}
}

func TestTransparentPostgresReadFunctionBoundary(t *testing.T) {
	trusted := postgresResolvedFunction{
		OID: 885, Schema: "pg_catalog", Name: "btrim", Language: "internal", Kind: "f", Volatility: "i",
	}
	tests := map[string]postgresResolvedFunction{
		"trusted scalar": trusted,
		"trusted aggregate": {
			OID: 2803, Schema: "pg_catalog", Name: "count", Language: "internal", Kind: "a", Volatility: "i",
		},
		"trusted stable window": {
			OID: 3100, Schema: "pg_catalog", Name: "row_number", Language: "internal", Kind: "w", Volatility: "s",
		},
		"trusted postgis extension member": {
			OID: 18614, Schema: "public", Name: "st_asgeojson", Language: "c", Kind: "f", Volatility: "i", Extension: "postgis",
		},
		"user schema": {OID: 1, Schema: "public", Language: "internal", Kind: "f", Volatility: "i"},
		"untrusted extension member": {
			OID: 2, Schema: "public", Language: "c", Kind: "f", Volatility: "i", Extension: "vector",
		},
		"non-internal":     {OID: 1, Schema: "pg_catalog", Language: "c", Kind: "f", Volatility: "i"},
		"set returning":    {OID: 1, Schema: "pg_catalog", Language: "internal", Kind: "f", Volatility: "i", ReturnsSet: true},
		"security definer": {OID: 1, Schema: "pg_catalog", Language: "internal", Kind: "f", Volatility: "i", SecurityDefiner: true},
		"volatile":         {OID: 1, Schema: "pg_catalog", Language: "internal", Kind: "f", Volatility: "v"},
		"procedure":        {OID: 1, Schema: "pg_catalog", Language: "internal", Kind: "p", Volatility: "i"},
		"missing identity": {Schema: "pg_catalog", Language: "internal", Kind: "f", Volatility: "i"},
	}
	for name, function := range tests {
		t.Run(name, func(t *testing.T) {
			got := isTransparentPostgresReadFunction(function)
			want := name == "trusted scalar" || name == "trusted aggregate" || name == "trusted stable window" || name == "trusted postgis extension member"
			if got != want {
				t.Fatalf("isTransparentPostgresReadFunction() = %v, want %v for %#v", got, want, function)
			}
		})
	}
}

func TestResolvePostgresQueryReadSetRejectsUntrustedFunctionInView(t *testing.T) {
	reference := postgresRelationReference{Schema: "reporting", Name: "order_counts"}
	catalog := &fakePostgresReadCatalog{
		resolved: map[postgresRelationReference]postgresResolvedRelation{
			reference: {OID: 50, Schema: "reporting", Name: "order_counts", Relkind: "v"},
		},
		viewFunctions: map[int64][]postgresResolvedFunction{
			50: {{OID: 61, Schema: "reporting", Name: "lookup_order", Language: "sql", Kind: "f", Volatility: "s"}},
		},
	}
	req := plugin.QueryRequest{EngineID: 7, Language: "sql", Query: "SELECT", Options: plugin.QueryOptions{ReadOnly: true}}
	_, err := (&PostgreSQLPlugin{}).resolvePostgresQueryReadSet(
		context.Background(), req, postgresQueryReadDependencies{Relations: []postgresRelationReference{reference}}, catalog,
	)
	if !errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
		t.Fatalf("error = %v, want ErrQueryReadSetUnresolved", err)
	}
}

type fakePostgresReadCatalog struct {
	resolved           map[postgresRelationReference]postgresResolvedRelation
	functionCandidates map[postgresFunctionReference][]postgresResolvedFunction
	dependencies       map[int64][]postgresResolvedRelation
	viewFunctions      map[int64][]postgresResolvedFunction
}

func (c *fakePostgresReadCatalog) ResolveRelation(_ context.Context, reference postgresRelationReference) (postgresResolvedRelation, error) {
	relation, ok := c.resolved[reference]
	if !ok {
		return postgresResolvedRelation{}, fmt.Errorf("relation not found")
	}
	return relation, nil
}

func (c *fakePostgresReadCatalog) FunctionCandidates(_ context.Context, reference postgresFunctionReference) ([]postgresResolvedFunction, error) {
	return append([]postgresResolvedFunction(nil), c.functionCandidates[reference]...), nil
}

func (c *fakePostgresReadCatalog) ViewDependencies(_ context.Context, oid int64) ([]postgresResolvedRelation, error) {
	return append([]postgresResolvedRelation(nil), c.dependencies[oid]...), nil
}

func (c *fakePostgresReadCatalog) ViewFunctionDependencies(_ context.Context, oid int64) ([]postgresResolvedFunction, error) {
	return append([]postgresResolvedFunction(nil), c.viewFunctions[oid]...), nil
}

func assertPostgresReadPath(t *testing.T, path plugin.EngineCatalogPath, engineID uint, schema, name, kind string) {
	t.Helper()
	if path.EngineID != engineID || len(path.Segments) != 3 {
		t.Fatalf("path = %#v", path)
	}
	if path.Segments[1].Term != plugin.EngineCatalogTermSchema || path.Segments[1].Name != schema {
		t.Fatalf("schema segment = %#v", path.Segments[1])
	}
	if path.Segments[2].Term != plugin.EngineCatalogTermTable || path.Segments[2].Name != name || path.Segments[2].Kind != kind {
		t.Fatalf("leaf segment = %#v", path.Segments[2])
	}
}
