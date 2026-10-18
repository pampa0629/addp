package preview

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
	"github.com/addp/manager/internal/repository"
)

type objectContainerPreviewPlugin struct {
	recordingFileCatalogPreviewPlugin
}

func (p *objectContainerPreviewPlugin) DescribeEngineCatalogFacts(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, options plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	facts, err := p.recordingFileCatalogPreviewPlugin.DescribeEngineCatalogFacts(ctx, conn, path, options)
	if facts != nil {
		facts.Kind = plugin.EngineCatalogKindObject
	}
	return facts, err
}

func TestObjectCatalogPreviewUsesScannedContainerIndexBeforeContentHandler(t *testing.T) {
	for _, source := range []struct{ format, filename, contentType string }{
		{"kml", "sites.kml", "application/vnd.google-earth.kml+xml"},
		{"gml", "sites.gml", "application/gml+xml"},
		{"topojson", "projected.topojson", "application/json"},
		{"kml", "renamed.data", "application/octet-stream"},
	} {
		t.Run(source.filename, func(t *testing.T) {
			storage := &objectContainerPreviewPlugin{recordingFileCatalogPreviewPlugin{
				engineType: "object-container-preview-test", contentType: source.contentType, sizeBytes: 521,
			}}
			plugin.Register(storage)
			t.Cleanup(func() { plugin.Unregister(storage.Type()) })
			attrs := map[string]interface{}{
				"item": map[string]interface{}{"data_type": "container", "format": source.format, "layout": "single"},
				"type_info": map[string]interface{}{"container": map[string]interface{}{
					"child_count": 2, "default_child": "sites", "resource_count": 1,
					"children": []interface{}{
						map[string]interface{}{"name": "sites", "child_kind": "feature_class", "data_type": "table", "row_count": 2,
							"native": map[string]interface{}{"table": "sites", "geometry_column": "position"}},
						map[string]interface{}{"name": "roads", "child_kind": "feature_class", "data_type": "table", "row_count": 1,
							"native": map[string]interface{}{"table": "roads", "geometry_column": "shape"}},
					},
				}},
			}
			key := "vectors/" + source.filename
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("engine_id") != "7" || r.URL.Query().Get("catalog_path") != "source/"+key {
					t.Errorf("unexpected Meta lookup: %s", r.URL)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if r.URL.Path == "/api/v1/meta/nodes/by-catalog-path" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if r.URL.Path != "/api/v1/meta/items/by-catalog-path" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"id": 42, "engine_id": 7, "tenant_id": 1, "item_type": "object", "name": source.filename,
					"full_name": "source/" + key, "size_bytes": 521, "attributes": attrs,
				})
			}))
			t.Cleanup(server.Close)
			meta := client.NewMetaClient(server.URL, client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
				return "metadata-test-token", nil
			}))
			registry := objectcontent.NewObjectContentRegistry()
			objectcontent.LoadObjectContentPlugins(registry, "../../plugins")
			provider := NewObjectCatalogPreviewProvider(repository.NewMetadataRepository(nil), meta, registry)
			result, err := provider.Preview(t.Context(), &PreviewRequest{
				Engine: &models.Engine{ID: 7, EngineType: storage.Type()}, Schema: "source", Table: key,
				TenantID: uintPtr(1), Locator: "addp://engine/7/path/source/" + key + "?type=object&item_id=42",
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Object == nil || result.Object.Content == nil {
				t.Fatalf("scanned container index missing: %#v", result)
			}
			content := result.Object.Content
			if content.Kind != models.ObjectPreviewKindContainer || content.FrontendRenderer != models.ObjectPreviewKindContainer || content.PreviewMaterial != models.PreviewMaterialContainer {
				t.Fatalf("content must use the shared container renderer: %#v", content)
			}
			index, ok := content.JSON.(map[string]interface{})
			if !ok {
				t.Fatalf("container index is not an object: %#v", content.JSON)
			}
			children, ok := index["children"].([]map[string]interface{})
			if !ok || len(children) != 2 || children[0]["name"] != "sites" || children[1]["name"] != "roads" {
				t.Fatalf("scanned layers were lost: %#v", index)
			}
			if storage.openContentCalls != 0 || storage.listChildrenCalls != 0 {
				t.Fatalf("container index reread source bytes or listed storage: reads=%d lists=%d", storage.openContentCalls, storage.listChildrenCalls)
			}
			if result.Object.StorageRef != "source/"+key || result.Object.SizeBytes != 521 || result.Object.Download == nil {
				t.Fatalf("source download facts changed: %#v", result.Object)
			}
		})
	}
}
