package tidb

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
	"github.com/addp/common/query/sqlcompile/conformance"
)

func TestIntegrationTiDBAnalyticalPlanUsesSharedCompiler(t *testing.T) {
	if os.Getenv("ADDP_TIDB_INTEGRATION") != "1" {
		t.Skip("set ADDP_TIDB_INTEGRATION=1 to run TiDB analytical integration test")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	p := &Plugin{}
	engineID := uint(95003)
	connInfo := tidbIntegrationConnInfo()
	database := connInfo["database"].(string)
	table := "addp_analytical_shared_compiler"
	path := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, database, table)
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })

	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS `"+table+"`"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE `"+table+"` (person_id VARCHAR(64) NOT NULL, member_id VARCHAR(64) NOT NULL, event_day DATE NOT NULL) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(context.Background(), "DROP TABLE IF EXISTS `"+table+"`")
	if _, err := db.ExecContext(ctx, "INSERT INTO `"+table+"` VALUES ('A','x','2026-01-01'),('A','x','2026-01-02'),('B','y','2026-01-03')"); err != nil {
		t.Fatal(err)
	}

	facts, err := p.DescribeEngineCatalogFacts(ctx, connInfo, path, plugin.EngineCatalogFactsOptions{})
	if err != nil || facts.Table == nil {
		t.Fatalf("DescribeEngineCatalogFacts() = %#v, %v", facts, err)
	}
	fields := make(map[string]datatype.FieldInfo, len(facts.Table.Fields))
	for _, field := range facts.Table.Fields {
		fields[field.Name] = field
	}
	selected := []string{"person_id", "member_id", "event_day"}
	columns := make([]plugin.ColumnBinding, 0, len(selected))
	scanFields := make([]datatype.FieldInfo, 0, len(selected))
	for _, name := range selected {
		field, ok := fields[name]
		if !ok {
			t.Fatalf("missing field %q in %#v", name, fields)
		}
		bindingField := field
		bindingField.Path = []string{field.Name}
		columns = append(columns, plugin.ColumnBinding{Column: name, Field: bindingField})
		scanFields = append(scanFields, datatype.FieldInfo{Name: field.Name, Type: field.Type, Nullable: field.Nullable, Precision: field.Precision, Scale: field.Scale})
	}

	planValue := plan.Plan{
		SchemaVersion:   plan.SchemaVersion,
		SemanticProfile: plan.SemanticProfile,
		Nodes: []plan.Node{
			{ID: "source", Op: "scan", Scan: &plan.Scan{Source: "fact", Fields: scanFields}},
			{ID: "members", Op: "project", Project: &plan.Project{Input: "source", Columns: []plan.Projection{
				{Name: "person_id", Expr: plan.Expr{Op: "column", Column: &plan.ColumnRef{Input: "source", Name: "person_id"}}},
				{Name: "member_id", Expr: plan.Expr{Op: "column", Column: &plan.ColumnRef{Input: "source", Name: "member_id"}}},
			}}},
			{ID: "unique", Op: "distinct", Distinct: &plan.Unary{Input: "members"}},
			{ID: "counted", Op: "aggregate", Aggregate: &plan.Aggregate{Input: "unique", Measures: []plan.Measure{{Name: "member_count", Op: "count_rows"}}}},
		},
		Root:   "counted",
		Output: plan.OutputContract{Fields: []datatype.FieldInfo{{Name: "member_count", Type: datatype.FieldTypeBigInt}}, StableKey: []string{"member_count"}},
	}
	if err := plan.Validate(planValue); err != nil {
		t.Fatal(err)
	}
	capabilities, err := p.ResolveCapabilities(ctx, connInfo, p.Capabilities())
	if err != nil {
		t.Fatal(err)
	}
	analytical := capabilities.Compute.Query.Analytical
	if analytical == nil || !analytical.Supported {
		t.Fatalf("TiDB analytical capability not certified: %#v", analytical)
	}
	compiled, err := p.AnalyticalCompiler().Compile(plugin.CompileRequest{
		Plan:    planValue,
		Sources: []plugin.SourceBinding{{Source: "fact", Path: path, Columns: columns}},
		Instance: plugin.AnalyticalInstance{
			EngineID: engineID,
			Capability: plugin.AnalyticalCapability{
				Supported:        analytical.Supported,
				PlanVersions:     analytical.PlanVersions,
				SemanticProfiles: analytical.SemanticProfiles,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := compiled.QueryRequest(nil, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := p.PrepareQuery(ctx, connInfo, request)
	if err != nil {
		t.Fatal(err)
	}
	result, err := prepared.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["member_count"] != int64(2) {
		t.Fatalf("analytical result = %#v, want member_count=2", result.Rows)
	}
}

// Run the same semantic matrix used by PostgreSQL and MySQL through TiDB's
// registered compiler, PreparedQuery bridge and instance certification.
func TestIntegrationTiDBAnalyticalConformance(t *testing.T) {
	if os.Getenv("ADDP_TIDB_INTEGRATION") != "1" {
		t.Skip("set ADDP_TIDB_INTEGRATION=1 to run TiDB analytical integration test")
	}
	provider := &Plugin{}
	info := tidbIntegrationConnInfo()
	for _, suite := range []struct {
		name string
		run  func(*testing.T, plugin.AnalyticalCompilerProvider, plugin.ConnectionInfo)
	}{
		{"integer", conformance.PreparedLosslessInteger},
		{"conditional", conformance.PreparedConditionals},
		{"arithmetic", conformance.PreparedArithmetic},
		{"expression", conformance.PreparedExpressions},
		{"calendar", conformance.PreparedCalendar},
		{"text", conformance.PreparedText},
		{"date buckets", conformance.PreparedDateBuckets},
		{"relations", conformance.PreparedRelations},
		{"result requests", conformance.PreparedResultRequests},
	} {
		t.Run(suite.name, func(t *testing.T) { suite.run(t, provider, info) })
	}
}
