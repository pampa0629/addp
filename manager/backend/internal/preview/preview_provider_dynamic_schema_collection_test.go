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
		ProviderPath: plugin.CatalogPath{
			Version:  plugin.CatalogPathVersion,
			EngineID: 11,
			Segments: []plugin.CatalogSegment{
				{Term: plugin.CatalogTermServer, Kind: plugin.CatalogTermServer},
				{Term: plugin.CatalogTermDatabase, Kind: plugin.CatalogKindNamespace, Name: "Outdoor"},
				{Term: plugin.CatalogTermCollection, Kind: plugin.CatalogKindCollection, Name: "Outdoors"},
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

	var command map[string]interface{}
	if err := json.Unmarshal([]byte(enginePlugin.queryRequests[0].Query), &command); err != nil {
		t.Fatalf("decode query command: %v", err)
	}
	if command["skip"] != float64(20) || command["limit"] != float64(20) {
		t.Fatalf("query command = %#v, want skip=20 limit=20", command)
	}
}

type recordingDynamicSchemaPreviewPlugin struct {
	engineType    string
	queryResult   *plugin.QueryResult
	queryRequests []plugin.QueryRequest
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
func (p *recordingDynamicSchemaPreviewPlugin) DescribeCatalogFacts(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	estimatedRowCount := int64(2383)
	return &plugin.CatalogFacts{
		Table: &datatype.TableInfo{EstimatedRowCount: &estimatedRowCount},
	}, nil
}

var _ plugin.QueryRuntimeProvider = (*recordingDynamicSchemaPreviewPlugin)(nil)
var _ plugin.CatalogFactsProvider = (*recordingDynamicSchemaPreviewPlugin)(nil)
