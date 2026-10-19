package starrocks

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func integrationInfo(t *testing.T) plugin.ConnectionInfo {
	t.Helper()
	if os.Getenv("ADDP_STARROCKS_INTEGRATION") != "1" {
		t.Skip("use make test-common-starrocks")
	}
	encoded, err := os.ReadFile(filepath.Join(os.Getenv("ADDP_STARROCKS_ADMISSION_DIR"), "connection.json"))
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Password string `json:"reader_password"`
	}
	if json.Unmarshal(encoded, &value) != nil || value.Host != "127.0.0.1" || value.Port <= 0 || value.Password == "" {
		t.Fatal("private fixture descriptor required")
	}
	t.Cleanup(func() { _ = plugin.ClosePool(71) })
	return plugin.ConnectionInfo{"host": value.Host, "port": value.Port, "database": "addp_acceptance", "user": "fixture_reader", "password": value.Password}
}
func TestIntegrationProviderCatalogAndBatch(t *testing.T) {
	info := integrationInfo(t)
	p := &Plugin{}
	ctx := t.Context()
	probe, err := p.ProbeConnection(ctx, info)
	if err != nil || !certifiedVersion.MatchString(probe.ServerVersion) {
		t.Fatalf("probe: %+v %v", probe, err)
	}
	root := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), 71)
	entries, err := p.ListChildren(ctx, info, root, plugin.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if isSystemNamespace(e.Name) {
			t.Fatal("reserved database visible")
		}
		if e.Name == "addp_acceptance" {
			found = true
			if e.LeafCount == nil || *e.LeafCount != 1 {
				t.Fatalf("ordinary visible table count: %+v", e)
			}
		}
	}
	if !found {
		t.Fatal("business database missing")
	}
	entries, err = p.ListChildren(ctx, info, plugin.TabularNamespacePath(71, "database", "addp_acceptance"), plugin.ListOptions{})
	if err != nil || len(entries) != 1 || entries[0].Name != "source" {
		t.Fatalf("ordinary table visibility: %+v %v", entries, err)
	}
	path := plugin.TabularItemPath(71, "database", "addp_acceptance", "source")
	for _, name := range []string{"visible_view", "write_probe", "missing"} {
		hidden := plugin.TabularItemPath(71, "database", "addp_acceptance", name)
		if _, err := p.ResolvePath(ctx, info, hidden); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
			t.Fatalf("nonordinary/ungranted target %s resolved: %v", name, err)
		}
	}
	facts, err := p.DescribeEngineCatalogFacts(ctx, info, path, plugin.EngineCatalogFactsOptions{IncludeStatistics: true, IncludeConstraints: true, IncludeIndexes: true, IncludeSpatialFacts: true})
	if err != nil {
		t.Fatal(err)
	}
	if facts.Table == nil || !reflect.DeepEqual(facts.Table.PrimaryKey, []string{"name", "id"}) || len(facts.Table.Fields) != 4 || facts.Table.RowCount == nil || *facts.Table.RowCount != 2 {
		t.Fatalf("catalog facts: %+v", facts)
	}
	if facts.Table.Fields[2].Precision != 38 || facts.Table.Fields[2].Scale != 18 || !facts.Table.Fields[0].PrimaryKey || !facts.Table.Fields[1].PrimaryKey || len(facts.Constraints) != 0 || facts.Spatial != nil {
		t.Fatalf("native field facts: %+v", facts.Table)
	}
	batch, err := p.ReadBatch(ctx, info, path, plugin.BatchReadOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertExactRows(t, batch.Rows)
	if batch.Fields[2].Precision != 38 || batch.Fields[1].Type != datatype.FieldTypeBigInt {
		t.Fatalf("batch inferred fields instead of native schema: %+v", batch.Fields)
	}
	if _, err := p.ReadBatch(ctx, info, path, plugin.BatchReadOptions{Query: "SELECT name,id FROM addp_acceptance.visible_view"}); err == nil {
		t.Fatal("batch query escaped its physical table")
	}
}
func assertExactRows(t *testing.T, rows []map[string]interface{}) {
	t.Helper()
	if len(rows) != 2 {
		t.Fatalf("expected two rows: %+v", rows)
	}
	for _, r := range rows {
		switch fmt.Sprint(r["id"]) {
		case "9007199254740993":
			if r["name"] != "正式接入中文" || fmt.Sprint(r["amount"]) != "12345678901234567890.123456789012345678" || r["optional_text"] != nil {
				t.Fatalf("precise row changed: %+v", r)
			}
		case "2":
			actual, ok := new(big.Rat).SetString(fmt.Sprint(r["amount"]))
			want, _ := new(big.Rat).SetString("-0.000000000000000001")
			if !ok || actual.Cmp(want) != 0 || r["name"] != "保留尾空格  " || r["optional_text"] != "" {
				t.Fatalf("scale/empty/trailing spaces changed: %+v", r)
			}
		default:
			t.Fatalf("unexpected row: %+v", r)
		}
	}
}
func TestIntegrationProviderPreparedQueryAndNativeResultTypes(t *testing.T) {
	info := integrationInfo(t)
	p := &Plugin{}
	ctx := context.Background()
	prepared, err := p.PrepareQuery(ctx, info, plugin.QueryRequest{EngineID: 71, Language: "sql", Query: "SELECT name,id,amount,optional_text FROM source WHERE id>=:id ORDER BY id", Options: plugin.QueryOptions{ReadOnly: true, Parameters: map[string]interface{}{"id": int64(2)}}})
	if err != nil {
		t.Fatal(err)
	}
	readSet, err := prepared.ReadSet(ctx)
	if err != nil || len(readSet.Paths) != 1 || readSet.Paths[0].StringPath() != "addp_acceptance/source" {
		t.Fatalf("prepared source identity: %+v %v", readSet, err)
	}
	lineage, err := prepared.OutputLineage(ctx)
	if err != nil || len(lineage.Sources) != 1 {
		t.Fatalf("prepared field lineage: %+v %v", lineage, err)
	}
	result, err := prepared.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertExactRows(t, result.Rows)
	if result.Fields[1].Type != datatype.FieldTypeBigInt || result.Fields[2].Precision != 38 || result.Fields[2].Scale != 18 {
		t.Fatalf("result schema: %+v", result.Fields)
	}
	bound, err := p.PrepareQuery(ctx, info, plugin.QueryRequest{EngineID: 71, Language: "sql", Query: "SELECT id,name,amount,optional_text,CAST(:text AS CHAR(128)) AS bound_text FROM source WHERE id>=:minimum ORDER BY id", Options: plugin.QueryOptions{ReadOnly: true, Parameters: map[string]interface{}{"text": "中文' OR 1=1 --", "minimum": int64(3)}}})
	if err != nil {
		t.Fatal(err)
	}
	boundRead, err := bound.ReadSet(ctx)
	if err != nil || len(boundRead.Paths) != 1 || boundRead.Paths[0].StringPath() != "addp_acceptance/source" {
		t.Fatalf("bound source identity: %+v %v", boundRead, err)
	}
	boundResult, err := bound.Execute(ctx)
	if err != nil || len(boundResult.Rows) != 1 || boundResult.Rows[0]["bound_text"] != "中文' OR 1=1 --" {
		t.Fatalf("native text/integer bind changed: %+v %v", boundResult, err)
	}
	for _, target := range []struct{ p, s int }{{1, 0}, {9, 0}, {12, 2}, {18, 18}, {38, 0}, {38, 18}, {38, 38}} {
		query := fmt.Sprintf("SELECT CAST(NULL AS DECIMAL(%d,%d)) AS computed_amount FROM source WHERE id<0", target.p, target.s)
		result, err := p.ExecuteSQL(ctx, info, query, plugin.QueryOptions{ReadOnly: true})
		if err != nil || len(result.Rows) != 0 || len(result.Fields) != 1 || result.Fields[0].Type != datatype.FieldTypeDecimal || result.Fields[0].Precision != target.p || result.Fields[0].Scale != target.s {
			t.Fatalf("empty expression decimal(%d,%d): %+v %v", target.p, target.s, result, err)
		}
	}
	result, err = p.ExecuteSQL(ctx, info, "SELECT name FROM source WHERE name=?", plugin.QueryOptions{ReadOnly: true, Args: []interface{}{"' OR 1=1 --"}})
	if err != nil || len(result.Rows) != 0 {
		t.Fatal("parameter treated as SQL")
	}
}
