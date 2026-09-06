package shared

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/xwb1989/sqlparser"
)

func TestMySQLCompatibleQueryProvenanceInspectsPublishedServiceJoin(t *testing.T) {
	provenance := testMySQLCompatibleQueryProvenance("OceanBase")
	references, err := provenance.inspectReadReferences(`
		SELECT addp_source.order_no, addp_source.customer_code
		FROM (
			SELECT o.order_no, c.customer_code
			FROM orders o
			JOIN customers c ON c.id = o.customer_id
		) AS addp_source
		ORDER BY addp_source.order_no ASC
		LIMIT 51
	`)
	if err != nil {
		t.Fatalf("inspectReadReferences failed: %v", err)
	}
	want := []mysqlCompatibleRelationReference{{Name: "orders"}, {Name: "customers"}}
	if fmt.Sprint(references) != fmt.Sprint(want) {
		t.Fatalf("references = %#v, want %#v", references, want)
	}
}

func TestMySQLCompatibleQueryProvenanceRejectsUnprovenSources(t *testing.T) {
	provenance := testMySQLCompatibleQueryProvenance("OceanBase")
	for name, query := range map[string]string{
		"ordinary function": `SELECT lower(name) FROM customers`,
		"row locking":       `SELECT * FROM orders FOR UPDATE`,
		"cte":               `WITH recent AS (SELECT * FROM orders) SELECT * FROM recent`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := provenance.inspectReadReferences(query)
			if !errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
				t.Fatalf("error = %v, want ErrQueryReadSetUnresolved", err)
			}
		})
	}
}

func TestMySQLCompatibleQueryProvenanceResolvesCanonicalReadSet(t *testing.T) {
	provenance := testMySQLCompatibleQueryProvenance("OceanBase")
	catalog := &fakeMySQLCompatibleReadCatalog{resolved: map[mysqlCompatibleRelationReference]mysqlCompatibleResolvedRelation{
		{Database: "business", Name: "orders"}: {Database: "business", Name: "orders", TableType: "BASE TABLE", Engine: "InnoDB"},
		{Database: "crm", Name: "customers"}:   {Database: "crm", Name: "customers", TableType: "BASE TABLE", Engine: "InnoDB"},
	}}
	req := plugin.QueryRequest{EngineID: 23, Language: "sql", Query: "SELECT", Options: plugin.QueryOptions{ReadOnly: true}}
	readSet, err := provenance.resolveReadSet(t.Context(), req, "business", []mysqlCompatibleRelationReference{
		{Name: "orders"}, {Database: "crm", Name: "customers"}, {Name: "orders"},
	}, catalog)
	if err != nil {
		t.Fatalf("resolveReadSet failed: %v", err)
	}
	if len(readSet.Paths) != 2 {
		t.Fatalf("paths = %#v, want two canonical paths", readSet.Paths)
	}
	assertMySQLCompatibleReadPath(t, readSet.Paths[0], 23, "business", "orders")
	assertMySQLCompatibleReadPath(t, readSet.Paths[1], 23, "crm", "customers")
}

func TestMySQLCompatibleQueryProvenanceRejectsViewsAndSystemDatabases(t *testing.T) {
	provenance := testMySQLCompatibleQueryProvenance("OceanBase")
	for name, relation := range map[string]mysqlCompatibleResolvedRelation{
		"view":      {Database: "business", Name: "order_view", TableType: "VIEW"},
		"federated": {Database: "business", Name: "remote_orders", TableType: "BASE TABLE", Engine: "FEDERATED"},
		"system":    {Database: "mysql", Name: "user", TableType: "BASE TABLE", Engine: "InnoDB"},
	} {
		t.Run(name, func(t *testing.T) {
			reference := mysqlCompatibleRelationReference{Database: relation.Database, Name: relation.Name}
			catalog := &fakeMySQLCompatibleReadCatalog{resolved: map[mysqlCompatibleRelationReference]mysqlCompatibleResolvedRelation{reference: relation}}
			req := plugin.QueryRequest{EngineID: 23, Language: "sql", Query: "SELECT", Options: plugin.QueryOptions{ReadOnly: true}}
			_, err := provenance.resolveReadSet(t.Context(), req, "business", []mysqlCompatibleRelationReference{reference}, catalog)
			if !errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
				t.Fatalf("error = %v, want ErrQueryReadSetUnresolved", err)
			}
		})
	}
}

func TestMySQLCompatibleQueryProvenanceResolvesJoinOutputLineage(t *testing.T) {
	provenance := testMySQLCompatibleQueryProvenance("OceanBase")
	statement, err := sqlparser.Parse(`
		SELECT addp_source.order_no, addp_source.customer_code, addp_source.total_amount
		FROM (
			SELECT o.order_no, c.customer_code, o.total_amount
			FROM orders o JOIN customers c ON c.id = o.customer_id
		) AS addp_source
	`)
	if err != nil {
		t.Fatal(err)
	}
	sources := []plugin.QueryOutputSource{
		testMySQLCompatibleLineageSource(23, "business", "customers", "id", "customer_code"),
		testMySQLCompatibleLineageSource(23, "business", "orders", "order_no", "customer_id", "total_amount"),
	}
	resolved, err := provenance.resolveSelectOutputLineage("business", statement, sources)
	if err != nil {
		t.Fatalf("resolveSelectOutputLineage failed: %v", err)
	}
	readSet, err := plugin.NewQueryReadSet(sources[0].Path, sources[1].Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := plugin.ValidateQueryOutputLineage(readSet, &plugin.QueryOutputLineage{Sources: resolved}); err != nil {
		t.Fatalf("ValidateQueryOutputLineage failed: %v", err)
	}
	assertMySQLCompatibleLineageBindings(t, resolved, "customers", map[string]string{"customer_code": "customer_code"})
	assertMySQLCompatibleLineageBindings(t, resolved, "orders", map[string]string{"order_no": "order_no", "total_amount": "total_amount"})
}

func TestMySQLCompatibleQueryProvenanceRejectsUnprovenProjection(t *testing.T) {
	provenance := testMySQLCompatibleQueryProvenance("OceanBase")
	sources := []plugin.QueryOutputSource{testMySQLCompatibleLineageSource(23, "business", "orders", "id", "total_amount")}
	statement, err := sqlparser.Parse(`SELECT total_amount + 1 AS adjusted FROM orders`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = provenance.resolveSelectOutputLineage("business", statement, sources)
	if !errors.Is(err, plugin.ErrQueryOutputLineageUnresolved) {
		t.Fatalf("error = %v, want ErrQueryOutputLineageUnresolved", err)
	}
}

func testMySQLCompatibleQueryProvenance(engineName string) MySQLCompatibleQueryProvenance {
	return MySQLCompatibleQueryProvenance{
		EngineName:   engineName,
		DefaultPort:  2881,
		CatalogModel: plugin.TabularCatalogModel(plugin.EngineCatalogTermDatabase),
		IsSystemNamespace: func(name string) bool {
			return name == "mysql" || name == "information_schema"
		},
	}
}

type fakeMySQLCompatibleReadCatalog struct {
	resolved map[mysqlCompatibleRelationReference]mysqlCompatibleResolvedRelation
}

func (f *fakeMySQLCompatibleReadCatalog) ResolveRelation(_ context.Context, reference mysqlCompatibleRelationReference) (mysqlCompatibleResolvedRelation, error) {
	resolved, ok := f.resolved[reference]
	if !ok {
		return mysqlCompatibleResolvedRelation{}, fmt.Errorf("relation not found")
	}
	return resolved, nil
}

func assertMySQLCompatibleReadPath(t *testing.T, path plugin.EngineCatalogPath, engineID uint, database, table string) {
	t.Helper()
	segments := plugin.EngineCatalogPathWithoutRoot(path).Segments
	if path.EngineID != engineID || len(segments) != 2 || segments[0].Name != database || segments[1].Name != table {
		t.Fatalf("path = %#v, want engine %d table %s.%s", path, engineID, database, table)
	}
}

func testMySQLCompatibleLineageSource(engineID uint, database, table string, fields ...string) plugin.QueryOutputSource {
	fieldInfos := make([]datatype.FieldInfo, len(fields))
	for index, field := range fields {
		fieldInfos[index] = datatype.FieldInfo{Name: field, Type: datatype.FieldTypeString}
	}
	return plugin.QueryOutputSource{
		Path: plugin.EngineCatalogBranchLeafPath(
			plugin.TabularCatalogModel(plugin.EngineCatalogTermDatabase), engineID,
			plugin.EngineCatalogTermDatabase, database,
			plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, table,
		),
		Fields: fieldInfos,
	}
}

func assertMySQLCompatibleLineageBindings(t *testing.T, sources []plugin.QueryOutputSource, table string, want map[string]string) {
	t.Helper()
	for _, source := range sources {
		segments := plugin.EngineCatalogPathWithoutRoot(source.Path).Segments
		if len(segments) != 2 || segments[1].Name != table {
			continue
		}
		got := make(map[string]string, len(source.Bindings))
		for _, binding := range source.Bindings {
			got[binding.SourcePath[0]] = binding.OutputPath[0]
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s bindings = %#v, want %#v", table, got, want)
		}
		return
	}
	t.Fatalf("lineage source for table %s not found", table)
}
