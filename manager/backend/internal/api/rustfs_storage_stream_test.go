package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type rustfsSourceReadFixture func(context.Context, string, []plugin.EngineCatalogPath) error

func (f rustfsSourceReadFixture) ObserveCurrentSourceRead(ctx context.Context, c string, p []plugin.EngineCatalogPath) error {
	return f(ctx, c, p)
}

func TestRustFSStreamChecksExactCurrentObjectBeforeStorageIO(t *testing.T) {
	for _, format := range []string{"csv", "png", "tiff", "geojson"} {
		for _, scenario := range []string{"allow", "no grant", "revoked before open", "ticket", "no checker", "sibling", "multi"} {
			t.Run(format+"/"+scenario, func(t *testing.T) {
				primary := "addp-demo/samples/source." + format
				previous, prevErr := plugin.Get("rustfs")
				provider := &apiTIFFStreamIO{apiDownloadTestFilePlugin: newAPIDownloadTestObjectPlugin("rustfs", map[string]string{primary: "exact bytes"})}
				plugin.Register(provider)
				t.Cleanup(func() {
					if prevErr == nil {
						plugin.Register(previous)
					} else {
						plugin.Unregister("rustfs")
					}
				})
				layout := "single"
				if scenario == "multi" {
					layout = "multi"
				}
				item, _ := json.Marshal(map[string]any{"id": 1, "tenant_id": 1, "engine_id": 9, "item_type": "object", "name": "source." + format, "full_name": primary, "attributes": map[string]any{"item": map[string]any{"format": format, "data_type": "media", "layout": layout, "refs": []map[string]any{{"path": primary, "role": "main", "required": true, "primary": true}}}, "storage": map[string]any{"physical_path": primary}}})
				metadata := service.NewMetadataService(nil, apiDownloadTestSystemClient(t, 9, "rustfs"), apiDownloadTestMetaItemClient(t, string(item)), nil, nil)
				checks := 0
				if scenario != "no checker" {
					metadata.SetObjectSourceReadChecker(rustfsSourceReadFixture(func(_ context.Context, credential string, targets []plugin.EngineCatalogPath) error {
						checks++
						if credential != "addp_at_current_user" || len(targets) != 1 || targets[0].StringPath() != primary || targets[0].Segments[0].Term != "service" {
							t.Fatal("lost exact object/User")
						}
						if scenario == "no grant" || scenario == "revoked before open" && checks == 2 {
							return client.ErrManagerPreviewReadDenied
						}
						return nil
					}))
				}
				h := NewExplorerHandler(nil, nil, metadata, apiDownloadProtectionGate{}, nil)
				router := gin.New()
				router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 1, 1); c.Next() })
				router.GET("/storage-stream", h.StorageStream)
				selected := primary
				if scenario == "sibling" {
					selected = "addp-demo/samples/sibling." + format
				}
				url := "/storage-stream?locator=" + urlQueryEscape("addp://engine/9/path/"+primary+"?type=object&item_id=1") + "&storage_ref=" + urlQueryEscape(selected)
				req := httptest.NewRequest(http.MethodGet, url, nil)
				req.Header.Set("Range", "bytes=0-65536")
				req.Header.Set("Authorization", "Bearer addp_at_current_user")
				if scenario == "ticket" {
					req.Header.Set("Authorization", "Bearer addp_rat_ticket")
				}
				out := httptest.NewRecorder()
				router.ServeHTTP(out, req)
				want := http.StatusForbidden
				switch scenario {
				case "allow":
					want = http.StatusPartialContent
				case "ticket":
					want = http.StatusUnauthorized
				case "no checker":
					want = http.StatusServiceUnavailable
				case "sibling":
					want = http.StatusBadRequest
				}
				if out.Code != want {
					t.Fatalf("status=%d want=%d body=%s", out.Code, want, out.Body.String())
				}
				if scenario == "allow" {
					if out.Body.String() != "exact bytes" || checks != 2 || provider.stats != 1 || provider.opens != 1 {
						t.Fatal("stream did not recheck source before open", checks, provider.stats, provider.opens)
					}
				} else if provider.opens != 0 || scenario != "revoked before open" && provider.stats != 0 {
					t.Fatal("denial touched source storage", provider.stats, provider.opens)
				}
			})
		}
	}
}
