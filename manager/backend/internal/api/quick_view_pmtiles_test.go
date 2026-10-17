package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/nfs"
	pmtiles "github.com/addp/common/format/pmtiles"
	"github.com/addp/common/middleware/cors"
	"github.com/addp/manager/internal/preview"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type pmtilesResponseTestProvider struct {
	nfs.NFSPlugin
	archive []byte
	ranges  []plugin.ReadOptions
}

func (p *pmtilesResponseTestProvider) Type() string { return "pmtiles-response-test" }
func (p *pmtilesResponseTestProvider) ResolvePath(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	size := int64(len(p.archive))
	return &plugin.EngineCatalogEntry{Path: path, Kind: plugin.EngineCatalogKindFile, Storage: &plugin.EngineCatalogStorageFacts{SizeBytes: &size}}, nil
}
func (p *pmtilesResponseTestProvider) OpenRange(_ context.Context, _ plugin.ConnectionInfo, _ plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if opts.Offset < 0 || opts.Length <= 0 || opts.Offset+opts.Length > int64(len(p.archive)) {
		return nil, fmt.Errorf("invalid range")
	}
	p.ranges = append(p.ranges, opts)
	return io.NopCloser(bytes.NewReader(p.archive[opts.Offset : opts.Offset+opts.Length])), nil
}
func (p *pmtilesResponseTestProvider) OpenContent(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return nil, fmt.Errorf("PMTiles must use range reads")
}
func (p *pmtilesResponseTestProvider) ListChildren(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return nil, fmt.Errorf("PMTiles must resolve the exact archive")
}

func TestQuickViewBusinessPMTilesExposesCrossOriginStatusAndPreservesRangeBytes(t *testing.T) {
	writer, err := pmtiles.NewWriter(pmtiles.WriterOptions{Bounds: [4]float64{120, 30, 121, 31}, MinZoom: 0, MaxZoom: 5})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	var tile bytes.Buffer
	compressed := gzip.NewWriter(&tile)
	_, _ = compressed.Write([]byte("business tile"))
	_ = compressed.Close()
	if err := writer.AddTile(5, 26, 13, tile.Bytes()); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if _, err := writer.WriteTo(&archive); err != nil {
		t.Fatal(err)
	}
	provider := &pmtilesResponseTestProvider{archive: archive.Bytes()}
	plugin.Register(provider)
	t.Cleanup(func() { plugin.Unregister(provider.Type()) })
	resolver := preview.NewPreviewResolver(preview.NewPreviewRegistry(), apiDownloadTestSystemClient(t, 9, provider.Type()), apiDownloadTestMetaItemClient(t, `{
        "id":1,"tenant_id":1,"engine_id":9,"item_type":"file","name":"result.pmtiles","full_name":"result.pmtiles",
        "attributes":{"item":{"data_type":"media","format":"pmtiles","layout":"single"}}
    }`))
	handler := &QuickViewHandler{previewResolver: resolver, mvtService: &service.UnifiedMVTService{}}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(cors.CORS())
	router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 1, 1); c.Next() })
	router.GET("/tiles/:z/:x/:y.mvt", handler.GetQuickViewTileByLocator)
	locator := "addp://engine/9/path/result.pmtiles?type=file&item_id=1"
	for _, tc := range []struct {
		name, path, status string
		body               []byte
	}{
		{"nonempty", "/tiles/5/26/13.mvt", "ok", tile.Bytes()},
		{"empty", "/tiles/5/0/0.mvt", "empty", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tc.path+"?locator="+urlQueryEscape(locator), nil)
			request.Header.Set("Origin", "http://localhost:5170")
			router.ServeHTTP(response, request)
			if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), tc.body) {
				t.Fatalf("status/body: %d %x", response.Code, response.Body.Bytes())
			}
			for key, want := range map[string]string{"X-ADDP-Render-Source": "business_pmtiles", "X-ADDP-Tile-Cache": "MISS", "X-ADDP-Tile-Status": tc.status, "Cache-Control": "no-store"} {
				if response.Header().Get(key) != want {
					t.Fatalf("%s=%q, want %q", key, response.Header().Get(key), want)
				}
			}
			expose := strings.ToLower(response.Header().Get("Access-Control-Expose-Headers"))
			for _, key := range []string{"X-ADDP-Render-Source", "X-ADDP-Tile-Cache", "X-ADDP-Tile-Status"} {
				if !strings.Contains(expose, strings.ToLower(key)) {
					t.Errorf("browser cannot read %s: Expose-Headers=%s", key, expose)
				}
			}
			if len(tc.body) > 0 && response.Header().Get("Content-Encoding") != "gzip" {
				t.Fatal("lost gzip")
			}
			if len(tc.body) == 0 && response.Header().Get("Content-Encoding") != "" {
				t.Fatal("empty tile marked gzip")
			}
		})
	}
	if len(provider.ranges) < 4 {
		t.Fatalf("expected header/directory/tile ranges, got %v", provider.ranges)
	}
}
