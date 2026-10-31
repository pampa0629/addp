package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type wholeReadAPICheck func(context.Context, string, []plugin.EngineCatalogPath) error

func (f wholeReadAPICheck) CheckManagerPreviewRead(ctx context.Context, token string, targets []plugin.EngineCatalogPath) error {
	return f(ctx, token, targets)
}

func TestFileGDBRawHTTPExitsRequireWholeSourcePermission(t *testing.T) {
	const root, member = "data/test.gdb", "data/test.gdb/a00000001.gdbtable"
	const locator = "addp://engine/9/path/data/test.gdb?type=file&item_id=1"
	for _, endpoint := range []string{"/storage-stream?locator=" + url.QueryEscape(locator) + "&storage_ref=" + url.QueryEscape(member), "/storage-assets/9/items/1/" + member, "/downloads/file?locator=" + url.QueryEscape(locator)} {
		for _, outcome := range []string{"allow", "deny", "unavailable", "ticket", "protected"} {
			// This test does not invent directory packing for the existing download route.
			if strings.HasPrefix(endpoint, "/downloads/") && outcome == "allow" {
				continue
			}
			t.Run(fmt.Sprintf("%s/%s", endpoint, outcome), func(t *testing.T) {
				previous, previousErr := plugin.Get("nfs")
				source := &modelReadAPIProvider{apiDownloadTestFilePlugin: newAPIDownloadTestFilePlugin("nfs", map[string]string{member: "exact FileGDB bytes"})}
				plugin.Register(source)
				t.Cleanup(func() {
					if previousErr == nil {
						plugin.Register(previous)
					} else {
						plugin.Unregister("nfs")
					}
				})
				raw, _ := json.Marshal(map[string]interface{}{"id": 1, "tenant_id": 1, "engine_id": 9, "item_type": "file", "name": "test.gdb", "full_name": root, "fingerprint": commonModels.GenerateItemFingerprint(9, root), "attributes": map[string]interface{}{"item": map[string]interface{}{"layout": "whole", "data_type": "container", "format": "filegdb"}, "storage": map[string]interface{}{"physical_path": root}}})
				metadata := service.NewMetadataService(nil, apiDownloadTestSystemClient(t, 9, "nfs"), apiDownloadTestMetaItemClient(t, string(raw)), nil, nil)
				checks := 0
				metadata.SetWholeSourceReadChecker(wholeReadAPICheck(func(_ context.Context, token string, targets []plugin.EngineCatalogPath) error {
					checks++
					if token != "addp_at_whole_user" || !reflect.DeepEqual(targets, []plugin.EngineCatalogPath{plugin.FileDirectoryPath(9, root)}) {
						t.Fatal("lost current User or whole root")
					}
					if outcome == "deny" {
						return client.ErrManagerPreviewReadDenied
					}
					if outcome == "unavailable" {
						return client.ErrManagerPreviewReadUnavailable
					}
					return nil
				}))
				gate := apiDownloadProtectionGate{}
				if outcome == "protected" {
					gate.err = managerprotection.ErrRequired
				}
				router := gin.New()
				router.Use(commoni18n.I18nMiddleware())
				router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 1, 1); c.Next() })
				explorer := NewExplorerHandler(nil, nil, metadata, gate, nil)
				router.GET("/storage-stream", explorer.StorageStream)
				router.GET("/storage-assets/:engine_id/items/:item_id/*storage_ref", explorer.StorageAsset)
				router.GET("/downloads/file", NewDownloadHandler(metadata, gate).DownloadFile)
				request := httptest.NewRequest(http.MethodGet, endpoint, nil)
				if outcome != "ticket" {
					request.Header.Set("Authorization", "Bearer addp_at_whole_user")
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				want := map[string]int{"allow": 200, "deny": 403, "unavailable": 503, "ticket": 401, "protected": 403}[outcome]
				if response.Code != want {
					t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body.String())
				}
				if outcome == "allow" {
					if response.Body.String() != "exact FileGDB bytes" || source.stats != 1 || source.opens != 1 || checks != 2 {
						t.Fatal("lost exact bytes or per-I/O checks")
					}
				} else if source.stats != 0 || source.opens != 0 {
					t.Fatal("denied raw request reached source")
				}
			})
		}
	}
}
