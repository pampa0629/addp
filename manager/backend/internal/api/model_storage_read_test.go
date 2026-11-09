package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type modelReadAPIProvider struct {
	*apiDownloadTestFilePlugin
	stats, opens int
}

func (p *modelReadAPIProvider) DescribeEngineCatalogFacts(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath, options plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	p.stats++
	return p.apiDownloadTestFilePlugin.DescribeEngineCatalogFacts(ctx, info, path, options)
}
func (p *modelReadAPIProvider) OpenContent(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath, options plugin.ReadOptions) (io.ReadCloser, error) {
	p.opens++
	return p.apiDownloadTestFilePlugin.OpenContent(ctx, info, path, options)
}

func TestNFSUnadmittedRawFormatCannotReachSource(t *testing.T) {
	const primary = "data/metadata.csv"
	const locator = "addp://engine/9/path/data/metadata.csv?type=file&item_id=1"
	const content = "id,name\n1,source-value\n"
	previous, previousErr := plugin.Get("nfs")
	source := &modelReadAPIProvider{apiDownloadTestFilePlugin: newAPIDownloadTestFilePlugin("nfs", map[string]string{primary: content})}
	plugin.Register(source)
	t.Cleanup(func() {
		if previousErr == nil {
			plugin.Register(previous)
		} else {
			plugin.Unregister("nfs")
		}
	})
	system := apiDownloadTestSystemClient(t, 9, "nfs")
	raw := `{"id":1,"tenant_id":1,"engine_id":9,"item_type":"file","name":"metadata.csv","full_name":"data/metadata.csv","attributes":{"item":{"layout":"single","data_type":"table","format":"csv"},"storage":{"physical_path":"data/metadata.csv"}}}`
	metadata := service.NewMetadataService(nil, system, apiDownloadTestMetaItemClient(t, raw), nil, nil)
	router := gin.New()
	router.Use(commoni18n.I18nMiddleware())
	router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 1, 1); c.Next() })
	explorer := NewExplorerHandler(nil, nil, metadata, apiDownloadProtectionGate{}, nil)
	router.GET("/downloads/file", NewDownloadHandler(metadata, apiDownloadProtectionGate{}).DownloadFile)
	router.GET("/storage-stream", explorer.StorageStream)
	router.GET("/storage-assets/:engine_id/items/:item_id/*storage_ref", explorer.StorageAsset)
	router.GET("/resource-actions", NewResourceActionHandler(service.NewResourceActionService(system, metadata)).GetResourceActions)

	query := "?locator=" + url.QueryEscape(locator)
	for _, endpoint := range []string{"/downloads/file" + query, "/storage-stream" + query + "&storage_ref=" + url.QueryEscape(primary), "/storage-assets/9/items/1/" + primary} {
		t.Run(endpoint, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, endpoint, nil)
			request.Header.Set("Authorization", "Bearer addp_at_current_user")
			if !strings.HasPrefix(endpoint, "/downloads/") {
				request.Header.Set("Range", "bytes=0-1")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			var body map[string]any
			if response.Code != http.StatusForbidden || json.Unmarshal(response.Body.Bytes(), &body) != nil || body["error"] == nil ||
				strings.Contains(response.Body.String(), "source-value") || source.stats != 0 || source.opens != 0 {
				t.Errorf("response = %d %s, source stats/opens = %d/%d; want JSON 403 before source I/O", response.Code, response.Body.String(), source.stats, source.opens)
			}
		})
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/resource-actions"+query, nil))
	var actions service.ResourceActionsResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &actions) != nil {
		t.Fatalf("resource actions = %d %s", response.Code, response.Body.String())
	}
	if download, ok := actions.Actions["download"]; !ok || download.Supported {
		t.Errorf("download action = %#v, want unsupported", download)
	}
}

func TestAllModelRawHTTPExitsRequireCurrentSourcePermission(t *testing.T) {
	const primary = "models/test.ifc"
	const locator = "addp://engine/9/path/models/test.ifc?type=file&item_id=1"
	for _, endpoint := range []string{"/downloads/file?locator=" + url.QueryEscape(locator), "/storage-stream?locator=" + url.QueryEscape(locator) + "&storage_ref=" + url.QueryEscape(primary), "/storage-assets/9/items/1/" + primary} {
		for _, lang := range []string{"zh-cn", "en"} {
			for _, outcome := range []string{"allow", "deny", "unavailable", "ticket"} {
				t.Run(fmt.Sprintf("%s/%s/%s", endpoint, lang, outcome), func(t *testing.T) {
					previous, previousErr := plugin.Get("ftp")
					source := &modelReadAPIProvider{apiDownloadTestFilePlugin: newAPIDownloadTestFilePlugin("ftp", map[string]string{primary: "original exact model bytes"})}
					plugin.Register(source)
					t.Cleanup(func() {
						if previousErr == nil {
							plugin.Register(previous)
						} else {
							plugin.Unregister("ftp")
						}
					})
					raw, _ := json.Marshal(map[string]interface{}{"id": 1, "tenant_id": 1, "engine_id": 9, "item_type": "file", "name": "test.ifc", "full_name": primary, "fingerprint": commonModels.GenerateItemFingerprint(9, primary), "attributes": map[string]interface{}{"item": map[string]interface{}{"data_type": "model_3d", "layout": "single", "format": "ifc"}, "storage": map[string]interface{}{"physical_path": primary}}})
					metadata := service.NewMetadataService(nil, apiDownloadTestSystemClient(t, 9, "ftp"), apiDownloadTestMetaItemClient(t, string(raw)), nil, nil)
					checks := 0
					metadata.SetFileSourceReadChecker(apiModelReadChecker(func(_ context.Context, credential string, expected client.ManagerFileExpectedSource) error {
						checks++
						if credential != "addp_at_current_model_user" || expected.EngineVersion != "1" || expected.FileComposition.Format != "ifc" || len(expected.FileComposition.Refs) != 1 {
							t.Fatal("lost current identity or complete source")
						}
						switch outcome {
						case "deny":
							return client.ErrManagerPreviewReadDenied
						case "unavailable":
							return client.ErrManagerPreviewReadUnavailable
						}
						return nil
					}))
					router := gin.New()
					router.Use(commoni18n.I18nMiddleware())
					router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 1, 1); c.Next() })
					explorer := NewExplorerHandler(nil, nil, metadata, apiDownloadProtectionGate{}, nil)
					router.GET("/downloads/file", NewDownloadHandler(metadata, apiDownloadProtectionGate{}).DownloadFile)
					router.GET("/storage-stream", explorer.StorageStream)
					router.GET("/storage-assets/:engine_id/items/:item_id/*storage_ref", explorer.StorageAsset)
					requestURL := endpoint
					if outcome == "ticket" {
						separator := "?"
						if strings.Contains(requestURL, "?") {
							separator = "&"
						}
						requestURL += separator + "ticket=addp_rt_fixture"
					}
					request := httptest.NewRequest(http.MethodGet, requestURL, nil)
					request.Header.Set("Accept-Language", lang)
					if outcome != "ticket" {
						request.Header.Set("Authorization", "Bearer addp_at_current_model_user")
					}
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					want := map[string]int{"allow": 200, "deny": 403, "unavailable": 503, "ticket": 401}[outcome]
					if response.Code != want {
						t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body.String())
					}
					if outcome == "allow" {
						if response.Body.String() != "original exact model bytes" || source.opens != 1 || source.stats != 1 || checks != 2 {
							t.Fatal("original bytes or exact I/O not preserved")
						}
						return
					}
					if source.stats != 0 || source.opens != 0 || strings.Contains(response.Body.String(), "original exact model bytes") || strings.Contains(response.Body.String(), "addp_at_") {
						t.Fatal("rejected source produced I/O or private bytes")
					}
				})
			}
		}
	}
}
