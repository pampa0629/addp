package highgo

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/lib/pq"
)

// Runs inside the existing owned, random native/protocol write fixture. It
// adds no database, account or grant and cleans only its named objects.
func assertHighGoRelationalFacts(t *testing.T, ctx context.Context, db *sql.DB, p *Plugin, info plugin.ConnectionInfo, schema string) {
	t.Helper()
	qualified := func(name string) string { return pq.QuoteIdentifier(schema) + "." + pq.QuoteIdentifier(name) }
	names := []string{"fact_p0_1", "fact_p1_1", "fact_p0", "fact_p1", "fact_partitioned", "fact_expr", "fact_hash_0", "fact_hash_1", "fact_hash", "fact_mix_0", "fact_mix_1", "fact_mixed", `child "facts"`, `parent "facts"`}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, name := range names {
			if _, err := db.ExecContext(cleanup, "DROP TABLE IF EXISTS "+qualified(name)+" RESTRICT"); err != nil {
				t.Errorf("cleanup relational fixture %s: %v", name, err)
			}
		}
	})
	parent, child := qualified(`parent "facts"`), qualified(`child "facts"`)
	statements := []string{
		"CREATE TABLE " + parent + " (a bigint,b bigint,label text,CONSTRAINT facts_pk PRIMARY KEY(b,a),CONSTRAINT facts_unique UNIQUE(a,b),CONSTRAINT facts_label UNIQUE(label))",
		"CREATE TABLE " + child + " (x bigint,y bigint,note text,CONSTRAINT facts_fk FOREIGN KEY(x,y) REFERENCES " + parent + "(a,b))",
		"CREATE INDEX " + pq.QuoteIdentifier(`idx "ordered"`) + " ON " + child + "(y,x) INCLUDE(note)",
		"CREATE INDEX facts_partial ON " + child + "(x) WHERE x>0",
		"CREATE INDEX facts_expression ON " + child + "(lower(note))",
		"CREATE TABLE " + qualified("fact_partitioned") + " (day date,bucket int) PARTITION BY RANGE(day)",
		"CREATE TABLE " + qualified("fact_p0") + " PARTITION OF " + qualified("fact_partitioned") + " FOR VALUES FROM ('2026-01-01') TO ('2027-01-01') PARTITION BY LIST(bucket)",
		"CREATE TABLE " + qualified("fact_p1") + " PARTITION OF " + qualified("fact_partitioned") + " FOR VALUES FROM ('2027-01-01') TO ('2028-01-01') PARTITION BY LIST(bucket)",
		"CREATE TABLE " + qualified("fact_p0_1") + " PARTITION OF " + qualified("fact_p0") + " FOR VALUES IN (1)",
		"CREATE TABLE " + qualified("fact_p1_1") + " PARTITION OF " + qualified("fact_p1") + " FOR VALUES IN (1)",
		"CREATE TABLE " + qualified("fact_expr") + " (label text) PARTITION BY HASH(lower(label))",
		"CREATE TABLE " + qualified("fact_hash") + " (id bigint) PARTITION BY HASH(id)",
		"CREATE TABLE " + qualified("fact_hash_0") + " PARTITION OF " + qualified("fact_hash") + " FOR VALUES WITH (MODULUS 2,REMAINDER 0)",
		"CREATE TABLE " + qualified("fact_hash_1") + " PARTITION OF " + qualified("fact_hash") + " FOR VALUES WITH (MODULUS 2,REMAINDER 1)",
		"CREATE TABLE " + qualified("fact_mixed") + " (id bigint,bucket int) PARTITION BY RANGE(id)",
		"CREATE TABLE " + qualified("fact_mix_0") + " PARTITION OF " + qualified("fact_mixed") + " FOR VALUES FROM (0) TO (10) PARTITION BY LIST(bucket)",
		"CREATE TABLE " + qualified("fact_mix_1") + " PARTITION OF " + qualified("fact_mixed") + " FOR VALUES FROM (10) TO (20)",
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("prepare relational fixture: %v", err)
		}
	}
	read := func(name string, opts plugin.EngineCatalogFactsOptions) *plugin.EngineCatalogFacts {
		t.Helper()
		facts, err := p.DescribeEngineCatalogFacts(ctx, info, plugin.TabularItemPath(987655, plugin.EngineCatalogTermSchema, schema, name), opts)
		if err != nil {
			t.Fatal(err)
		}
		return facts
	}
	basic := read(`child "facts"`, plugin.EngineCatalogFactsOptions{})
	if len(basic.Indexes) != 0 || len(basic.Constraints) != 0 || basic.Partitioning != nil {
		t.Fatal("unrequested relational enrichment returned")
	}
	all := plugin.EngineCatalogFactsOptions{IncludeIndexes: true, IncludeConstraints: true, IncludePartitioning: true, IncludeSpatialFacts: true}
	facts := read(`child "facts"`, all)
	if len(facts.Indexes) != 1 || facts.Indexes[0].Name != `idx "ordered"` || !reflect.DeepEqual(facts.Indexes[0].Fields, []string{"y", "x"}) || facts.Indexes[0].IsUnique || facts.Indexes[0].IndexType != "btree" {
		t.Fatalf("index keys/order/INCLUDE/expression/partial: %#v", facts.Indexes)
	}
	wantFK := plugin.ConstraintFacts{Name: "facts_fk", ConstraintType: plugin.ConstraintTypeForeignKey, Fields: []string{"x", "y"}, ReferencedNamespace: schema, ReferencedTable: `parent "facts"`, ReferencedFields: []string{"a", "b"}}
	if len(facts.Constraints) != 1 || !reflect.DeepEqual(facts.Constraints[0], wantFK) || facts.Partitioning != nil || facts.Spatial != nil {
		t.Fatalf("foreign key or optional facts mismatch: %#v", facts)
	}
	facts = read(`parent "facts"`, all)
	if len(facts.Constraints) != 3 || len(facts.Indexes) != 3 || !reflect.DeepEqual(facts.Table.PrimaryKey, []string{"b", "a"}) {
		t.Fatalf("named constraints/ordered PK lost: %#v", facts)
	}
	kinds := map[string]string{}
	for _, c := range facts.Constraints {
		kinds[c.Name] = c.ConstraintType
	}
	if kinds["facts_pk"] != plugin.ConstraintTypePrimaryKey || kinds["facts_unique"] != plugin.ConstraintTypeUnique || kinds["facts_label"] != plugin.ConstraintTypeUnique {
		t.Fatalf("constraint kinds: %#v", kinds)
	}
	for _, index := range facts.Indexes {
		if !index.IsUnique {
			t.Fatal("unique index flag lost")
		}
	}
	cases := []struct {
		name string
		want *plugin.TablePartitioningFacts
	}{
		{"fact_partitioned", &plugin.TablePartitioningFacts{Strategy: "range", KeyFields: []string{"day"}, SubpartitionStrategy: "list", SubpartitionKeyFields: []string{"bucket"}, PartitionCount: 2}},
		{"fact_expr", nil},
		{"fact_hash", &plugin.TablePartitioningFacts{Strategy: "hash", KeyFields: []string{"id"}, PartitionCount: 2}},
		{"fact_mixed", &plugin.TablePartitioningFacts{Strategy: "range", KeyFields: []string{"id"}, PartitionCount: 2}},
	}
	for _, tc := range cases {
		facts := read(tc.name, all)
		if !reflect.DeepEqual(facts.Partitioning, tc.want) {
			t.Fatalf("partition facts %s: got=%#v want=%#v", tc.name, facts.Partitioning, tc.want)
		}
	}
	t.Log("relational facts verified: ordered PK/unique/FK/index keys, INCLUDE exclusion, expression/partial exclusion, range/list/hash partitions and heterogeneous subpartition boundary")
}
