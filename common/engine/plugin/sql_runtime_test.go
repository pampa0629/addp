package plugin

import (
	"context"
	"strings"
	"testing"
)

type fakeSQLRuntimeProvider struct {
	lastSQL string
}

func (p *fakeSQLRuntimeProvider) Type() string { return "postgresql" }

func (p *fakeSQLRuntimeProvider) DisplayName() string { return "Fake SQL" }

func (p *fakeSQLRuntimeProvider) EngineOrigin() string { return "database" }

func (p *fakeSQLRuntimeProvider) DefaultPort() int { return 0 }

func (p *fakeSQLRuntimeProvider) RequiredFields() []string { return nil }

func (p *fakeSQLRuntimeProvider) SensitiveFields() []string { return nil }

func (p *fakeSQLRuntimeProvider) Capabilities() EngineCapabilities { return EngineCapabilities{} }

func (p *fakeSQLRuntimeProvider) ValidateConnectionInfo(ConnectionInfo) error { return nil }

func (p *fakeSQLRuntimeProvider) TestConnection(context.Context, ConnectionInfo) error { return nil }

func (p *fakeSQLRuntimeProvider) QueryLanguages() []string { return []string{"sql"} }

func (p *fakeSQLRuntimeProvider) GenerateSampleQuery(context.Context, ConnectionInfo, SampleQueryOptions) (string, string) {
	return "", "sql"
}

func (p *fakeSQLRuntimeProvider) ExecuteRuntimeQuery(context.Context, ConnectionInfo, QueryRequest) (*QueryResult, error) {
	return nil, nil
}

func (p *fakeSQLRuntimeProvider) SQLDialect() string { return "postgresql" }

func (p *fakeSQLRuntimeProvider) ExecuteSQL(_ context.Context, _ ConnectionInfo, sql string, _ QueryOptions) (*QueryResult, error) {
	p.lastSQL = sql
	return &QueryResult{
		Columns: []string{"id"},
		Rows: []map[string]interface{}{
			{"id": 1},
		},
	}, nil
}

func TestReadSQLBatchUsesOffsetForTablePath(t *testing.T) {
	provider := &fakeSQLRuntimeProvider{}
	path := CatalogPath{
		Version:  CatalogPathVersion,
		EngineID: 1,
		Segments: []CatalogSegment{
			{Term: CatalogTermSchema, Kind: CatalogKindNamespace, Name: "public"},
			{Term: CatalogTermTable, Kind: CatalogKindTable, Name: "yanshi"},
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

	_, err := ReadSQLBatch(context.Background(), provider, nil, CatalogPath{}, BatchReadOptions{
		Query:  "SELECT * FROM public.yanshi",
		Limit:  50,
		Offset: 150,
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.lastSQL != "SELECT * FROM public.yanshi LIMIT 50 OFFSET 150" {
		t.Fatalf("generated SQL = %q", provider.lastSQL)
	}
}

func TestCountCatalogItemRowsUsesExplicitCatalogPath(t *testing.T) {
	provider := &fakeSQLRuntimeProvider{}
	Register(provider)
	t.Cleanup(func() {
		Unregister(provider.Type())
	})

	count, err := CountCatalogItemRows(context.Background(), &Engine{ID: 1, EngineType: provider.Type()}, TabularItemPath(1, CatalogTermSchema, "public", "roads"))
	if err != nil {
		t.Fatalf("CountCatalogItemRows() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if !strings.Contains(provider.lastSQL, `"public"."roads"`) {
		t.Fatalf("lastSQL = %s, want qualified table", provider.lastSQL)
	}

	_, err = CountCatalogItemRows(context.Background(), &Engine{ID: 1, EngineType: provider.Type()}, CatalogRootPath(TabularCatalogModel(CatalogTermSchema), 1))
	if err == nil {
		t.Fatal("expected error for root-only path")
	}
}
