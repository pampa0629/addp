package preview

import (
	"context"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
)

func TestGraphOverviewRowsUsesGraphInfo(t *testing.T) {
	t.Parallel()

	nodeCount := int64(3)
	relCount := int64(2)
	info := &datatype.GraphInfo{
		NodeShapes: []datatype.GraphNodeShapeInfo{{
			Name:       "Person",
			Properties: []datatype.FieldInfo{{Name: "name"}},
			Count:      &nodeCount,
		}},
		RelationshipShapes: []datatype.GraphRelationshipShapeInfo{{
			Type: "WORKS_FOR",
			Patterns: []datatype.GraphRelationshipPatternInfo{{
				From: datatype.GraphEndpointInfo{ShapeName: "Person"},
				To:   datatype.GraphEndpointInfo{ShapeName: "Company"},
			}},
			Count: &relCount,
		}},
	}

	columns, rows := graphOverviewRows(info)
	if !reflect.DeepEqual(columns, []string{"类型", "名称", "数量", "连接模式", "属性"}) {
		t.Fatalf("columns = %v", columns)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %v", rows)
	}
	if rows[0]["类型"] != "节点" || rows[0]["名称"] != "Person" || rows[0]["属性"] != "name" {
		t.Fatalf("node row = %#v", rows[0])
	}
	if rows[0]["__graph_sample_kind"] != "node_shape" {
		t.Fatalf("node row sample marker = %#v", rows[0])
	}
	if rows[1]["类型"] != "关系" || rows[1]["连接模式"] != "Person -> Company" {
		t.Fatalf("relationship row = %#v", rows[1])
	}
	if rows[1]["__graph_sample_kind"] != "relationship_shape" || rows[1]["__graph_relationship_type"] != "WORKS_FOR" {
		t.Fatalf("relationship row sample marker = %#v", rows[1])
	}
}

func TestGraphPreviewProviderFallsBackToGraphMetadataProvider(t *testing.T) {
	const engineType = "graph-preview-test"
	oldPlug, hadOld := plugin.Get(engineType)
	if hadOld == nil && oldPlug != nil {
		defer plugin.Register(oldPlug)
	} else {
		defer plugin.Unregister(engineType)
	}

	nodeCount := int64(1)
	graphPlug := &recordingGraphPreviewPlugin{
		engineType: engineType,
		graph: &datatype.GraphInfo{
			Model:     datatype.GraphModelPropertyGraph,
			NodeCount: &nodeCount,
			NodeShapes: []datatype.GraphNodeShapeInfo{{
				Name:  "Person",
				Count: &nodeCount,
			}},
		},
	}
	plugin.Register(graphPlug)

	provider := NewGraphPreviewProvider()
	preview, err := provider.Preview(context.Background(), &PreviewRequest{
		Engine: &models.Engine{
			ID:             42,
			EngineType:     engineType,
			ConnectionInfo: models.ConnectionInfo{"database": "neo4j"},
		},
		Schema: "neo4j",
		Table:  "graph",
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview == nil || len(preview.Rows) != 1 {
		t.Fatalf("preview rows = %#v", preview)
	}
	if len(graphPlug.paths) != 1 {
		t.Fatalf("DescribeGraph call count = %d, want 1", len(graphPlug.paths))
	}
	path := graphPlug.paths[0]
	if path.EngineID != 42 || len(path.Segments) != 2 || path.Segments[0].Name != "neo4j" || path.Segments[1].Term != plugin.CatalogTermGraph {
		t.Fatalf("DescribeGraph path = %#v", path)
	}
	if preview.Graph == nil || len(preview.Graph.Nodes) != 1 {
		t.Fatalf("preview graph sample = %#v", preview.Graph)
	}
}

func TestGraphPreviewProviderPassesGraphSampleFilter(t *testing.T) {
	const engineType = "graph-preview-filter-test"
	oldPlug, hadOld := plugin.Get(engineType)
	if hadOld == nil && oldPlug != nil {
		defer plugin.Register(oldPlug)
	} else {
		defer plugin.Unregister(engineType)
	}

	nodeCount := int64(1)
	graphPlug := &recordingGraphPreviewPlugin{
		engineType: engineType,
		graph: &datatype.GraphInfo{
			NodeShapes: []datatype.GraphNodeShapeInfo{{
				Name:  "Person",
				Count: &nodeCount,
			}},
		},
	}
	plugin.Register(graphPlug)

	provider := NewGraphPreviewProvider()
	_, err := provider.Preview(context.Background(), &PreviewRequest{
		Engine: &models.Engine{
			ID:             42,
			EngineType:     engineType,
			ConnectionInfo: models.ConnectionInfo{"database": "neo4j"},
		},
		Schema: "neo4j",
		Table:  "graph",
		GraphSample: map[string]interface{}{
			"kind":   "node_shape",
			"labels": []string{"Person"},
		},
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if len(graphPlug.sampleOpts) != 1 {
		t.Fatalf("SampleGraph call count = %d, want 1", len(graphPlug.sampleOpts))
	}
	if got := graphPlug.sampleOpts[0].Filter["kind"]; got != "node_shape" {
		t.Fatalf("sample filter = %#v", graphPlug.sampleOpts[0].Filter)
	}
}

func TestFlattenGraphEntityRowsIncludesEntityFields(t *testing.T) {
	t.Parallel()

	source := []map[string]interface{}{
		{
			"r": map[string]interface{}{
				"id":         "rel-1",
				"type":       "WORKS_AT",
				"properties": map[string]interface{}{},
			},
		},
	}

	columns, rows := flattenGraphEntityRows(source, "r")
	wantColumns := []string{"id", "type"}
	if !reflect.DeepEqual(columns, wantColumns) {
		t.Fatalf("columns = %v, want %v", columns, wantColumns)
	}
	if len(rows) != 1 || rows[0]["id"] != "rel-1" || rows[0]["type"] != "WORKS_AT" {
		t.Fatalf("rows = %v, want relationship identity fields", rows)
	}
}

type recordingGraphPreviewPlugin struct {
	engineType string
	graph      *datatype.GraphInfo
	paths      []plugin.CatalogPath
	sampleOpts []plugin.GraphSampleOptions
}

func (p *recordingGraphPreviewPlugin) Type() string {
	if p.engineType != "" {
		return p.engineType
	}
	return "graph-preview-test"
}
func (p *recordingGraphPreviewPlugin) DisplayName() string  { return "graph-preview-test" }
func (p *recordingGraphPreviewPlugin) EngineOrigin() string { return "general" }
func (p *recordingGraphPreviewPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingGraphPreviewPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingGraphPreviewPlugin) DefaultPort() int          { return 0 }
func (p *recordingGraphPreviewPlugin) RequiredFields() []string  { return nil }
func (p *recordingGraphPreviewPlugin) SensitiveFields() []string { return nil }
func (p *recordingGraphPreviewPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewGraphCapabilities(p.Type())
}
func (p *recordingGraphPreviewPlugin) DescribeGraph(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.MetadataOptions) (*datatype.GraphInfo, error) {
	p.paths = append(p.paths, path)
	return p.graph, nil
}

func (p *recordingGraphPreviewPlugin) SampleGraph(_ context.Context, _ plugin.ConnectionInfo, _ plugin.CatalogPath, opts plugin.GraphSampleOptions) (*plugin.GraphData, error) {
	p.sampleOpts = append(p.sampleOpts, opts)
	return &plugin.GraphData{
		Nodes: []plugin.GraphNode{{
			ElementId:  "node-1",
			Labels:     []string{"Person"},
			Properties: map[string]interface{}{"name": "Ada"},
		}},
	}, nil
}

var _ plugin.GraphMetadataProvider = (*recordingGraphPreviewPlugin)(nil)
var _ plugin.GraphSampleProvider = (*recordingGraphPreviewPlugin)(nil)
