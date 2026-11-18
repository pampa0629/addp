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

func registerNFSQuickViewTestPlugin(t *testing.T) {
	t.Helper()
	previous, previousErr := plugin.Get("nfs")
	plugin.Register(&nfs.NFSPlugin{})
	t.Cleanup(func() {
		if previousErr == nil {
			plugin.Register(previous)
		} else {
			plugin.Unregister("nfs")
		}
	})
}

func TestNFSTIFFCapabilityCurrentVisibilityAndDelivery(t *testing.T) {
	registerNFSQuickViewTestPlugin(t)
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
			for _, sql := range []string{"CREATE TABLE manager.preview_state (id INTEGER PRIMARY KEY, tenant_id INTEGER, user_id INTEGER, item_fingerprint TEXT, locator TEXT)", "ALTER TABLE manager.vector_tile_cache ADD COLUMN item_fingerprint TEXT", "ALTER TABLE manager.vector_materialized_view ADD COLUMN item_fingerprint TEXT", "ALTER TABLE manager.raster_cog ADD COLUMN item_fingerprint TEXT"} {
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

func TestPreviewStateUsesCurrentTenantUserForReadAndWrite(t *testing.T) {
	registerNFSQuickViewTestPlugin(t)
	db := newTaskProviderHandlerTestDB(t)
	if err := db.Exec(`CREATE TABLE manager.preview_state (
		id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, user_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL,
		locator TEXT NOT NULL, preferred_mode TEXT DEFAULT 'basic_preview', view_state JSON DEFAULT '{}',
		created_at DATETIME, updated_at DATETIME, UNIQUE(tenant_id, item_fingerprint, user_id))`).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"vector_tile_cache", "vector_materialized_view", "raster_cog"} {
		if err := db.Exec("ALTER TABLE manager." + table + " ADD COLUMN item_fingerprint TEXT").Error; err != nil {
			t.Fatal(err)
		}
	}
	meta := apiDownloadTestMetaItemClient(t, `{"id":1,"tenant_id":1,"engine_id":9,"item_type":"file","full_name":"images/private.tif","name":"private.tif","attributes":{"schema_version":1,"item":{"data_type":"media","format":"tiff","layout":"single"},"storage":{"total_size":1844},"capabilities":{"spatial":{"srid":4326,"extent":[1,2,3,4]}},"type_info":{"media":{"width":240,"height":120}},"format_info":{"tiff":{"profile":"cog","is_cloud_optimized":true}}}}`)
	resolver := preview.NewPreviewResolver(nil, apiDownloadTestSystemClient(t, 9, "nfs"), meta)
	h := NewQuickViewHandler(service.NewQuickViewService(db, meta), resolver, nil, nil)
	h.SetObjectPreviewChecker(&objectVisibilityFixture{visible: true, readable: true})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		switch c.GetHeader("X-Test-Session") {
		case "user-a-first-session", "user-a-new-session":
			setTenantAuthContextForTest(c, 1, 31)
		case "user-b-first-session", "user-b-new-session":
			setTenantAuthContextForTest(c, 1, 32)
		}
	})
	router.GET("/preview-state", h.GetPreviewStateByLocator)
	router.GET("/quick-view/capability", h.GetQuickViewCapabilityByLocator)
	router.PATCH("/preview-state/view-state", h.UpdateViewStateByLocator)
	router.PATCH("/preview-state/preferred-mode", h.UpdatePreferredModeByLocator)

	const locator = "addp://engine/9/path/images/private.tif?type=file&item_id=1"
	call := func(session, method, path string, payload map[string]any, status int) *httptest.ResponseRecorder {
		t.Helper()
		if method == "GET" {
			path += "?locator=" + urlQueryEscape(locator)
		} else {
			payload["locator"] = locator
		}
		requestBody, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(string(requestBody)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer addp_at_original_user")
		req.Header.Set("X-Test-Session", session)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != status {
			t.Fatalf("%s %s status=%d want %d: %s", method, path, res.Code, status, res.Body.String())
		}
		return res
	}
	assertState := func(session, path, mode string, zoom int) {
		t.Helper()
		res := call(session, "GET", path, nil, 200)
		var state struct {
			PreferredMode string `json:"preferred_mode"`
			ViewState     map[string]struct {
				Map struct {
					Zoom int `json:"zoom"`
				} `json:"map"`
			} `json:"view_state"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if state.PreferredMode != mode || state.ViewState["basic_preview"].Map.Zoom != zoom ||
			res.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("unexpected state from %s: %s", path, res.Body.String())
		}
	}
	writeView := func(session string, zoom int, suppliedUser uint) {
		t.Helper()
		call(session, "PATCH", "/preview-state/view-state", map[string]any{
			"user_id": suppliedUser,
			"view_state": map[string]any{
				"basic_preview": map[string]any{"map": map[string]any{"center": []int{2, 3}, "zoom": zoom}},
			},
		}, 200)
	}
	call("machine", "GET", "/preview-state", nil, 403)
	call("machine", "PATCH", "/preview-state/view-state", map[string]any{}, 403)
	call("machine", "PATCH", "/preview-state/preferred-mode", map[string]any{}, 403)
	writeView("user-a-first-session", 8, 32)
	call("user-a-first-session", "PATCH", "/preview-state/preferred-mode", map[string]any{"preferred_mode": "basic_preview", "user_id": 32}, 200)
	assertState("user-b-first-session", "/preview-state", "basic_preview", 0)
	assertState("user-b-first-session", "/quick-view/capability", "map_quick_view", 0)

	writeView("user-b-first-session", 13, 31)
	call("user-b-first-session", "PATCH", "/preview-state/preferred-mode", map[string]any{"preferred_mode": "map_quick_view", "user_id": 31}, 200)
	for _, path := range []string{"/preview-state", "/quick-view/capability"} {
		assertState("user-a-new-session", path, "basic_preview", 8)
		assertState("user-b-new-session", path, "map_quick_view", 13)
	}
}
