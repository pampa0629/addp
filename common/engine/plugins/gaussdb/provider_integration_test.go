package gaussdb

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func integrationInfo(t *testing.T) plugin.ConnectionInfo {
	t.Helper()
	if os.Getenv("ADDP_GAUSSDB_INTEGRATION") != "1" {
		t.Skip("use make test-common-gaussdb")
	}
	content, err := os.ReadFile(filepath.Join(os.Getenv("ADDP_GAUSSDB_FIXTURE_DIR"), "gaussdb-engine.json"))
	if err != nil {
		t.Fatal(err)
	}
	var descriptor struct {
		Info plugin.ConnectionInfo `json:"connection_info"`
	}
	if err = json.Unmarshal(content, &descriptor); err != nil {
		t.Fatal(err)
	}
	return descriptor.Info
}
func TestIntegrationGaussDBCatalogAndPreciseValues(t *testing.T) {
	info := integrationInfo(t)
	p := &Plugin{}
	ctx := t.Context()
	if _, err := p.ProbeConnection(ctx, info); err != nil {
		t.Fatal(err)
	}
	root := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), 71)
	children, err := p.ListChildren(ctx, info, root, plugin.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range children {
		if entry.Name != "public" && entry.Name != "addp_acceptance" {
			t.Fatal("ungranted or reserved schema exposed")
		}
		if entry.Name == "addp_acceptance" {
			found = true
		}
	}
	if !found {
		for _, statement := range []string{"SELECT nspname, has_schema_privilege(oid,'USAGE') AS can_use FROM pg_catalog.pg_namespace ORDER BY nspname", "SELECT schema_name FROM information_schema.schemata", "SELECT pg_get_viewdef('information_schema.schemata'::regclass) AS definition", "SELECT table_name FROM information_schema.tables WHERE table_schema='addp_acceptance'", "SELECT column_name FROM information_schema.columns WHERE table_schema='addp_acceptance' AND table_name='source'", "SELECT datcompatibility FROM pg_catalog.pg_database WHERE datname=current_database()"} {
			result, e := p.ExecuteSQL(ctx, info, statement, plugin.QueryOptions{ReadOnly: true})
			t.Logf("catalog diagnostic %s result=%+v error=%v", statement, result, e)
		}
		t.Fatalf("business schema missing: %+v", children)
	}
	path := plugin.TabularItemPath(71, plugin.EngineCatalogTermSchema, "addp_acceptance", "source")
	entries, err := p.ListChildren(ctx, info, plugin.TabularNamespacePath(71, plugin.EngineCatalogTermSchema, "addp_acceptance"), plugin.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "source" {
		t.Fatalf("SELECT-only visibility: %+v", entries)
	}
	resolved, err := p.ResolvePath(ctx, info, path)
	if err != nil || resolved == nil {
		t.Fatalf("resolve: %v", err)
	}
	for _, name := range []string{"hidden", "missing"} {
		hidden := plugin.TabularItemPath(71, plugin.EngineCatalogTermSchema, "addp_acceptance", name)
		if _, err := p.ResolvePath(ctx, info, hidden); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
			t.Fatalf("ungranted %s resolved: %v", name, err)
		}
		if _, err := p.DescribeEngineCatalogFacts(ctx, info, hidden, plugin.EngineCatalogFactsOptions{}); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
			t.Fatalf("ungranted %s facts: %v", name, err)
		}
	}
	facts, err := p.DescribeEngineCatalogFacts(ctx, info, path, plugin.EngineCatalogFactsOptions{IncludeStatistics: true, IncludeIndexes: true, IncludeConstraints: true, IncludePartitioning: true, IncludeSpatialFacts: true})
	if err != nil {
		t.Fatal(err)
	}
	if facts.Table == nil || !reflect.DeepEqual(facts.Table.PrimaryKey, []string{"name", "id"}) || len(facts.Table.Fields) != 4 {
		t.Fatalf("field/PK facts: %+v", facts)
	}
	if len(facts.Indexes) != 0 || len(facts.Constraints) != 0 || facts.Partitioning != nil || facts.Spatial != nil {
		t.Fatal("uncertified enrichment returned")
	}
	if facts.Table.RowCount == nil || *facts.Table.RowCount != 2 || facts.Table.Fields[2].Precision != 38 || facts.Table.Fields[2].Scale != 18 {
		t.Fatalf("statistics or Decimal schema: %+v", facts.Table)
	}
	batch, err := p.ReadBatch(ctx, info, path, plugin.BatchReadOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertExactRows(t, batch)
	session, err := p.OpenTableReadSession(ctx, info, path, plugin.TableReadSessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(context.Background())
	var rows []map[string]interface{}
	for {
		batch, err := session.ReadBatch(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Rows) == 0 {
			break
		}
		rows = append(rows, batch.Rows...)
	}
	assertExactRows(t, &plugin.BatchData{Rows: rows})
}
func assertExactRows(t *testing.T, batch *plugin.BatchData) {
	t.Helper()
	if batch == nil || len(batch.Rows) != 2 {
		t.Fatalf("expected two precise rows: %+v", batch)
	}
	for _, r := range batch.Rows {
		switch fmt.Sprint(r["id"]) {
		case "9007199254740993":
			if fmt.Sprint(r["amount"]) != "12345678901234567890.123456789012345678" || r["name"] != "高斯正式接入中文" || r["optional_text"] != nil {
				t.Fatalf("precision/null/Chinese changed: %+v", r)
			}
		case "2":
			if !sameDecimal(fmt.Sprint(r["amount"]), "-0.000000000000000001") || r["name"] != "保留尾空格  " || r["optional_text"] != nil {
				t.Fatalf("scale/trailing spaces/empty changed: %+v", r)
			}
		default:
			t.Fatalf("unexpected row: %+v", r)
		}
	}
}
func TestIntegrationGaussDBPreparedReadAndCancellation(t *testing.T) {
	info := integrationInfo(t)
	p := &Plugin{}
	ctx := t.Context()
	boundary, err := p.ExecuteSQL(ctx, info, "SELECT current_setting('transaction_read_only') AS mode", plugin.QueryOptions{ReadOnly: true})
	if err != nil || len(boundary.Rows) != 1 || boundary.Rows[0]["mode"] != "on" {
		t.Fatalf("native read-only transaction not active: result=%+v err=%v", boundary, err)
	}
	typed, typeErr := p.ExecuteSQL(ctx, info, "SELECT id AS big_id,CAST(id AS INTEGER) AS small_id,amount FROM addp_acceptance.source WHERE id=2", plugin.QueryOptions{ReadOnly: true})
	if typeErr != nil || len(typed.Fields) != 3 || typed.Fields[0].Type != datatype.FieldTypeBigInt || typed.Fields[1].Type != datatype.FieldTypeInt || typed.Fields[2].Type != datatype.FieldTypeDecimal || typed.Fields[2].Precision != 38 || typed.Fields[2].Scale != 18 {
		t.Fatalf("native SQL output schema lost: %+v %v", typed, typeErr)
	}
	if typed.Rows[0]["big_id"] != int64(2) || typed.Rows[0]["small_id"] != int64(2) {
		t.Fatal("Provider must retain native SQL integer values")
	}
	unbounded, err := p.ExecuteSQL(ctx, info, "SELECT CAST(1.25 AS NUMERIC) AS amount", plugin.QueryOptions{ReadOnly: true})
	if err != nil || len(unbounded.Fields) != 1 || unbounded.Fields[0].Type != datatype.FieldTypeDecimal || unbounded.Fields[0].Precision != 0 || unbounded.Fields[0].Scale != 0 {
		t.Fatalf("unbounded native numeric must not invent precision: %+v %v", unbounded, err)
	}
	req := plugin.QueryRequest{EngineID: 71, Language: "sql", Query: "SELECT id,name,amount,optional_text FROM addp_acceptance.source WHERE id >= $1", Options: plugin.QueryOptions{ReadOnly: true, Args: []interface{}{int64(1)}}}
	prepared, err := p.PrepareQuery(ctx, info, req)
	if err != nil {
		t.Fatal(err)
	}
	set, err := prepared.ReadSet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := plugin.TabularItemPath(71, plugin.EngineCatalogTermSchema, "addp_acceptance", "source")
	if set == nil || len(set.Paths) != 1 || !reflect.DeepEqual(set.Paths[0], want) {
		t.Fatalf("read set identity: %+v", set)
	}
	if _, err := prepared.OutputLineage(ctx); err != nil {
		t.Fatal(err)
	}
	session, err := p.OpenQueryReadSession(ctx, prepared)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(context.Background())
	batch, err := session.ReadBatch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	assertExactRows(t, batch)
	if _, err := prepared.Execute(ctx); err == nil {
		t.Fatal("one-shot plan executed twice")
	}
	if _, err := p.ExecuteSQL(ctx, info, "DELETE FROM addp_acceptance.source", plugin.QueryOptions{ReadOnly: true}); err == nil {
		t.Fatal("readonly query accepted write")
	}
	short, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := p.ExecuteSQL(short, info, "SELECT pg_sleep(10)", plugin.QueryOptions{ReadOnly: true}); err == nil || short.Err() != context.DeadlineExceeded || time.Since(started) > 3*time.Second {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := p.ExecuteSQL(ctx, info, "SELECT 1", plugin.QueryOptions{ReadOnly: true}); err != nil {
		t.Fatalf("pool recovery: %v", err)
	}
}

func sameDecimal(got, want string) bool {
	g, ok := new(big.Rat).SetString(got)
	w, ok2 := new(big.Rat).SetString(want)
	return ok && ok2 && g.Cmp(w) == 0
}
