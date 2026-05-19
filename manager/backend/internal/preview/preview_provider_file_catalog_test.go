package preview

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
)

func TestNFSPhysicalPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema string
		table  string
		want   string
	}{
		{name: "root", want: "/"},
		{name: "root file", table: "README.md", want: "/README.md"},
		{name: "directory", schema: "gis-data", want: "/gis-data"},
		{name: "nested file", schema: "gis-data", table: "sample.csv", want: "/gis-data/sample.csv"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := nfsPhysicalPath(tt.schema, tt.table); got != tt.want {
				t.Fatalf("nfsPhysicalPath(%q, %q) = %q, want %q", tt.schema, tt.table, got, tt.want)
			}
		})
	}
}

func TestFileCatalogPreviewUsesMetaContainerAttributes(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingFileCatalogPreviewPlugin{engineType: "nfs"}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	provider := NewFileCatalogPreviewProvider(nil, objectcontent.NewObjectContentRegistry())
	objectcontent.LoadObjectContentPlugins(provider.(*fileCatalogPreviewProvider).content, "../../plugins/manifest.json")

	preview, err := provider.Preview(context.Background(), &PreviewRequest{
		Engine: &models.Engine{EngineType: "nfs", ID: 7},
		Schema: "gis-data",
		Table:  "sample.sqlite",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "container",
				"format":    "sqlite",
			},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"child_count": int64(2),
					"children": []interface{}{
						map[string]interface{}{"name": "cities", "table": "cities", "child_kind": "table", "data_type": "table", "row_count": int64(3), "column_count": int64(2)},
						map[string]interface{}{"name": "roads", "table": "roads", "child_kind": "table", "data_type": "table", "row_count": int64(4), "column_count": int64(5)},
					},
				},
			},
			"format_info": map[string]interface{}{
				"sqlite": map[string]interface{}{
					"table_count":      int64(2),
					"sampled_children": int64(2),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.Object == nil || preview.Object.Content == nil {
		t.Fatalf("object content missing: %#v", preview)
	}
	if preview.Object.Content.Kind != models.ObjectPreviewKindContainer {
		t.Fatalf("content kind = %q, want container", preview.Object.Content.Kind)
	}
	if preview.Object.Content.FrontendRenderer != models.ObjectPreviewKindContainer {
		t.Fatalf("frontend renderer = %q, want container", preview.Object.Content.FrontendRenderer)
	}
	if preview.Object.Content.Metadata["frontend_renderer"] != models.ObjectPreviewKindContainer {
		t.Fatalf("metadata frontend renderer = %#v, want container", preview.Object.Content.Metadata["frontend_renderer"])
	}
	jsonMap, ok := preview.Object.Content.JSON.(map[string]interface{})
	if !ok {
		t.Fatalf("content json = %#v, want map", preview.Object.Content.JSON)
	}
	children, ok := jsonMap["children"].([]map[string]interface{})
	if !ok || len(children) != 2 {
		t.Fatalf("children = %#v, want 2 children", jsonMap["children"])
	}
	if enginePlugin.openContentCalls != 0 {
		t.Fatalf("OpenContent calls = %d, want 0 when meta container attrs are available", enginePlugin.openContentCalls)
	}
	if _, ok := children[0]["columns"]; ok {
		t.Fatalf("container child should not carry columns: %#v", children[0])
	}
}

type recordingFileCatalogPreviewPlugin struct {
	engineType        string
	openContentCalls  int
	describedItemPath plugin.CatalogPath
}

func (p *recordingFileCatalogPreviewPlugin) Type() string         { return p.engineType }
func (p *recordingFileCatalogPreviewPlugin) DisplayName() string  { return p.engineType }
func (p *recordingFileCatalogPreviewPlugin) EngineOrigin() string { return "general" }
func (p *recordingFileCatalogPreviewPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingFileCatalogPreviewPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingFileCatalogPreviewPlugin) DefaultPort() int          { return 0 }
func (p *recordingFileCatalogPreviewPlugin) RequiredFields() []string  { return nil }
func (p *recordingFileCatalogPreviewPlugin) SensitiveFields() []string { return nil }
func (p *recordingFileCatalogPreviewPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (p *recordingFileCatalogPreviewPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (p *recordingFileCatalogPreviewPlugin) ListChildren(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ListOptions) ([]plugin.CatalogNode, error) {
	return nil, nil
}
func (p *recordingFileCatalogPreviewPlugin) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.CatalogPath) (*plugin.CatalogNode, error) {
	return nil, nil
}
func (p *recordingFileCatalogPreviewPlugin) DescribeItem(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.MetadataOptions) (*plugin.ItemMetadata, error) {
	p.describedItemPath = path
	now := time.Now()
	return &plugin.ItemMetadata{
		Path: path,
		Kind: plugin.CatalogKindFile,
		Stats: map[string]interface{}{
			"size_bytes": int64(1024),
		},
		Attributes: map[string]interface{}{
			"path":         path.StringPath(),
			"content_type": "application/vnd.sqlite3",
		},
		UpdatedAt: &now,
	}, nil
}
func (p *recordingFileCatalogPreviewPlugin) OpenContent(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	p.openContentCalls++
	return io.NopCloser(strings.NewReader("unused")), nil
}
