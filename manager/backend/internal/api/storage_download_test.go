package api

import (
	"archive/zip"
	"bytes"
	"context"
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
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

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
	handler := NewDownloadHandler(metadataService)

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

func TestDownloadFileRejectsDatabaseLocator(t *testing.T) {
	t.Parallel()

	metadataService := service.NewMetadataService(nil, apiDownloadTestSystemClient(t, 9, "postgresql"), nil, nil, nil)
	handler := NewDownloadHandler(metadataService)

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
		if r.URL.Path != "/api/v1/meta/items/by-catalog-path" {
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
func (p *apiDownloadTestFilePlugin) DescribeCatalogFacts(_ context.Context, _ plugin.ConnectionInfo, itemPath plugin.CatalogPath, _ plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	filePath := strings.Trim(itemPath.StringPath(), "/")
	content, ok := p.files[filePath]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}
	now := time.Unix(0, 0)
	sizeBytes := int64(len(content))
	return &plugin.CatalogFacts{
		Path: itemPath,
		Kind: p.itemKind(),
		Storage: &plugin.CatalogStorageFacts{
			Name:      filePath,
			Path:      filePath,
			SizeBytes: &sizeBytes,
		},
		UpdatedAt: &now,
	}, nil
}

func (p *apiDownloadTestFilePlugin) itemKind() string {
	if p.objectCatalog {
		return plugin.CatalogKindObject
	}
	return plugin.CatalogKindFile
}
func (p *apiDownloadTestFilePlugin) OpenContent(_ context.Context, _ plugin.ConnectionInfo, itemPath plugin.CatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	filePath := strings.Trim(itemPath.StringPath(), "/")
	content, ok := p.files[filePath]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}
	return io.NopCloser(strings.NewReader(content)), nil
}
