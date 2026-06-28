package preview

import (
	"context"
	"encoding/binary"
	"io"
	"math"
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

func TestFileCatalogGLBPreviewUsesModel3DStorageStreamURL(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingFileCatalogPreviewPlugin{
		engineType:   "nfs",
		contentType:  "model/gltf-binary",
		sizeBytes:    72 * 1024 * 1024,
		expectedPath: "/models/building.glb",
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
		Schema: "models",
		Table:  "building.glb",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "model_3d",
				"format":    "glb",
			},
			"type_info": map[string]interface{}{
				"model_3d": map[string]interface{}{
					"model_kind":   "mesh_scene",
					"vertex_count": int64(8),
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
	content := preview.Object.Content
	if content.Kind != models.ObjectPreviewKindModel3D || content.PreviewMaterial != "url" || content.FrontendRenderer != models.ObjectPreviewKindModel3D {
		t.Fatalf("content = %#v, want model_3d URL preview", content)
	}
	if content.URL == "" || !strings.Contains(content.URL, "storage_ref=models%2Fbuilding.glb") {
		t.Fatalf("content URL = %q, want file storage stream URL", content.URL)
	}
	if enginePlugin.openContentCalls != 0 {
		t.Fatalf("OpenContent calls = %d, want 0 when GLB preview uses storage stream URL", enginePlugin.openContentCalls)
	}
}

func TestFileCatalog3DTilesPreviewUsesManifestStorageStreamURL(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingFileCatalogPreviewPlugin{
		engineType:   "nfs",
		contentType:  "application/octet-stream",
		sizeBytes:    128,
		expectedPath: "/3d/mars3d-qx-dyt-3dtiles",
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
		Schema: "3d",
		Table:  "mars3d-qx-dyt-3dtiles",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "model_3d",
				"format":    "3dtiles",
				"layout":    "whole",
			},
			"type_info": map[string]interface{}{
				"model_3d": map[string]interface{}{
					"model_kind": "tiled_scene",
				},
			},
			"format_info": map[string]interface{}{
				"3dtiles": map[string]interface{}{
					"manifest_ref": "tileset.json",
					"tile_count":   int64(3),
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
	content := preview.Object.Content
	if content.Kind != models.ObjectPreviewKindModel3D || content.PreviewMaterial != "url" || content.FrontendRenderer != "3dtiles" {
		t.Fatalf("content = %#v, want 3dtiles URL preview", content)
	}
	if content.URL == "" || !strings.Contains(content.URL, "storage_ref=3d%2Fmars3d-qx-dyt-3dtiles%2Ftileset.json") {
		t.Fatalf("content URL = %q, want tileset.json storage stream URL", content.URL)
	}
	if preview.Object.StorageRef != "3d/mars3d-qx-dyt-3dtiles/tileset.json" {
		t.Fatalf("storage_ref = %q, want manifest path", preview.Object.StorageRef)
	}
	if preview.Object.ContentType != "application/vnd.ogc.3dtiles+json" {
		t.Fatalf("content_type = %q, want 3D Tiles MIME", preview.Object.ContentType)
	}
	if enginePlugin.openContentCalls != 0 {
		t.Fatalf("OpenContent calls = %d, want 0 when 3D Tiles preview uses storage stream URL", enginePlugin.openContentCalls)
	}
}

func TestFileCatalogLASPreviewReturnsPointCloudJSON(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingFileCatalogPreviewPlugin{
		engineType:   "nfs",
		contentType:  "application/vnd.las",
		sizeBytes:    int64(len(fileCatalogPreviewTestLASWithPoints())),
		expectedPath: "/point-cloud/site.las",
		content:      fileCatalogPreviewTestLASWithPoints(),
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
		Schema: "point-cloud",
		Table:  "site.las",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "point_cloud",
				"format":    "las",
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
	if content.Kind != models.ObjectPreviewKindPointCloud || content.PreviewMaterial != "json" || content.FrontendRenderer != models.ObjectPreviewKindPointCloud {
		t.Fatalf("content = %#v, want point_cloud JSON preview", content)
	}
	payload, ok := content.JSON.(map[string]interface{})
	if !ok {
		t.Fatalf("content.JSON = %#v, want map", content.JSON)
	}
	points, ok := payload["points"].([]map[string]interface{})
	if !ok || len(points) != 3 {
		t.Fatalf("points = %#v, want 3 sampled points", payload["points"])
	}
	if enginePlugin.openContentCalls == 0 {
		t.Fatal("OpenContent calls = 0, want stream read for LAS preview")
	}
}

type recordingFileCatalogPreviewPlugin struct {
	engineType        string
	openContentCalls  int
	describedItemPath plugin.CatalogPath
	contentType       string
	sizeBytes         int64
	expectedPath      string
	content           []byte
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
	if len(p.content) > 0 {
		return io.NopCloser(strings.NewReader(string(p.content))), nil
	}
	return io.NopCloser(strings.NewReader("unused")), nil
}

func fileCatalogPreviewTestLASWithPoints() []byte {
	const headerSize = 375
	const recordLength = 36
	pointCount := 3
	buf := make([]byte, headerSize+recordLength*pointCount)
	copy(buf[:4], []byte("LASF"))
	buf[24] = 1
	buf[25] = 4
	binary.LittleEndian.PutUint16(buf[94:96], uint16(headerSize))
	binary.LittleEndian.PutUint32(buf[96:100], headerSize)
	buf[104] = 7
	binary.LittleEndian.PutUint16(buf[105:107], recordLength)
	binary.LittleEndian.PutUint32(buf[107:111], uint32(pointCount))
	fileCatalogPreviewTestPutFloat64(buf[131:139], 0.01)
	fileCatalogPreviewTestPutFloat64(buf[139:147], 0.01)
	fileCatalogPreviewTestPutFloat64(buf[147:155], 0.01)
	fileCatalogPreviewTestPutFloat64(buf[155:163], 0)
	fileCatalogPreviewTestPutFloat64(buf[163:171], 0)
	fileCatalogPreviewTestPutFloat64(buf[171:179], 0)
	fileCatalogPreviewTestPutFloat64(buf[179:187], 7)
	fileCatalogPreviewTestPutFloat64(buf[187:195], 1)
	fileCatalogPreviewTestPutFloat64(buf[195:203], 8)
	fileCatalogPreviewTestPutFloat64(buf[203:211], 2)
	fileCatalogPreviewTestPutFloat64(buf[211:219], 9)
	fileCatalogPreviewTestPutFloat64(buf[219:227], 3)
	rawPoints := [][4]int32{
		{100, 200, 300, 11},
		{400, 500, 600, 22},
		{700, 800, 900, 33},
	}
	for i, point := range rawPoints {
		offset := headerSize + i*recordLength
		binary.LittleEndian.PutUint32(buf[offset:offset+4], uint32(point[0]))
		binary.LittleEndian.PutUint32(buf[offset+4:offset+8], uint32(point[1]))
		binary.LittleEndian.PutUint32(buf[offset+8:offset+12], uint32(point[2]))
		binary.LittleEndian.PutUint16(buf[offset+12:offset+14], uint16(point[3]))
	}
	return buf
}

func fileCatalogPreviewTestPutFloat64(target []byte, value float64) {
	binary.LittleEndian.PutUint64(target, math.Float64bits(value))
}
