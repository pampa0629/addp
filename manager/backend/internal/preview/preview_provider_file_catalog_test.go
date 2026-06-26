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
	objectcontent.LoadObjectContentPlugins(provider.(*fileCatalogPreviewProvider).content, "../../plugins")

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

func TestFileCatalogImagePreviewUsesStorageStreamURL(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingFileCatalogPreviewPlugin{
		engineType:   "nfs",
		contentType:  "image/tiff",
		sizeBytes:    72 * 1024 * 1024,
		expectedPath: "/geotiff/srtm_40_01.tif",
	}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	contentRegistry := objectcontent.NewObjectContentRegistry()
	objectcontent.LoadObjectContentPlugins(contentRegistry, "../../plugins")
	provider := NewFileCatalogPreviewProvider(nil, contentRegistry)

	preview, err := provider.Preview(context.Background(), &PreviewRequest{
		Engine: &models.Engine{EngineType: "nfs", ID: 26},
		Schema: "geotiff",
		Table:  "srtm_40_01.tif",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "media",
				"format":    "tiff",
			},
		},
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.Object == nil || preview.Object.Content == nil {
		t.Fatalf("object content missing: %#v", preview)
	}
	content := preview.Object.Content
	if content.Kind != models.ObjectPreviewKindImage {
		t.Fatalf("content kind = %q, want image", content.Kind)
	}
	if content.URL == "" || content.PreviewMaterial != "url" || content.FrontendRenderer != models.ObjectPreviewKindImage {
		t.Fatalf("content = %#v, want URL material image preview", content)
	}
	if !strings.Contains(content.URL, "/api/v1/manager/storage-stream?") ||
		!strings.Contains(content.URL, "engine_id=26") ||
		!strings.Contains(content.URL, "storage_ref=geotiff%2Fsrtm_40_01.tif") {
		t.Fatalf("content URL = %q, want file storage stream URL", content.URL)
	}
	if preview.Object.URL != content.URL {
		t.Fatalf("object URL = %q, content URL = %q", preview.Object.URL, content.URL)
	}
	if enginePlugin.openContentCalls != 0 {
		t.Fatalf("OpenContent calls = %d, want 0 when image preview uses storage stream URL", enginePlugin.openContentCalls)
	}
}

type recordingFileCatalogPreviewPlugin struct {
	engineType        string
	openContentCalls  int
	describedItemPath plugin.CatalogPath
	contentType       string
	sizeBytes         int64
	expectedPath      string
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
func (p *recordingFileCatalogPreviewPlugin) ListChildren(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	return nil, nil
}
func (p *recordingFileCatalogPreviewPlugin) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	return nil, nil
}
func (p *recordingFileCatalogPreviewPlugin) DescribeCatalogFacts(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	p.describedItemPath = path
	now := time.Now()
	sizeBytes := int64(1024)
	if p.sizeBytes > 0 {
		sizeBytes = p.sizeBytes
	}
	contentType := p.contentType
	if contentType == "" {
		contentType = "application/vnd.sqlite3"
	}
	storagePath := path.StringPath()
	if p.expectedPath != "" {
		storagePath = p.expectedPath
	}
	return &plugin.CatalogFacts{
		Path: path,
		Kind: plugin.CatalogKindFile,
		Storage: &plugin.CatalogStorageFacts{
			Path:        storagePath,
			ContentType: contentType,
			SizeBytes:   &sizeBytes,
		},
		UpdatedAt: &now,
	}, nil
}
func (p *recordingFileCatalogPreviewPlugin) OpenContent(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	p.openContentCalls++
	return io.NopCloser(strings.NewReader("unused")), nil
}
