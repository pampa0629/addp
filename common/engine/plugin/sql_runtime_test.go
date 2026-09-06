package plugin

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeSQLRuntimeProvider struct {
	engineType  string
	lastSQL     string
	lastOptions QueryOptions
}

func (p *fakeSQLRuntimeProvider) Type() string {
	if p.engineType != "" {
		return p.engineType
	}
	return "postgresql"
}

func (p *fakeSQLRuntimeProvider) DisplayName() string { return "Fake SQL" }

func (p *fakeSQLRuntimeProvider) EngineOrigin() string { return "database" }

func (p *fakeSQLRuntimeProvider) DefaultPort() int { return 0 }

func (p *fakeSQLRuntimeProvider) RequiredFields() []string { return nil }

func (p *fakeSQLRuntimeProvider) SensitiveFields() []string          { return nil }
func (p *fakeSQLRuntimeProvider) ConnectionIdentityFields() []string { return []string{"host"} }

func (p *fakeSQLRuntimeProvider) Capabilities() EngineCapabilities { return EngineCapabilities{} }

func (p *fakeSQLRuntimeProvider) ValidateConnectionInfo(ConnectionInfo) error { return nil }

func (p *fakeSQLRuntimeProvider) TestConnection(context.Context, ConnectionInfo) error { return nil }

func (p *fakeSQLRuntimeProvider) QueryLanguages() []string { return []string{"sql"} }

func (p *fakeSQLRuntimeProvider) GenerateSampleQuery(context.Context, ConnectionInfo, SampleQueryOptions) (string, string) {
	return "", "sql"
}

func (p *fakeSQLRuntimeProvider) PrepareQuery(_ context.Context, conn ConnectionInfo, req QueryRequest) (PreparedQuery, error) {
	return PrepareSQLRuntimeQuery(p, conn, req, nil, nil)
}

func (p *fakeSQLRuntimeProvider) SQLDialect() string {
	if p.engineType == "oracle" {
		return "oracle"
	}
	return "postgresql"
}

func (p *fakeSQLRuntimeProvider) SupportsParameterizedQueries() bool { return true }

func (p *fakeSQLRuntimeProvider) ExecuteSQL(_ context.Context, _ ConnectionInfo, sql string, opts QueryOptions) (*QueryResult, error) {
	p.lastSQL = sql
	p.lastOptions = opts
	return &QueryResult{
		Columns: []string{"id"},
		Rows: []map[string]interface{}{
			{"id": 1},
		},
	}, nil
}

func TestReadSQLBatchPassesBoundParameters(t *testing.T) {
	provider := &fakeSQLRuntimeProvider{}
	_, err := ReadSQLBatch(context.Background(), provider, nil, EngineCatalogPath{}, BatchReadOptions{
		Query: "SELECT * FROM public.yanshi WHERE status = ?",
		Args:  []interface{}{"active"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.lastOptions.Args) != 1 || provider.lastOptions.Args[0] != "active" {
		t.Fatalf("query args = %#v", provider.lastOptions.Args)
	}
}

func TestBindSQLRuntimeParametersUsesDialectPlaceholderStyle(t *testing.T) {
	tests := []struct {
		dialect string
		wantSQL string
	}{
		{dialect: "postgresql", wantSQL: "SELECT * FROM members WHERE status = $1 AND score > $2"},
		{dialect: "oracle", wantSQL: "SELECT * FROM members WHERE status = :1 AND score > :2"},
		{dialect: "mysql", wantSQL: "SELECT * FROM members WHERE status = ? AND score > ?"},
		{dialect: "clickhouse", wantSQL: "SELECT * FROM members WHERE status = ? AND score > ?"},
	}
	for _, test := range tests {
		t.Run(test.dialect, func(t *testing.T) {
			bound, args, err := BindSQLRuntimeParameters(test.dialect,
				"SELECT * FROM members WHERE status = :status AND score > :score",
				QueryOptions{Parameters: map[string]interface{}{"status": "active", "score": 10}},
			)
			if err != nil {
				t.Fatal(err)
			}
			if bound != test.wantSQL {
				t.Fatalf("bound SQL = %q, want %q", bound, test.wantSQL)
			}
			if len(args) != 2 || args[0] != "active" || args[1] != 10 {
				t.Fatalf("args = %#v", args)
			}
		})
	}
}

func TestBindSQLRuntimeParametersRejectsMixedParameterModes(t *testing.T) {
	_, _, err := BindSQLRuntimeParameters("postgresql", "SELECT $1", QueryOptions{
		Args:       []interface{}{1},
		Parameters: map[string]interface{}{"value": 1},
	})
	if err == nil {
		t.Fatal("expected mixed positional and named parameters to fail")
	}
}

func TestPrepareSQLRuntimeQueryBindsOnceAndFailsClosedWithoutReadSetResolver(t *testing.T) {
	provider := &fakeSQLRuntimeProvider{}
	prepared, err := provider.PrepareQuery(t.Context(), ConnectionInfo{"host": "db"}, QueryRequest{
		EngineID: 9,
		Language: "sql",
		Query:    "SELECT * FROM members WHERE status = :status",
		Options: QueryOptions{
			ReadOnly:   true,
			Parameters: map[string]interface{}{"status": "active"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.ReadSet(t.Context()); !errors.Is(err, ErrQueryReadSetUnresolved) {
		t.Fatalf("ReadSet() error = %v", err)
	}
	analysis, err := prepared.Analysis(t.Context())
	if err != nil || analysis.SchemaCoverage != QuerySchemaCoverageUnknown || len(analysis.Diagnostics) != 0 {
		t.Fatalf("Analysis() = %#v, error = %v", analysis, err)
	}
	if _, err := prepared.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	if provider.lastSQL != "SELECT * FROM members WHERE status = $1" {
		t.Fatalf("executed SQL = %q", provider.lastSQL)
	}
	if provider.lastOptions.Parameters != nil || len(provider.lastOptions.Args) != 1 || provider.lastOptions.Args[0] != "active" {
		t.Fatalf("executed options = %#v", provider.lastOptions)
	}
}

func TestConsumeSQLPreparedQueryReturnsTheBoundOneShotRequest(t *testing.T) {
	provider := &fakeSQLRuntimeProvider{}
	prepared, err := provider.PrepareQuery(t.Context(), ConnectionInfo{"host": "db"}, QueryRequest{
		EngineID: 7, Language: "sql", Query: "SELECT :value AS value",
		Options: QueryOptions{ReadOnly: true, Parameters: map[string]interface{}{"value": "safe"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	connInfo, request, err := ConsumeSQLPreparedQuery(prepared, provider)
	if err != nil {
		t.Fatal(err)
	}
	if connInfo["host"] != "db" || request.Query != "SELECT $1 AS value" || len(request.Options.Args) != 1 || request.Options.Args[0] != "safe" {
		t.Fatalf("consumed SQL plan = conn:%#v request:%#v", connInfo, request)
	}
	if request.Options.Parameters != nil {
		t.Fatalf("consumed SQL parameters = %#v, want bound positional args only", request.Options.Parameters)
	}
	if _, _, err := ConsumeSQLPreparedQuery(prepared, provider); !errors.Is(err, ErrPreparedQueryConsumed) {
		t.Fatalf("second consume error = %v", err)
	}
	if _, err := prepared.Execute(t.Context()); !errors.Is(err, ErrPreparedQueryConsumed) {
		t.Fatalf("execute after session consume error = %v", err)
	}
}

func TestReadSQLBatchUsesOffsetForTablePath(t *testing.T) {
	provider := &fakeSQLRuntimeProvider{}
	path := EngineCatalogPath{
		Version:  EngineCatalogPathVersion,
		EngineID: 1,
		Segments: []EngineCatalogSegment{
			{Term: EngineCatalogTermSchema, Kind: EngineCatalogKindNamespace, Name: "public"},
			{Term: EngineCatalogTermTable, Kind: EngineCatalogKindTable, Name: "yanshi"},
		},
	}

	batch, err := ReadSQLBatch(context.Background(), provider, nil, path, BatchReadOptions{
		Limit:  100,
		Offset: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Offset != 200 {
		t.Fatalf("batch offset = %d, want 200", batch.Offset)
	}
	if !strings.Contains(provider.lastSQL, "LIMIT 100 OFFSET 200") {
		t.Fatalf("generated SQL = %q, want LIMIT/OFFSET", provider.lastSQL)
	}
}

func TestReadSQLBatchPaginatesCustomQuery(t *testing.T) {
	provider := &fakeSQLRuntimeProvider{}

	_, err := ReadSQLBatch(context.Background(), provider, nil, EngineCatalogPath{}, BatchReadOptions{
		Query:  "SELECT * FROM public.yanshi",
		Limit:  50,
		Offset: 150,
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.lastSQL != "SELECT * FROM (SELECT * FROM public.yanshi) AS addp_page LIMIT 50 OFFSET 150" {
		t.Fatalf("generated SQL = %q", provider.lastSQL)
	}
}

func TestReadSQLBatchUsesOraclePagination(t *testing.T) {
	provider := &fakeSQLRuntimeProvider{engineType: "oracle"}

	_, err := ReadSQLBatch(context.Background(), provider, nil, EngineCatalogPath{}, BatchReadOptions{
		Query:  "SELECT * FROM BUSINESS.ORDERS",
		Limit:  50,
		Offset: 150,
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.lastSQL != "SELECT * FROM (SELECT * FROM BUSINESS.ORDERS) addp_page OFFSET 150 ROWS FETCH NEXT 50 ROWS ONLY" {
		t.Fatalf("generated SQL = %q", provider.lastSQL)
	}
}

func TestCountCatalogItemRowsUsesExplicitCatalogPath(t *testing.T) {
	provider := &fakeSQLRuntimeProvider{}
	Register(provider)
	t.Cleanup(func() {
		Unregister(provider.Type())
	})

	count, err := CountEngineCatalogItemRows(context.Background(), &Engine{ID: 1, EngineType: provider.Type()}, TabularItemPath(1, EngineCatalogTermSchema, "public", "roads"))
	if err != nil {
		t.Fatalf("CountEngineCatalogItemRows() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if !strings.Contains(provider.lastSQL, `"public"."roads"`) {
		t.Fatalf("lastSQL = %s, want qualified table", provider.lastSQL)
	}

	_, err = CountEngineCatalogItemRows(context.Background(), &Engine{ID: 1, EngineType: provider.Type()}, EngineCatalogRootPath(TabularCatalogModel(EngineCatalogTermSchema), 1))
	if err == nil {
		t.Fatal("expected error for root-only path")
	}
}
