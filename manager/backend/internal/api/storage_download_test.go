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

func TestStorageDownloadBundlesNFSShapefile(t *testing.T) {
	t.Parallel()

	engineType := "api_download_test_nfs_bundle"
	plugin.Register(newAPIDownloadTestFilePlugin(engineType, map[string]string{
		"shp/farmland.shp": "shape",
		"shp/farmland.shx": "index",
		"shp/farmland.dbf": "attrs",
	}))
	t.Cleanup(func() { plugin.Unregister(engineType) })

	systemClient := apiDownloadTestSystemClient(t, 26, engineType)
	metaClient := apiDownloadTestMetaItemClient(t, `{
		"id": 1,
		"engine_id": 26,
		"item_type": "file",
		"name": "farmland.shp",
		"full_name": "shp/farmland.shp",
		"attributes": {
			"item": {
				"layout": "multi",
				"format": "shapefile",
				"refs": [
					{"path":"shp/farmland.shp","role":"main","required":true,"primary":true},
					{"path":"shp/farmland.shx","role":"index","required":true},
					{"path":"shp/farmland.dbf","role":"attributes","required":true}
				]
			}
		}
	}`)
	metadataService := service.NewMetadataService(nil, systemClient, metaClient, nil, nil)
	handler := NewExplorerHandler(nil, nil, metadataService)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/storage-download", handler.StorageDownload)

	req := httptest.NewRequest(http.MethodGet, "/storage-download?engine_id=26&storage_ref=shp%2Ffarmland.shp", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("Content-Type = %q, want application/zip", got)
	}
	if got := resp.Header().Get("Content-Disposition"); got != `attachment; filename="farmland.shapefile.zip"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if resp.Body.Len() == 0 {
		t.Fatal("download body is empty")
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

func apiDownloadTestSystemClient(t *testing.T, engineID uint, engineType string) *client.SystemClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != fmt.Sprintf("/api/v1/system/engines/%d", engineID) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":              engineID,
			"name":            "engine",
			"engine_type":     engineType,
			"connection_info": map[string]interface{}{},
			"is_active":       true,
		})
	}))
	t.Cleanup(server.Close)
	return client.NewSystemClient(server.URL, "test-token")
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
	return client.NewMetaClientWithInternalKey(server.URL, "internal-key")
}

type apiDownloadTestFilePlugin struct {
	engineType string
	files      map[string]string
}

func newAPIDownloadTestFilePlugin(engineType string, files map[string]string) *apiDownloadTestFilePlugin {
	copied := map[string]string{}
	for path, content := range files {
		copied[strings.Trim(path, "/")] = content
	}
	return &apiDownloadTestFilePlugin{engineType: engineType, files: copied}
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
	return plugin.NewFileCapabilities(p.engineType)
}
func (p *apiDownloadTestFilePlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}
func (p *apiDownloadTestFilePlugin) DescribeItem(_ context.Context, _ plugin.ConnectionInfo, itemPath plugin.CatalogPath, _ plugin.MetadataOptions) (*plugin.ItemMetadata, error) {
	filePath := strings.Trim(itemPath.StringPath(), "/")
	content, ok := p.files[filePath]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}
	now := time.Unix(0, 0)
	return &plugin.ItemMetadata{
		Path: itemPath,
		Kind: plugin.CatalogKindFile,
		Stats: map[string]interface{}{
			"size_bytes": int64(len(content)),
		},
		Attributes: map[string]interface{}{
			"name": filePath,
			"path": filePath,
		},
		UpdatedAt: &now,
	}, nil
}
func (p *apiDownloadTestFilePlugin) OpenContent(_ context.Context, _ plugin.ConnectionInfo, itemPath plugin.CatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	filePath := strings.Trim(itemPath.StringPath(), "/")
	content, ok := p.files[filePath]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}
	return io.NopCloser(strings.NewReader(content)), nil
}
