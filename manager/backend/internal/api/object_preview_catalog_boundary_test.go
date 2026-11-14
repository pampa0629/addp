package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/nfs"
	"github.com/addp/manager/internal/objectcontent"
	"github.com/addp/manager/internal/preview"
	"github.com/gin-gonic/gin"
)

// Keep the real file-catalog provider and image handler in this regression.
// Only the owner HTTP facts, current authorization observation and source I/O
// are fixtures; this does not grant access or render a TIFF in a browser.
func TestNFSTIFFObjectPreviewCatalogPermissionTransitions(t *testing.T) {
	source := &objectPreviewCatalogSource{}
	previous, previousErr := plugin.Get("nfs")
	plugin.Register(source)
	t.Cleanup(func() {
		if previousErr == nil {
			plugin.Register(previous)
		} else {
			plugin.Unregister("nfs")
		}
	})

	attributes := map[string]interface{}{
		"schema_version": 1,
		"item": map[string]interface{}{
			"data_type": "media", "format": "tiff", "layout": "single",
			"future_item_fact": "private-item-fact",
		},
		"storage": map[string]interface{}{
			"physical_path": "images/private.tif", "total_size": 1844,
			"content_versions": []map[string]interface{}{{"path": "images/private.tif", "sha256": "private-content-digest", "size_bytes": 1844}},
		},
		"type_info":        map[string]interface{}{"media": map[string]interface{}{"kind": "image", "width": 240, "height": 120}},
		"format_info":      map[string]interface{}{"tiff": map[string]interface{}{"page_count": 1, "pages": []map[string]interface{}{{"ifd_index": 0, "width": 240, "height": 120}}}},
		"future_partition": map[string]interface{}{"summary": "private-future-fact"},
	}
	item, err := json.Marshal(map[string]interface{}{
		"id": 1, "tenant_id": 1, "engine_id": 9, "item_type": "file", "name": "private.tif", "full_name": "images/private.tif",
		"scanned_depth": "deep", "size_bytes": 1844, "row_count": 12345, "attributes": attributes,
	})
	if err != nil {
		t.Fatal(err)
	}
	content := objectcontent.NewObjectContentRegistry()
	objectcontent.LoadObjectContentPlugins(content, "")
	registry := preview.NewPreviewRegistry()
	registry.Register(preview.NewFileCatalogPreviewProvider(nil, content))
	resolver := preview.NewPreviewResolver(registry, apiDownloadTestSystemClient(t, 9, "nfs"), apiDownloadTestMetaItemClient(t, string(item)))
	observation := &objectVisibilityFixture{}
	handler := NewExplorerHandler(nil, resolver, nil, nil, nil)
	handler.SetObjectPreviewChecker(observation)
	router := gin.New()
	router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 1, 33); c.Next() })
	router.GET("/preview", handler.Preview)
	const locator = "addp://engine/9/path/images/private.tif?type=file&item_id=1"

	// Reuse the same handler, resolver and clients across all transitions, so a
	// former full response cannot stand in for the current user's observation.
	for _, tc := range []struct {
		name              string
		visible, readable bool
		status, checks    int
	}{
		{name: "no authorization", status: 404, checks: 1},
		{name: "metadata authorization", visible: true, status: 200, checks: 1},
		{name: "read authorization", visible: true, readable: true, status: 200, checks: 2},
		{name: "read withdrawn metadata retained", visible: true, status: 200, checks: 1},
		{name: "all authorization withdrawn", status: 404, checks: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observation.visible, observation.readable = tc.visible, tc.readable
			beforeChecks, beforeStats := observation.calls, source.stats
			req := httptest.NewRequest("GET", "/preview?locator="+url.QueryEscape(locator), nil)
			req.Header.Set("Authorization", "Bearer addp_at_original_user")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status || response.Header().Get("Cache-Control") != "no-store" || observation.calls-beforeChecks != tc.checks {
				t.Fatalf("status=%d checks=%d cache=%q body=%s", response.Code, observation.calls-beforeChecks, response.Header().Get("Cache-Control"), response.Body.String())
			}
			wantStats := 0
			if tc.readable {
				wantStats = 1
			}
			if source.stats-beforeStats != wantStats || source.opens != 0 {
				t.Fatalf("source stat=%d want=%d direct content reads=%d", source.stats-beforeStats, wantStats, source.opens)
			}
			if tc.status != 200 {
				var denied map[string]interface{}
				if err := json.Unmarshal(response.Body.Bytes(), &denied); err != nil {
					t.Fatal(err)
				}
				if len(denied) != 1 || denied["error"] == nil {
					t.Fatalf("denial exposed target facts: %s", response.Body.String())
				}
				return
			}
			var body struct {
				PreviewType string `json:"preview_type"`
				Data        struct {
					Object map[string]interface{} `json:"object"`
					Item   struct {
						Attributes []struct {
							Key   string      `json:"key"`
							Value interface{} `json:"value"`
						} `json:"attributes"`
					} `json:"item_meta"`
				} `json:"data"`
				Metadata map[string]interface{} `json:"metadata"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			gotAttributes := make(map[string]interface{})
			for _, attr := range body.Data.Item.Attributes {
				gotAttributes[attr.Key] = attr.Value
			}
			if !tc.readable {
				wantAttributes := map[string]interface{}{"schema_version": float64(1), "item": map[string]interface{}{"data_type": "media", "format": "tiff", "layout": "single"}}
				if body.PreviewType != "metadata_only" || body.Data.Object != nil || !reflect.DeepEqual(gotAttributes, wantAttributes) || body.Metadata["size_bytes"] != nil || body.Metadata["item_count"] != nil {
					t.Fatalf("metadata-only response exceeded structural allowlist: %s", response.Body.String())
				}
				return
			}
			if body.PreviewType != "object" || body.Data.Object == nil || gotAttributes["storage"] == nil || body.Data.Object["size_bytes"] != float64(1844) {
				t.Fatalf("read response lost source facts: %s", response.Body.String())
			}
			image := body.Data.Object["content"].(map[string]interface{})
			imageMetadata := image["metadata"].(map[string]interface{})
			if image["kind"] != "image" || image["preview_material"] != "url" || image["data"] != nil || imageMetadata["width"] != float64(240) || imageMetadata["height"] != float64(120) {
				t.Fatalf("read response lost TIFF image contract: %s", response.Body.String())
			}
			streamURL, err := url.Parse(image["url"].(string))
			if err != nil || streamURL.Path != "/api/v1/manager/storage-stream" || streamURL.Query().Get("locator") != locator || streamURL.Query().Get("storage_ref") != "images/private.tif" || body.Data.Object["url"] != image["url"] {
				t.Fatalf("read response lost controlled content URL: %v err=%v", image["url"], err)
			}
		})
	}
}

type objectPreviewCatalogSource struct {
	nfs.NFSPlugin
	stats, opens int
}

func (s *objectPreviewCatalogSource) DescribeEngineCatalogFacts(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, _ plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	s.stats++
	if target.EngineID != 9 || target.StringPath() != "images/private.tif" {
		return nil, fmt.Errorf("unexpected source target: %v", target)
	}
	size := int64(1844)
	return &plugin.EngineCatalogFacts{Path: target, Kind: plugin.EngineCatalogKindFile, Storage: &plugin.EngineCatalogStorageFacts{Path: target.StringPath(), SizeBytes: &size, ContentType: "image/tiff"}}, nil
}

func (s *objectPreviewCatalogSource) OpenContent(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	s.opens++
	return nil, fmt.Errorf("TIFF preview metadata must use the controlled stream URL")
}
