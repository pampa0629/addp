package preview

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
)

func TestDynamicSchemaCollectionPreviewUsesMetaRowCountForPagination(t *testing.T) {
	const engineType = "dynamic_preview_test"
	enginePlugin := &recordingDynamicSchemaPreviewPlugin{
		engineType: engineType,
		fields: []datatype.FieldInfo{
			{Name: "_id", Path: []string{"_id"}, Type: datatype.FieldTypeString, NativeType: "string"},
			{Name: "members.userInfo.nickName", Path: []string{"members", "userInfo", "nickName"}, Type: datatype.FieldTypeString, NativeType: "string"},
		},
		queryResult: &plugin.QueryResult{
			Columns: []string{"_id"},
			Rows: []map[string]interface{}{
				{"_id": "row-21"},
			},
		},
	}
	plugin.Register(enginePlugin)
	defer plugin.Unregister(engineType)

	metaRowCount := int64(2383)
	provider := NewDynamicSchemaCollectionPreviewProvider()
	result, err := provider.Preview(context.Background(), &PreviewRequest{
		Engine: &models.Engine{
			ID:         11,
			EngineType: engineType,
		},
		Schema:       "Outdoor",
		Table:        "Outdoors",
		Page:         2,
		PageSize:     20,
		ItemRowCount: &metaRowCount,
		ProviderPath: plugin.EngineCatalogPath{
			Version:  plugin.EngineCatalogPathVersion,
			EngineID: 11,
			Segments: []plugin.EngineCatalogSegment{
				{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer},
				{Term: plugin.EngineCatalogTermDatabase, Kind: plugin.EngineCatalogKindNamespace, Name: "Outdoor"},
				{Term: plugin.EngineCatalogTermCollection, Kind: plugin.EngineCatalogKindCollection, Name: "Outdoors"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if result.Total != 2383 {
		t.Fatalf("Total = %d, want 2383 from Meta row count", result.Total)
	}
	if len(enginePlugin.queryRequests) != 1 {
		t.Fatalf("query request count = %d, want 1", len(enginePlugin.queryRequests))
	}
	if len(enginePlugin.factsOptions) != 1 {
		t.Fatalf("catalog facts request count = %d, want 1", len(enginePlugin.factsOptions))
	}
	if got := enginePlugin.factsOptions[0]; got.SampleSize != 100 || got.IncludeStatistics {
		t.Fatalf("catalog facts options = %#v, want sample_size=100 without statistics", got)
	}

	var command map[string]interface{}
	if err := json.Unmarshal([]byte(enginePlugin.queryRequests[0].Query), &command); err != nil {
		t.Fatalf("decode query command: %v", err)
	}
	if command["skip"] != float64(20) || command["limit"] != float64(20) {
		t.Fatalf("query command = %#v, want skip=20 limit=20", command)
	}
	if len(result.ColumnMetadata) != 2 || result.ColumnMetadata[1].ColumnName != "members.userInfo.nickName" {
		t.Fatalf("column metadata = %#v", result.ColumnMetadata)
	}
	if got := result.ColumnMetadata[1].Path; len(got) != 3 || got[0] != "members" || got[2] != "nickName" {
		t.Fatalf("nested field path = %#v", got)
	}
}

type recordingDynamicSchemaPreviewPlugin struct {
	engineType    string
	fields        []datatype.FieldInfo
	queryResult   *plugin.QueryResult
	queryRequests []plugin.QueryRequest
	factsOptions  []plugin.EngineCatalogFactsOptions
}

func (p *recordingDynamicSchemaPreviewPlugin) Type() string         { return p.engineType }
func (p *recordingDynamicSchemaPreviewPlugin) DisplayName() string  { return p.engineType }
func (p *recordingDynamicSchemaPreviewPlugin) EngineOrigin() string { return "general" }
func (p *recordingDynamicSchemaPreviewPlugin) DefaultPort() int     { return 0 }
func (p *recordingDynamicSchemaPreviewPlugin) RequiredFields() []string {
	return nil
}
func (p *recordingDynamicSchemaPreviewPlugin) SensitiveFields() []string {
	return nil
}
func (p *recordingDynamicSchemaPreviewPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingDynamicSchemaPreviewPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingDynamicSchemaPreviewPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewDynamicSchemaCapabilities(p.engineType)
}
func (p *recordingDynamicSchemaPreviewPlugin) QueryLanguages() []string {
	return []string{"mql"}
}
func (p *recordingDynamicSchemaPreviewPlugin) GenerateSampleQuery(context.Context, plugin.ConnectionInfo, plugin.SampleQueryOptions) (string, string) {
	return "", "mql"
}
func (p *recordingDynamicSchemaPreviewPlugin) ExecuteRuntimeQuery(_ context.Context, _ plugin.ConnectionInfo, req plugin.QueryRequest) (*plugin.QueryResult, error) {
	p.queryRequests = append(p.queryRequests, req)
	return p.queryResult, nil
}
func (p *recordingDynamicSchemaPreviewPlugin) DescribeEngineCatalogFacts(_ context.Context, _ plugin.ConnectionInfo, _ plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	p.factsOptions = append(p.factsOptions, opts)
	estimatedRowCount := int64(2383)
	return &plugin.EngineCatalogFacts{
		Table: &datatype.TableInfo{Fields: p.fields, EstimatedRowCount: &estimatedRowCount},
	}, nil
}

var _ plugin.QueryRuntimeProvider = (*recordingDynamicSchemaPreviewPlugin)(nil)
var _ plugin.EngineCatalogFactsProvider = (*recordingDynamicSchemaPreviewPlugin)(nil)
