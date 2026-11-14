package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/nfs"
	"github.com/addp/manager/internal/preview"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type capabilityVisibilityFixture struct {
	objectVisibilityFixture
	change string
}

func (f *capabilityVisibilityFixture) CheckManagerObjectPreview(ctx context.Context, token string, targets []plugin.EngineCatalogPath) (*authorization.SourceObjectPreviewCheck, error) {
	r, e := f.objectVisibilityFixture.CheckManagerObjectPreview(ctx, token, targets)
	if f.calls == 1 {
		switch f.change {
		case "metadata":
			f.readable = false
		case "hidden":
			f.visible = false
			f.readable = false
		case "outage":
			f.err = client.ErrMetadataQueryUnavailable
		}
	}
	return r, e
}

func TestNFSTIFFCapabilityCurrentVisibilityAndDelivery(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	plugin.Register(&nfs.NFSPlugin{})
	t.Cleanup(func() {
		if previousErr == nil {
			plugin.Register(previous)
		} else {
			plugin.Unregister("nfs")
		}
	})
	for _, tc := range []struct {
		name              string
		visible, readable bool
		change            string
		err               error
		wrong, noChecker  bool
		status, checks    int
		full              bool
	}{
		{name: "no grant", status: 404, checks: 1},
		{name: "metadata only", visible: true, status: 200, checks: 1},
		{name: "read", visible: true, readable: true, status: 200, checks: 2, full: true},
		{name: "read revoked with metadata retained", visible: true, readable: true, change: "metadata", status: 200, checks: 2},
		{name: "all revoked before delivery", visible: true, readable: true, change: "hidden", status: 404, checks: 2},
		{name: "dependency lost before delivery", visible: true, readable: true, change: "outage", status: 503, checks: 2},
		{name: "dependency unavailable", err: client.ErrMetadataQueryUnavailable, status: 503, checks: 1},
		{name: "credential rejected", err: client.ErrMetadataCredentialRejected, status: 401, checks: 1},
		{name: "wrong identity", visible: true, wrong: true, status: 404, checks: 1},
		{name: "checker absent", noChecker: true, status: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTaskProviderHandlerTestDB(t)
			for _, sql := range []string{"CREATE TABLE manager.preview_state (id INTEGER PRIMARY KEY, tenant_id INTEGER, item_fingerprint TEXT, locator TEXT)", "ALTER TABLE manager.vector_tile_cache ADD COLUMN item_fingerprint TEXT", "ALTER TABLE manager.vector_materialized_view ADD COLUMN item_fingerprint TEXT", "ALTER TABLE manager.raster_cog ADD COLUMN item_fingerprint TEXT"} {
				if e := db.Exec(sql).Error; e != nil {
					t.Fatal(e)
				}
			}
			queries := 0
			db.Callback().Query().Before("gorm:query").Register("visibility-query-count", func(*gorm.DB) { queries++ })
			meta := apiDownloadTestMetaItemClient(t, `{"id":1,"tenant_id":1,"engine_id":9,"item_type":"file","full_name":"images/private.tif","name":"private.tif","attributes":{"schema_version":1,"item":{"data_type":"media","format":"tiff","layout":"single"},"storage":{"total_size":1844},"type_info":{"media":{"width":240,"height":120}},"format_info":{"tiff":{"profile":"cog","is_cloud_optimized":true}}}}`)
			resolver := preview.NewPreviewResolver(nil, apiDownloadTestSystemClient(t, 9, "nfs"), meta)
			fixture := &capabilityVisibilityFixture{objectVisibilityFixture: objectVisibilityFixture{visible: tc.visible, readable: tc.readable, wrong: tc.wrong, err: tc.err}, change: tc.change}
			h := NewQuickViewHandler(service.NewQuickViewService(db, meta), resolver, nil, nil)
			if !tc.noChecker {
				h.SetObjectPreviewChecker(fixture)
			}
			router := gin.New()
			router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 1, 33); c.Next() })
			router.GET("/capability", h.GetQuickViewCapabilityByLocator)
			req := httptest.NewRequest("GET", "/capability?locator="+urlQueryEscape("addp://engine/9/path/images/private.tif?type=file&item_id=1"), nil)
			req.Header.Set("Authorization", "Bearer addp_at_original_user")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status || fixture.calls != tc.checks || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d checks=%d body=%s", response.Code, fixture.calls, response.Body.String())
			}
			if !tc.readable && queries != 0 {
				t.Fatal("metadata/hidden target loaded persisted content or view state")
			}
			var body map[string]interface{}
			if e := json.Unmarshal(response.Body.Bytes(), &body); e != nil {
				t.Fatal(e)
			}
			if tc.status != 200 {
				if len(body) > 2 || strings.Contains(response.Body.String(), "images/private.tif") {
					t.Fatal("denial leaked target")
				}
				return
			}
			if tc.full {
				r, ok := body["raster"].(map[string]interface{})
				if !ok || r["size_bytes"] != float64(1844) || r["width"] != float64(240) {
					t.Fatalf("read lost raster facts: %s", response.Body.String())
				}
				return
			}
			if body["status"] != "metadata_only" || body["can_use_quick_view"] != false || len(body["available_actions"].([]interface{})) != 0 {
				t.Fatal("metadata-only capability allows content")
			}
			for _, key := range []string{"raster", "render_facts", "render_source", "view_state", "last_checked_at", "model_3d", "default_vector_tile_cache_id"} {
				if body[key] != nil {
					t.Fatalf("metadata projection disclosed %s", key)
				}
			}
			if len(body["quick_view"].(map[string]interface{})) != 0 {
				t.Fatal("metadata-only capability exposed content URL")
			}
		})
	}
}
