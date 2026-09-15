package shared

import (
	"fmt"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestMySQLAnalyticalReadClosureAndLineage(t *testing.T) {
	p := testMySQLCompatibleQueryProvenance("MySQL")
	query := `WITH base AS (SELECT o.customer_id, o.total_amount FROM orders o),
	 totals AS (SELECT customer_id, COUNT(*) AS value FROM base GROUP BY customer_id)
	 SELECT t.customer_id, t.value, c.customer_code FROM totals t JOIN customers c ON c.id=t.customer_id`
	refs, err := p.inspectReadReferences(query)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(refs) != fmt.Sprint([]mysqlCompatibleRelationReference{{Name: "orders"}, {Name: "customers"}}) {
		t.Fatalf("CTE leaked into physical closure: %#v", refs)
	}
	statement, err := parseMySQLReadQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	sources := []plugin.QueryOutputSource{
		testMySQLCompatibleLineageSource(23, "business", "orders", "customer_id", "total_amount"),
		testMySQLCompatibleLineageSource(23, "business", "customers", "id", "customer_code"),
	}
	lineage, err := p.resolveSelectOutputLineage("business", statement, sources)
	if err != nil {
		t.Fatal(err)
	}
	assertMySQLCompatibleLineageBindings(t, lineage, "orders", map[string]string{"customer_id": "customer_id"})
	assertMySQLCompatibleLineageBindings(t, lineage, "customers", map[string]string{"customer_code": "customer_code"})
}

func TestMySQLCTEScopePreservesQualifiedPhysicalRelations(t *testing.T) {
	p := testMySQLCompatibleQueryProvenance("MySQL")
	query := `WITH orders AS (SELECT id FROM business.orders)
	 SELECT a.id FROM orders a JOIN (WITH orders AS (SELECT id FROM archive.orders) SELECT id FROM orders) z ON a.id=z.id`
	refs, err := p.inspectReadReferences(query)
	if err != nil {
		t.Fatal(err)
	}
	want := []mysqlCompatibleRelationReference{{Database: "business", Name: "orders"}, {Database: "archive", Name: "orders"}}
	if fmt.Sprint(refs) != fmt.Sprint(want) {
		t.Fatalf("scoped closure=%#v", refs)
	}
}

func TestMySQLAnalyticalASTRejectsUnprovenEffects(t *testing.T) {
	for _, query := range []string{
		`WITH RECURSIVE x AS (SELECT id FROM orders) SELECT * FROM x`,
		`WITH x AS (SELECT * FROM x) SELECT * FROM x`,
		`WITH x AS (SELECT * FROM y), y AS (SELECT * FROM orders) SELECT * FROM x`,
		`WITH x AS (SELECT id FROM orders), x AS (SELECT id FROM customers) SELECT * FROM x`,
		`WITH x AS (SELECT id FROM orders FOR UPDATE) SELECT * FROM x`,
		`WITH unused AS (SELECT load_file('/tmp/private')) SELECT id FROM orders`,
		`SELECT business.count(id) FROM orders`,
		`SELECT sleep(1) FROM orders`,
		`SELECT @x := id FROM orders`,
		`SELECT id FROM orders INTO OUTFILE '/tmp/private'`,
		`SELECT id FROM orders; DELETE FROM orders`,
		`SELECT /*!50000 load_file('/tmp/private'), */ id FROM orders`,
	} {
		t.Run(query, func(t *testing.T) {
			if _, err := parseMySQLReadQuery(query); err == nil {
				t.Fatal("unproven query accepted")
			}
		})
	}
}

func TestMySQLUnionDoesNotInventDirectLineage(t *testing.T) {
	p := testMySQLCompatibleQueryProvenance("MySQL")
	statement, err := parseMySQLReadQuery(`SELECT id AS value FROM orders UNION ALL SELECT id AS value FROM customers`)
	if err != nil {
		t.Fatal(err)
	}
	sources := []plugin.QueryOutputSource{testMySQLCompatibleLineageSource(23, "business", "orders", "id"), testMySQLCompatibleLineageSource(23, "business", "customers", "id")}
	lineage, err := p.resolveSelectOutputLineage("business", statement, sources)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range lineage {
		if len(source.Bindings) != 0 {
			t.Fatal("mixed-source UNION claimed direct lineage")
		}
	}
}

func TestMySQLCTEExpansionIsBounded(t *testing.T) {
	ctes := []string{"x0 AS (SELECT id FROM orders)"}
	for i := 1; i < 24; i++ {
		ctes = append(ctes, fmt.Sprintf("x%d AS (SELECT a.id FROM x%d a JOIN x%d b ON a.id=b.id)", i, i-1, i-1))
	}
	if _, err := parseMySQLReadQuery("WITH " + strings.Join(ctes, ",") + " SELECT id FROM x23"); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("unbounded expansion: %v", err)
	}
}
