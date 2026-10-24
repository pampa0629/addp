package api

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

func TestDownloadFileModelCompositionPreservesExactBytes(t *testing.T) {
	for _, object := range []bool{false, true} {
		for _, format := range []string{"ifc", "3ds", "dae", "obj"} {
			t.Run(fmt.Sprintf("%s/object=%v", format, object), func(t *testing.T) {
				parent := "models/" + format
				if object {
					parent = "bucket/" + parent
				}
				primary := parent + "/model." + format
				// Downloading does not parse model formats. Include non-text bytes to
				// verify that both streaming and bundling preserve the entire payload.
				files := map[string]string{primary: "model\x00\xff\x80\r\n" + format}
				refs := []map[string]interface{}{{"path": primary, "role": "model", "required": true, "primary": true}}
				if format == "3ds" || format == "dae" {
					files[parent+"/texture.png"] = "texture\x00\xfe\x81\r\n"
					refs = append(refs, map[string]interface{}{"path": parent + "/texture.png", "role": "texture", "required": true})
				}
				if format == "obj" {
					for _, name := range []string{"paint red.mtl", "paint blue.mtl", "red/color.png", "blue/color.png"} {
						files[parent+"/"+name] = name + "\x00\xfe\r\n"
						refs = append(refs, map[string]interface{}{"path": parent + "/" + name, "role": "dependency", "required": true})
					}
				}
				engineType := fmt.Sprintf("api_model_download_fixture_%s_%v", format, object)
				itemType := "file"
				if object {
					plugin.Register(newAPIDownloadTestObjectPlugin(engineType, files))
					itemType = "object"
				} else {
					plugin.Register(newAPIDownloadTestFilePlugin(engineType, files))
				}
				t.Cleanup(func() { plugin.Unregister(engineType) })
				item, err := json.Marshal(map[string]interface{}{
					"id": 1, "tenant_id": 1, "engine_id": 9, "item_type": itemType, "name": "model." + format, "full_name": primary,
					"attributes": map[string]interface{}{"item": map[string]interface{}{"layout": "single", "format": format, "data_type": "model_3d", "refs": refs}},
				})
				if err != nil {
					t.Fatal(err)
				}
				metadata := service.NewMetadataService(nil, apiDownloadTestSystemClient(t, 9, engineType), apiDownloadTestMetaItemClient(t, string(item)), nil, nil)
				handler := NewDownloadHandler(metadata, apiDownloadProtectionGate{})
				router := gin.New()
				router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 1, 1); c.Next() })
				router.GET("/downloads/file", handler.DownloadFile)
				locator := "addp://engine/9/path/" + primary + "?type=" + itemType + "&item_id=1"
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/downloads/file?locator="+urlQueryEscape(locator), nil))
				if response.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200", response.Code)
				}
				name := "model." + format
				if len(refs) == 1 {
					if !bytes.Equal(response.Body.Bytes(), []byte(files[primary])) {
						t.Fatal("single-file download differs from its original bytes")
					}
				} else {
					name += ".zip"
					if response.Header().Get("Content-Type") != "application/zip" {
						t.Fatal("multi-ref model download must return application/zip")
					}
					archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
					if err != nil {
						t.Fatal(err)
					}
					if len(archive.File) != len(refs) {
						t.Fatal("bundle must contain exactly the primary and confirmed texture")
					}
					seen := map[string]bool{}
					for _, entry := range archive.File {
						original, ok := files[parent+"/"+entry.Name]
						if !ok || seen[entry.Name] {
							t.Fatalf("unexpected or duplicate member %q", entry.Name)
						}
						seen[entry.Name] = true
						content, err := entry.Open()
						if err != nil {
							t.Fatal(err)
						}
						raw, err := io.ReadAll(content)
						_ = content.Close()
						if err != nil || !bytes.Equal(raw, []byte(original)) || sha256.Sum256(raw) != sha256.Sum256([]byte(original)) {
							t.Fatalf("member %q differs from its original bytes", entry.Name)
						}
					}
				}
				if response.Header().Get("Content-Disposition") != fmt.Sprintf("attachment; filename=%q", name) {
					t.Fatal("logical-object download filename differs")
				}
				t.Logf("format=%s status=%d bytes=%d refs=%d content_type=%q disposition=%q", format, response.Code, response.Body.Len(), len(refs), response.Header().Get("Content-Type"), response.Header().Get("Content-Disposition"))
			})
		}
	}
}

func TestDownloadFileBundlesObjectShapefileByLocator(t *testing.T) {
	t.Parallel()

	engineType := "api_locator_download_test_object_bundle"
	plugin.Register(newAPIDownloadTestObjectPlugin(engineType, map[string]string{
		"gischain/data/farmland.shp": "shape",
		"gischain/data/farmland.shx": "index",
		"gischain/data/farmland.dbf": "attrs",
	}))
	t.Cleanup(func() { plugin.Unregister(engineType) })

	systemClient := apiDownloadTestSystemClient(t, 9, engineType)
	metaClient := apiDownloadTestMetaItemClient(t, `{
		"id": 1,
		"tenant_id": 1,
		"engine_id": 9,
		"item_type": "object",
		"name": "farmland.shp",
		"full_name": "gischain/data/farmland.shp",
		"attributes": {
			"item": {
				"layout": "multi",
				"format": "shapefile",
				"refs": [
					{"path":"data/farmland.shp","role":"main","required":true,"primary":true},
					{"path":"data/farmland.shx","role":"index","required":true},
					{"path":"data/farmland.dbf","role":"attributes","required":true}
				]
			}
		}
	}`)
	metadataService := service.NewMetadataService(nil, systemClient, metaClient, nil, nil)
	handler := NewDownloadHandler(metadataService, apiDownloadProtectionGate{})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 1, 1)
		c.Next()
	})
	router.GET("/downloads/file", handler.DownloadFile)

	locator := "addp://engine/9/path/gischain/data/farmland.shp?type=object&item_id=1"
	req := httptest.NewRequest(http.MethodGet, "/downloads/file?locator="+urlQueryEscape(locator), nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("Content-Type = %q, want application/zip", got)
	}
	zipReader, err := zip.NewReader(bytes.NewReader(resp.Body.Bytes()), int64(resp.Body.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	names := make([]string, 0, len(zipReader.File))
	for _, file := range zipReader.File {
		names = append(names, file.Name)
	}
	sort.Strings(names)
	want := []string{"farmland.dbf", "farmland.shp", "farmland.shx"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("zip entries = %#v, want %#v", names, want)
	}
}

func TestDownloadFileDeniesManagedRawContent(t *testing.T) {
	t.Parallel()

	engineType := "api_locator_download_test_managed"
	plugin.Register(newAPIDownloadTestObjectPlugin(engineType, map[string]string{
		"bucket/report.pdf": "secret",
	}))
	t.Cleanup(func() { plugin.Unregister(engineType) })

	metadataService := service.NewMetadataService(nil, apiDownloadTestSystemClient(t, 9, engineType), apiDownloadTestMetaItemClient(t, `{
		"id": 1,
		"tenant_id": 1,
		"engine_id": 9,
		"item_type": "object",
		"name": "report.pdf",
		"full_name": "bucket/report.pdf",
		"attributes": {"item":{"layout":"single","format":"pdf"},"storage":{"physical_path":"report.pdf","bucket":"bucket"}}
	}`), nil, nil)
	handler := NewDownloadHandler(metadataService, apiDownloadProtectionGate{err: dataprotection.ErrDenied})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 1, 1)
		c.Next()
	})
	router.GET("/downloads/file", handler.DownloadFile)

	locator := "addp://engine/9/path/bucket/report.pdf?type=object&item_id=1"
	req := httptest.NewRequest(http.MethodGet, "/downloads/file?locator="+urlQueryEscape(locator), nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden || !strings.Contains(resp.Body.String(), "security_protection_required") {
		t.Fatalf("response = %d %s, want stable protection denial", resp.Code, resp.Body.String())
	}
}

func TestStorageStreamDeniesManagedRawContent(t *testing.T) {
	t.Parallel()

	engineType := "api_storage_stream_test_managed"
	plugin.Register(newAPIDownloadTestObjectPlugin(engineType, map[string]string{
		"bucket/report.pdf": "secret",
	}))
	t.Cleanup(func() { plugin.Unregister(engineType) })

	metadataService := service.NewMetadataService(nil, apiDownloadTestSystemClient(t, 9, engineType), apiDownloadTestMetaItemClient(t, `{
		"id": 1,
		"tenant_id": 1,
		"engine_id": 9,
		"item_type": "object",
		"name": "report.pdf",
		"full_name": "bucket/report.pdf",
		"attributes": {"item":{"layout":"single","format":"pdf"},"storage":{"physical_path":"report.pdf","bucket":"bucket"}}
	}`), nil, nil)
	handler := NewExplorerHandler(nil, nil, metadataService, apiDownloadProtectionGate{err: dataprotection.ErrDenied}, nil)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 1, 1)
		c.Next()
	})
	router.GET("/storage-stream", handler.StorageStream)

	locator := "addp://engine/9/path/bucket/report.pdf?type=object&item_id=1"
	req := httptest.NewRequest(http.MethodGet, "/storage-stream?locator="+urlQueryEscape(locator)+"&storage_ref=bucket%2Freport.pdf", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden || !strings.Contains(resp.Body.String(), "security_protection_required") {
		t.Fatalf("response = %d %s, want stable protection denial", resp.Code, resp.Body.String())
	}
}

func TestStorageAssetDeniesManagedRawContent(t *testing.T) {
	t.Parallel()

	engineType := "api_storage_asset_test_managed"
	plugin.Register(newAPIDownloadTestObjectPlugin(engineType, map[string]string{
		"bucket/tiles/city/Data/0.s3m": "secret",
	}))
	t.Cleanup(func() { plugin.Unregister(engineType) })

	metadataService := service.NewMetadataService(nil, apiDownloadTestSystemClient(t, 9, engineType), apiDownloadTestMetaItemClient(t, `{
		"id": 1,
		"tenant_id": 1,
		"engine_id": 9,
		"item_type": "object",
		"name": "city",
		"full_name": "bucket/tiles/city",
		"attributes": {"item":{"layout":"whole","format":"s3m"},"storage":{"physical_path":"tiles/city","bucket":"bucket"}}
	}`), nil, nil)
	handler := NewExplorerHandler(nil, nil, metadataService, apiDownloadProtectionGate{err: dataprotection.ErrDenied}, nil)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 1, 1)
		c.Next()
	})
	router.GET("/storage-assets/:engine_id/items/:item_id/*storage_ref", handler.StorageAsset)

	req := httptest.NewRequest(http.MethodGet, "/storage-assets/9/items/1/bucket/tiles/city/Data/0.s3m", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden || !strings.Contains(resp.Body.String(), "security_protection_required") {
		t.Fatalf("response = %d %s, want stable protection denial", resp.Code, resp.Body.String())
	}
}

func TestDownloadFileRejectsDatabaseLocator(t *testing.T) {
	t.Parallel()

	metadataService := service.NewMetadataService(nil, apiDownloadTestSystemClient(t, 9, "postgresql"), nil, nil, nil)
	handler := NewDownloadHandler(metadataService, nil)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/downloads/file", handler.DownloadFile)

	locator := "addp://engine/9/path/public/roads?type=table&item_id=1"
	req := httptest.NewRequest(http.MethodGet, "/downloads/file?locator="+urlQueryEscape(locator), nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
}

func urlQueryEscape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "?", "%3F"), "&", "%26")
}

func apiDownloadTestSystemClient(t *testing.T, engineID uint, engineType string) *client.SystemClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != fmt.Sprintf("/api/v1/system/engines/%d", engineID) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":                engineID,
			"tenant_id":         1,
			"name":              "engine",
			"engine_type":       engineType,
			"connection_info":   map[string]interface{}{},
			"lifecycle_state":   "active",
			"connection_status": "online",
		})
	}))
	t.Cleanup(server.Close)
	return client.NewSystemClient(server.URL, client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "addp_at_test_service_token", nil
	}))
}

func apiDownloadTestMetaItemClient(t *testing.T, itemJSON string) *client.MetaClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/items/1" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(itemJSON))
	}))
	t.Cleanup(server.Close)
	return client.NewMetaClient(server.URL, client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "test-token", nil
	}))
}

type apiDownloadProtectionGate struct {
	err error
}

func (g apiDownloadProtectionGate) RequireUnmanaged(_ context.Context, _ int64, _ []dataprotection.ResourceReference, _ time.Time) error {
	return g.err
}

func (g apiDownloadProtectionGate) Gate(_ int64, _ dataprotection.ResourceReference, _ time.Time) projectionstore.GateResult {
	return projectionstore.GateResult{}
}

type apiDownloadTestFilePlugin struct {
	engineType    string
	files         map[string]string
	objectCatalog bool
}

func newAPIDownloadTestFilePlugin(engineType string, files map[string]string) *apiDownloadTestFilePlugin {
	copied := map[string]string{}
	for path, content := range files {
		copied[strings.Trim(path, "/")] = content
	}
	return &apiDownloadTestFilePlugin{engineType: engineType, files: copied}
}

func newAPIDownloadTestObjectPlugin(engineType string, files map[string]string) *apiDownloadTestFilePlugin {
	p := newAPIDownloadTestFilePlugin(engineType, files)
	p.objectCatalog = true
	return p
}

func (p *apiDownloadTestFilePlugin) Type() string         { return p.engineType }
func (p *apiDownloadTestFilePlugin) DisplayName() string  { return p.engineType }
func (p *apiDownloadTestFilePlugin) EngineOrigin() string { return "general" }
func (p *apiDownloadTestFilePlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *apiDownloadTestFilePlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *apiDownloadTestFilePlugin) DefaultPort() int          { return 0 }
func (p *apiDownloadTestFilePlugin) RequiredFields() []string  { return nil }
func (p *apiDownloadTestFilePlugin) SensitiveFields() []string { return nil }
func (p *apiDownloadTestFilePlugin) Capabilities() plugin.EngineCapabilities {
	if p.objectCatalog {
		return plugin.NewObjectCapabilities(p.engineType)
	}
	return plugin.NewFileCapabilities(p.engineType)
}
func (p *apiDownloadTestFilePlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}
func (p *apiDownloadTestFilePlugin) DescribeEngineCatalogFacts(_ context.Context, _ plugin.ConnectionInfo, itemPath plugin.EngineCatalogPath, _ plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	filePath := strings.Trim(itemPath.StringPath(), "/")
	content, ok := p.files[filePath]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}
	now := time.Unix(0, 0)
	sizeBytes := int64(len(content))
	return &plugin.EngineCatalogFacts{
		Path: itemPath,
		Kind: p.itemKind(),
		Storage: &plugin.EngineCatalogStorageFacts{
			Name:      filePath,
			Path:      filePath,
			SizeBytes: &sizeBytes,
		},
		UpdatedAt: &now,
	}, nil
}

func (p *apiDownloadTestFilePlugin) itemKind() string {
	if p.objectCatalog {
		return plugin.EngineCatalogKindObject
	}
	return plugin.EngineCatalogKindFile
}
func (p *apiDownloadTestFilePlugin) OpenContent(_ context.Context, _ plugin.ConnectionInfo, itemPath plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	filePath := strings.Trim(itemPath.StringPath(), "/")
	content, ok := p.files[filePath]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func TestDownloadFileRejectsMalformedRawCompositionWithLocalizedError(t *testing.T) {
	for _, object := range []bool{false, true} {
		for _, lang := range []string{"zh-cn", "en"} {
			for _, bad := range []string{"models/model.obj", "../secret", "/absolute", "models/./a", "models//a", "models/model.obj/child"} {
				t.Run(fmt.Sprintf("%v/%s/%s", object, lang, bad), func(t *testing.T) {
					engineType := fmt.Sprintf("api_bad_bundle_%v_%s_%s", object, lang, bad)
					primary := "models/model.obj"
					itemType := "file"
					if object {
						primary = "bucket/" + primary
						itemType = "object"
						if strings.HasPrefix(bad, "models/") {
							bad = "bucket/" + bad
						}
					}
					files := map[string]string{primary: "must not be read"}
					if object {
						plugin.Register(newAPIDownloadTestObjectPlugin(engineType, files))
					} else {
						plugin.Register(newAPIDownloadTestFilePlugin(engineType, files))
					}
					t.Cleanup(func() { plugin.Unregister(engineType) })
					refs := []map[string]interface{}{{"path": primary, "primary": true, "required": true}, {"path": bad, "required": true}}
					item, err := json.Marshal(map[string]interface{}{"id": 1, "tenant_id": 1, "engine_id": 9, "item_type": itemType, "name": "model.obj", "full_name": primary, "attributes": map[string]interface{}{"item": map[string]interface{}{"layout": "single", "format": "obj", "refs": refs}}})
					if err != nil {
						t.Fatal(err)
					}
					metadata := service.NewMetadataService(nil, apiDownloadTestSystemClient(t, 9, engineType), apiDownloadTestMetaItemClient(t, string(item)), nil, nil)
					handler := NewDownloadHandler(metadata, apiDownloadProtectionGate{})
					router := gin.New()
					router.Use(commoni18n.I18nMiddleware())
					router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 1, 1); c.Next() })
					router.GET("/downloads/file", handler.DownloadFile)
					locator := "addp://engine/9/path/" + primary + "?type=" + itemType + "&item_id=1"
					req := httptest.NewRequest(http.MethodGet, "/downloads/file?locator="+urlQueryEscape(locator), nil)
					req.Header.Set("Accept-Language", lang)
					response := httptest.NewRecorder()
					router.ServeHTTP(response, req)
					var body struct {
						Error string `json:"error"`
						Code  string `json:"error_code"`
					}
					if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
						t.Fatal(err)
					}
					word := "组成"
					if lang == "en" {
						word = "composition"
					}
					if response.Code != http.StatusBadRequest || body.Code != "download_bundle_invalid" || !strings.Contains(body.Error, word) || response.Header().Get("Content-Type") == "application/zip" {
						t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
					}
				})
			}
		}
	}
}
