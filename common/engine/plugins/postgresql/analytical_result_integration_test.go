package postgresql

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
	"github.com/addp/common/query/sqlcompile"
)

// This fixture compiles only the test's fixed DAG. It exercises the real query
// provider and result component; it is not a production analytical compiler.
type resultIntegrationCompiler struct{ table string }

func (c resultIntegrationCompiler) Identity() plugin.CompilerIdentity {
	return plugin.CompilerIdentity{ID: "postgres.result_fixture", Version: "1"}
}
func (c resultIntegrationCompiler) Check(r plugin.CompileRequest) (plugin.SupportReport, error) {
	return plugin.SupportReport{Supported: true}, r.Validate()
}
func (c resultIntegrationCompiler) Compile(r plugin.CompileRequest) (plugin.CompiledQuery, error) {
	if err := r.Validate(); err != nil {
		return plugin.CompiledQuery{}, err
	}
	relations := map[plan.NodeID]string{"source": "src", "violations": "bad", "filtered": "filtered", "ordered": "ordered", "limited": "limited"}
	query, err := sqlcompile.RenderResult(r.Plan, relations, []plan.SortKey{{Name: "value", Direction: "asc", Nulls: "last"}}, analyticalResultDialect{}, nil)
	if err != nil {
		return plugin.CompiledQuery{}, err
	}
	query = `WITH src AS (SELECT value FROM "common_pg_it".` + analyticalResultDialect{}.QuoteIdentifier(c.table) + `),
bad AS (SELECT value FROM src WHERE value < 0),
filtered AS (SELECT value FROM src WHERE value > :minimum),
ordered AS (SELECT value FROM filtered ORDER BY value ASC NULLS LAST),
limited AS (SELECT value FROM ordered ORDER BY value ASC NULLS LAST LIMIT 1) ` + query
	return plugin.NewCompiledQuery(r, c.Identity(), "sql", query, nil)
}

type analyticalResultIntegrationProvider struct {
	*PostgreSQLPlugin
	compiler resultIntegrationCompiler
}

func (p *analyticalResultIntegrationProvider) AnalyticalCompiler() plugin.AnalyticalCompiler {
	return p.compiler
}
func (p *analyticalResultIntegrationProvider) PrepareQuery(_ context.Context, conn plugin.ConnectionInfo, req plugin.QueryRequest) (plugin.PreparedQuery, error) {
	return plugin.PrepareSQLRuntimeQuery(p, conn, req, p.resolvePreparedQueryReadSet, p.resolvePreparedQueryOutputLineage)
}

func TestIntegrationPostgresAnalyticalResultAssertions(t *testing.T) {
	db, pg, conn := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	ctx := t.Context()
	table := fmt.Sprintf("analytical_result_%d", time.Now().UnixNano())
	createPostgresPrepareBaseTable(t, ctx, db, "common_pg_it", table, `value bigint NOT NULL PRIMARY KEY`)
	defer dropPostgresPrepareTable(db, "common_pg_it", table)
	quoted := `"common_pg_it".` + analyticalResultDialect{}.QuoteIdentifier(table)
	if _, err := db.ExecContext(ctx, "INSERT INTO "+quoted+" VALUES (-1),(2),(3)"); err != nil {
		t.Fatal(err)
	}
	engineID := uint(time.Now().UnixNano())
	defer plugin.ClosePool(engineID)
	fields := []datatype.FieldInfo{{Name: "value", Type: datatype.FieldTypeBigInt}}
	column := func(input plan.NodeID) plan.Expr {
		return plan.Expr{Op: "column", Column: &plan.ColumnRef{Input: input, Name: "value"}}
	}
	p := plan.Plan{SchemaVersion: plan.SchemaVersion, SemanticProfile: plan.SemanticProfile, Root: "limited",
		Parameters: []plan.Parameter{{Name: "minimum", Type: datatype.FieldTypeBigInt, Required: true}},
		Output:     plan.OutputContract{Fields: fields, StableKey: []string{"value"}},
		Assertions: []plan.Assertion{{Violation: "violations", Code: "negative_value"}},
		Nodes: []plan.Node{
			{ID: "source", Op: "scan", Scan: &plan.Scan{Source: "numbers", Fields: fields}},
			{ID: "violations", Op: "filter", Filter: &plan.Filter{Input: "source", Predicate: plan.Expr{Op: "lt", Args: []plan.Expr{column("source"), {Op: "literal", Literal: &plan.Literal{Type: datatype.FieldTypeBigInt, Text: "0"}}}}}},
			{ID: "filtered", Op: "filter", Filter: &plan.Filter{Input: "source", Predicate: plan.Expr{Op: "gt", Args: []plan.Expr{column("source"), {Op: "parameter", Parameter: "minimum"}}}}},
			{ID: "ordered", Op: "sort", Sort: &plan.Sort{Input: "filtered", Keys: []plan.SortKey{{Name: "value", Direction: "asc", Nulls: "last"}}}},
			{ID: "limited", Op: "limit", Limit: &plan.Limit{Input: "ordered", Count: 1}},
		},
	}
	path := postgresPrepareTablePath("common_pg_it", table)
	path.EngineID = engineID
	path.Segments = append([]plugin.EngineCatalogSegment{{Term: "server", Kind: "server"}}, path.Segments...)
	r := plugin.CompileRequest{Plan: p, Instance: plugin.AnalyticalInstance{EngineID: engineID, Capability: plugin.AnalyticalCapability{Supported: true, PlanVersions: []string{plan.SchemaVersion}, SemanticProfiles: []string{plan.SemanticProfile}}},
		Sources: []plugin.SourceBinding{{Source: "numbers", Path: path, Columns: []plugin.ColumnBinding{{Column: "value", Field: datatype.FieldInfo{Name: "value", Path: []string{"value"}, Type: datatype.FieldTypeBigInt, NativeType: "bigint"}}}}},
	}
	provider := &analyticalResultIntegrationProvider{PostgreSQLPlugin: pg, compiler: resultIntegrationCompiler{table: table}}
	compiled, err := provider.compiler.Compile(r)
	if err != nil {
		t.Fatal(err)
	}
	run := func(minimum string) (*plugin.QueryResult, error) {
		req, err := compiled.QueryRequest(map[string]plan.Literal{"minimum": {Type: datatype.FieldTypeBigInt, Text: minimum}}, time.Second*10)
		if err != nil {
			t.Fatal(err)
		}
		prepared, err := provider.PrepareQuery(ctx, conn, req)
		if err != nil {
			t.Fatal(err)
		}
		readSet, err := prepared.ReadSet(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(readSet.Paths) != 1 {
			t.Fatalf("source dependency lost: %#v", readSet)
		}
		lineage, err := prepared.OutputLineage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := plugin.ValidateQueryOutputLineage(readSet, lineage); err != nil {
			t.Fatal(err)
		}
		return prepared.Execute(ctx)
	}
	for _, minimum := range []string{"100", "0"} {
		out, err := run(minimum)
		if !errors.Is(err, plugin.ErrAnalyticalAssertion) || out != nil {
			t.Fatalf("minimum=%s result=%#v err=%v", minimum, out, err)
		}
		var assertion *plugin.AnalyticalAssertionError
		if !errors.As(err, &assertion) || assertion.Code != "negative_value" {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM "+quoted+" WHERE value < 0"); err != nil {
		t.Fatal(err)
	}
	out, err := run("100")
	if err != nil || len(out.Rows) != 0 {
		t.Fatalf("empty success: %#v %v", out, err)
	}
	out, err = run("0")
	if err != nil || len(out.Rows) != 1 || out.Rows[0]["value"] != int64(2) {
		t.Fatalf("paged success: %#v %v", out, err)
	}
}
