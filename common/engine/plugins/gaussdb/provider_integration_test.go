package gaussdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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

func TestIntegrationGaussDBGuardedTablePage(t *testing.T) {
	info := integrationInfo(t)
	p := &Plugin{}
	guarded, ok := any(p).(plugin.GuardedTableReadSessionProvider)
	if !ok {
		t.Fatal("native GaussDB guarded preview is not connected")
	}
	const engineID = uint(987643)
	ctx := plugin.WithPoolEngineVersion(t.Context(), engineID, 1)
	info = maps.Clone(info)
	info["pool_max_open_conns"], info["pool_max_idle_conns"] = 1, 1
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })
	path := plugin.TabularItemPath(engineID, "schema", "addp_acceptance", "source")
	opts := plugin.TableReadSessionOptions{Page: &plugin.CatalogTablePageOptions{Limit: 1, Predicate: `"id" = $1`, Args: []any{int64(9007199254740993)}}}
	checks := 0
	check := func(_ context.Context, set *plugin.QueryReadSet, fields []datatype.FieldInfo) error {
		checks++
		if set == nil || len(set.Paths) != 1 || !reflect.DeepEqual(set.Paths[0], path) || len(fields) != 4 ||
			fields[2].Precision != 38 || fields[2].Scale != 18 || !fields[0].PrimaryKey || !fields[1].PrimaryKey {
			t.Fatalf("native locked source/fields: %#v %#v", set, fields)
		}
		return nil
	}
	denied := errors.New("current source or live schema denied")
	if session, err := guarded.OpenGuardedTableReadSession(ctx, info, path, opts, func(c context.Context, set *plugin.QueryReadSet, fields []datatype.FieldInfo) error {
		_ = check(c, set, fields)
		return denied
	}); session != nil || !errors.Is(err, denied) {
		t.Fatalf("authorization refusal opened content: %v", err)
	}
	// Disagreeing name/id order proves that pagination uses the native
	// composite key (name,id), not column declaration order (id,name).
	if _, err := gaussDBFixtureSQL(t, `INSERT INTO addp_acceptance.source VALUES (9007199254740994,'A guarded order',0,NULL);`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := gaussDBFixtureSQL(t, `DELETE FROM addp_acceptance.source WHERE id=9007199254740994;`); err != nil {
			t.Error(err)
		}
	}()
	for offset, expected := range []int64{9007199254740994, 2} {
		page := plugin.TableReadSessionOptions{Page: &plugin.CatalogTablePageOptions{Limit: 1, Offset: int64(offset)}}
		s, e := guarded.OpenGuardedTableReadSession(ctx, info, path, page, check)
		if e != nil {
			t.Fatal(e)
		}
		batch, e := s.ReadBatch(ctx, 10)
		closeErr := s.Close(ctx)
		if e != nil || closeErr != nil || len(batch.Rows) != 1 || batch.Offset != int64(offset) || fmt.Sprint(batch.Rows[0]["id"]) != fmt.Sprint(expected) {
			t.Fatalf("native composite-key page %d: %#v read=%v close=%v", offset, batch, e, closeErr)
		}
	}
	if _, err := gaussDBFixtureSQL(t, `DELETE FROM addp_acceptance.source WHERE id=9007199254740994;`); err != nil {
		t.Fatal(err)
	}
	session, err := guarded.OpenGuardedTableReadSession(ctx, info, path, opts, check)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(context.Background())
	batch, err := session.ReadBatch(ctx, 10)
	if err != nil || len(batch.Rows) != 1 || fmt.Sprint(batch.Rows[0]["id"]) != "9007199254740993" ||
		fmt.Sprint(batch.Rows[0]["amount"]) != "12345678901234567890.123456789012345678" || batch.Rows[0]["name"] != "高斯正式接入中文" {
		t.Fatalf("bounded native page: %#v %v", batch, err)
	}
	set, err := session.(plugin.ReadSessionEvidence).ReadSet(ctx)
	if err != nil || set == nil || len(set.Paths) != 1 || !reflect.DeepEqual(set.Paths[0], path) {
		t.Fatalf("same-session read evidence: %#v %v", set, err)
	}
	// The same owned DDL must fail while the read lock is held, then succeed
	// after Close. Server statement_timeout prevents leaving a queued ALTER.
	const alter = `SET statement_timeout=150; ALTER TABLE addp_acceptance.source ADD COLUMN guarded_probe INT;`
	if _, err := gaussDBFixtureSQL(t, alter); err == nil {
		t.Fatal("native DDL replaced the authorized schema before session close")
	}
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := gaussDBFixtureSQL(t, alter+` ALTER TABLE addp_acceptance.source DROP COLUMN guarded_probe;`); err != nil {
		t.Fatalf("owned DDL control did not recover after close: %v", err)
	}
	pool, err := plugin.GetOrCreatePoolFromFactory(&plugin.Engine{ID: engineID, EngineType: p.Type(), Version: 1, ConnectionInfo: info}, nil)
	if err != nil {
		t.Fatal(err)
	}
	db, err := pool.DB()
	if err != nil {
		t.Fatal(err)
	}
	borrowed, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer borrowed.Close()
	wait, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	_, err = guarded.OpenGuardedTableReadSession(wait, info, path, opts, check)
	cancel()
	if plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeConnectionAcquisitionTimeout || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("native page bypassed shared pool budget: %v", err)
	}
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	opts.Page.Args = []any{int64(2)}
	session, err = guarded.OpenGuardedTableReadSession(ctx, info, path, opts, check)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(context.Background())
	batch, err = session.ReadBatch(ctx, 1)
	if err != nil || len(batch.Rows) != 1 || batch.Rows[0]["name"] != "保留尾空格  " || batch.Rows[0]["optional_text"] != nil {
		t.Fatalf("same-pool recovery lost native values: %#v %v", batch, err)
	}
	if err := session.Close(ctx); err != nil || db.Stats().InUse != 0 || db.PingContext(ctx) != nil {
		t.Fatalf("guarded reader closed or leaked shared pool: %v", err)
	}
	if _, err := gaussDBFixtureSQL(t, `CREATE VIEW addp_acceptance.guarded_view AS SELECT * FROM addp_acceptance.source; GRANT SELECT ON addp_acceptance.guarded_view TO fixture_reader;`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := gaussDBFixtureSQL(t, `DROP VIEW addp_acceptance.guarded_view;`); err != nil {
			t.Error(err)
		}
	}()
	before := checks
	view := plugin.TabularItemPath(engineID, "schema", "addp_acceptance", "guarded_view")
	if s, e := guarded.OpenGuardedTableReadSession(ctx, info, view, opts, check); s != nil || !errors.Is(e, plugin.ErrQueryReadSetUnresolved) || checks != before {
		t.Fatalf("view reached guarded authorization: %v", e)
	}
	if _, err := gaussDBFixtureSQL(t, `CREATE ROW LEVEL SECURITY POLICY guarded_policy ON addp_acceptance.source USING (id > 0);`); err != nil {
		t.Fatalf("native RLS control setup: %v", err)
	}
	defer func() {
		if _, err := gaussDBFixtureSQL(t, `DROP ROW LEVEL SECURITY POLICY IF EXISTS guarded_policy ON addp_acceptance.source;`); err != nil {
			t.Error(err)
		}
	}()
	if s, e := guarded.OpenGuardedTableReadSession(ctx, info, path, opts, check); s != nil || !errors.Is(e, plugin.ErrQueryReadSetUnresolved) || checks != before {
		t.Fatalf("native RLS table reached guarded authorization: %v", e)
	}
	if _, err := gaussDBFixtureSQL(t, `CREATE TABLE addp_acceptance.guarded_partition(id INT) PARTITION BY RANGE(id) (PARTITION p1 VALUES LESS THAN(100)); GRANT SELECT ON addp_acceptance.guarded_partition TO fixture_reader;`); err != nil {
		t.Fatalf("native partition control setup: %v", err)
	}
	defer func() {
		if _, err := gaussDBFixtureSQL(t, `DROP TABLE addp_acceptance.guarded_partition;`); err != nil {
			t.Error(err)
		}
	}()
	partition := plugin.TabularItemPath(engineID, "schema", "addp_acceptance", "guarded_partition")
	if s, e := guarded.OpenGuardedTableReadSession(ctx, info, partition, opts, check); s != nil || !errors.Is(e, plugin.ErrQueryReadSetUnresolved) || checks != before {
		t.Fatalf("native partition table reached guarded authorization: %v", e)
	}
}

// Reuse the fixture owner's label-checked local socket operation. No owner
// password is exported to Go or to the ordinary platform/browser account.
func gaussDBFixtureSQL(t *testing.T, statement string) (string, error) {
	t.Helper()
	script, err := filepath.Abs("../../../../business/scripts/online-gaussdb-consumer-fixture.py")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "python3", "-c", `import importlib.util,json,sys
spec=importlib.util.spec_from_file_location('owned_gaussdb_fixture',sys.argv[1])
module=importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
state=json.loads((module.directory()/'gaussdb-state.json').read_text())
print(module.sql(state,sys.stdin.read()))`, script)
	command.Stdin = strings.NewReader(statement)
	returnBytes, err := command.Output()
	return string(returnBytes), err
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
	t.Run("shared delegated query timeout stages", func(t *testing.T) {
		const engineID = 8416
		current := plugin.WithPoolEngineVersion(t.Context(), engineID, 1)
		connection := make(plugin.ConnectionInfo, len(info)+2)
		for key, value := range info {
			connection[key] = value
		}
		connection["pool_max_open_conns"], connection["pool_max_idle_conns"] = 1, 0
		t.Cleanup(func() { _ = plugin.ClosePool(engineID) })
		pool, err := plugin.GetOrCreatePoolFromFactory(&plugin.Engine{ID: engineID, EngineType: p.Type(), Version: 1, ConnectionInfo: connection}, nil)
		if err != nil {
			t.Fatal(err)
		}
		native, err := pool.DB()
		if err != nil {
			t.Fatal(err)
		}
		borrowed, err := native.Conn(current)
		if err != nil {
			t.Fatal(err)
		}
		defer borrowed.Close()
		opts := plugin.QueryOptions{EngineID: engineID, EngineType: p.Type(), ReadOnly: true}
		blocked, stop := context.WithTimeout(current, 150*time.Millisecond)
		_, err = p.ExecuteSQL(blocked, connection, "SELECT 1", opts)
		stop()
		if !errors.Is(err, context.DeadlineExceeded) || plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeConnectionAcquisitionTimeout {
			t.Fatalf("GaussDB pool wait lost its stage: %v (code=%q)", err, plugin.QueryErrorCodeOf(err))
		}
		if err := borrowed.Close(); err != nil {
			t.Fatal(err)
		}
		blocked, stop = context.WithTimeout(current, 150*time.Millisecond)
		_, err = p.ExecuteSQL(blocked, connection, "SELECT pg_sleep(10)", opts)
		stop()
		if !errors.Is(err, context.DeadlineExceeded) || plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeSQLExecutionTimeout {
			t.Fatalf("GaussDB SQL execution lost its stage: %v (code=%q)", err, plugin.QueryErrorCodeOf(err))
		}
		result, err := p.ExecuteSQL(current, connection, "SELECT current_setting('transaction_read_only') AS mode", opts)
		if err != nil || len(result.Rows) != 1 || result.Rows[0]["mode"] != "on" {
			t.Fatalf("GaussDB shared query did not recover its read-only transaction: %+v, %v", result, err)
		}
	})
}

func sameDecimal(got, want string) bool {
	g, ok := new(big.Rat).SetString(got)
	w, ok2 := new(big.Rat).SetString(want)
	return ok && ok2 && g.Cmp(w) == 0
}
