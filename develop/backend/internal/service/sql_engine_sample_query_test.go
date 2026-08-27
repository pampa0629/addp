package service

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
)

type executableSampleQueryProvider struct {
	query       string
	language    string
	result      *plugin.QueryResult
	executeErr  error
	sampleOpts  *plugin.SampleQueryOptions
	executedReq *plugin.QueryRequest
}

func (p *executableSampleQueryProvider) Type() string { return "develop_sample_query_test" }

func (p *executableSampleQueryProvider) DisplayName() string { return "Develop Sample Query Test" }

func (p *executableSampleQueryProvider) EngineOrigin() string { return "general" }

func (p *executableSampleQueryProvider) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}

func (p *executableSampleQueryProvider) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}

func (p *executableSampleQueryProvider) DefaultPort() int { return 0 }

func (p *executableSampleQueryProvider) RequiredFields() []string { return nil }

func (p *executableSampleQueryProvider) SensitiveFields() []string { return nil }

func (p *executableSampleQueryProvider) ConnectionIdentityFields() []string {
	return []string{"endpoint"}
}

func (p *executableSampleQueryProvider) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}

func (p *executableSampleQueryProvider) QueryLanguages() []string { return []string{"mql"} }

func (p *executableSampleQueryProvider) GenerateSampleQuery(_ context.Context, _ plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	p.sampleOpts = &opts
	return p.query, p.language
}

func (p *executableSampleQueryProvider) ExecuteRuntimeQuery(_ context.Context, _ plugin.ConnectionInfo, req plugin.QueryRequest) (*plugin.QueryResult, error) {
	p.executedReq = &req
	return p.result, p.executeErr
}

func TestGenerateExecutableSampleQueryRequiresSuccessfulNonEmptyExecution(t *testing.T) {
	tests := []struct {
		name       string
		result     *plugin.QueryResult
		executeErr error
		wantOK     bool
	}{
		{
			name:   "successful query with rows",
			result: &plugin.QueryResult{Rows: []map[string]interface{}{{"name": "Beijing"}}},
			wantOK: true,
		},
		{
			name:       "runtime execution failure",
			executeErr: errors.New("collection no longer exists"),
		},
		{
			name:   "empty result",
			result: &plugin.QueryResult{Rows: []map[string]interface{}{}},
		},
		{
			name: "nil runtime result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &executableSampleQueryProvider{
				query: "db.places.find({}).limit(10)", language: "mql",
				result: tt.result, executeErr: tt.executeErr,
			}
			plugin.Register(provider)
			t.Cleanup(func() { plugin.Unregister(provider.Type()) })

			query, language, err := generateExecutableSampleQuery(context.Background(), &commonModels.Engine{
				ID: 42, EngineType: provider.Type(), ConnectionInfo: map[string]interface{}{"endpoint": "test"},
			}, nil)
			if provider.executedReq == nil {
				t.Fatal("generated sample query was not executed")
			}
			if provider.executedReq.Query != provider.query || provider.executedReq.Language != provider.language {
				t.Fatalf("executed request = %#v", provider.executedReq)
			}
			if !provider.executedReq.Options.ReadOnly {
				t.Fatal("sample query execution was not read-only")
			}

			if tt.wantOK {
				if err != nil || query != provider.query || language != provider.language {
					t.Fatalf("generateExecutableSampleQuery() = (%q, %q, %v)", query, language, err)
				}
				return
			}
			if !errors.Is(err, ErrSampleQueryUnavailable) || query != "" || language != "" {
				t.Fatalf("generateExecutableSampleQuery() = (%q, %q, %v), want unavailable", query, language, err)
			}
		})
	}
}

func TestGenerateExecutableSampleQueryUsesSelectedCatalogPath(t *testing.T) {
	selectedPath := plugin.EngineCatalogPath{
		Version:  plugin.EngineCatalogPathVersion,
		EngineID: 42,
		Segments: []plugin.EngineCatalogSegment{
			{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer},
			{Term: plugin.EngineCatalogTermDatabase, Kind: plugin.EngineCatalogKindNamespace, Name: "business"},
			{Term: plugin.EngineCatalogTermCollection, Kind: plugin.EngineCatalogKindCollection, Name: "orders"},
		},
	}
	provider := &executableSampleQueryProvider{
		query:    `{"find":"orders","filter":{},"limit":10}`,
		language: "mql",
		result:   &plugin.QueryResult{Rows: []map[string]interface{}{{"id": 1}}},
	}
	plugin.Register(provider)
	t.Cleanup(func() { plugin.Unregister(provider.Type()) })

	query, language, err := generateExecutableSampleQuery(context.Background(), &commonModels.Engine{
		ID: 42, EngineType: provider.Type(), ConnectionInfo: map[string]interface{}{"endpoint": "test"},
	}, &selectedPath)
	if err != nil || query != provider.query || language != provider.language {
		t.Fatalf("generateExecutableSampleQuery() = (%q, %q, %v)", query, language, err)
	}
	if provider.sampleOpts == nil || provider.sampleOpts.Path.StringPath() != "business/orders" {
		t.Fatalf("sample options = %#v", provider.sampleOpts)
	}
	if provider.executedReq == nil || !provider.executedReq.Options.ReadOnly {
		t.Fatalf("executed request = %#v", provider.executedReq)
	}
	if provider.executedReq.TargetPath == nil || provider.executedReq.TargetPath.StringPath() != "business/orders" {
		t.Fatalf("executed target path = %#v", provider.executedReq.TargetPath)
	}
}

func TestGenerateExecutableSampleQueryReportsSelectedResourceEmpty(t *testing.T) {
	selectedPath := plugin.EngineCatalogPath{
		Version:  plugin.EngineCatalogPathVersion,
		EngineID: 42,
		Segments: []plugin.EngineCatalogSegment{
			{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer},
			{Term: plugin.EngineCatalogTermDatabase, Kind: plugin.EngineCatalogKindNamespace, Name: "business"},
			{Term: plugin.EngineCatalogTermCollection, Kind: plugin.EngineCatalogKindCollection, Name: "empty_orders"},
		},
	}
	provider := &executableSampleQueryProvider{
		query:    `{"find":"empty_orders","filter":{},"limit":10}`,
		language: "mql",
		result:   &plugin.QueryResult{Rows: []map[string]interface{}{}},
	}
	plugin.Register(provider)
	t.Cleanup(func() { plugin.Unregister(provider.Type()) })

	query, language, err := generateExecutableSampleQuery(context.Background(), &commonModels.Engine{
		ID: 42, EngineType: provider.Type(), ConnectionInfo: map[string]interface{}{"endpoint": "test"},
	}, &selectedPath)
	if !errors.Is(err, ErrSampleQueryResourceEmpty) || query != "" || language != "" {
		t.Fatalf("generateExecutableSampleQuery() = (%q, %q, %v), want selected resource empty", query, language, err)
	}
	if errors.Is(err, ErrSampleQueryUnavailable) {
		t.Fatalf("selected resource empty must not collapse into generic unavailable: %v", err)
	}
}
