package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/nfs"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	"github.com/gin-gonic/gin"
)

type objectVisibilityFixture struct {
	visible, readable, wrong bool
	err                      error
	calls                    int
}

func (f *objectVisibilityFixture) CheckManagerObjectPreview(_ context.Context, credential string, targets []plugin.EngineCatalogPath) (*authorization.SourceObjectPreviewCheck, error) {
	f.calls++
	if credential != "addp_at_original_user" {
		return nil, errors.New("wrong original credential")
	}
	if f.err != nil {
		return nil, f.err
	}
	fp := ""
	if f.visible {
		fp = objectPreviewFingerprint()
	}
	if f.wrong {
		fp = strings.Repeat("f", 64)
	}
	return &authorization.SourceObjectPreviewCheck{ObservedAt: time.Now(), Targets: []authorization.SourceVisibilityTargetCheck{{SourceMetadataTargetCheck: authorization.SourceMetadataTargetCheck{Target: targets[0], Visible: f.visible}, ContentReadable: f.readable, ItemFingerprint: fp}}}, nil
}

type objectVisibilityProvider struct {
	calls  int
	during func()
}

func (*objectVisibilityProvider) Name() string { return "builtin:file-catalog" }
func (p *objectVisibilityProvider) Preview(context.Context, *preview.PreviewRequest) (*models.TablePreview, error) {
	p.calls++
	if p.during != nil {
		p.during()
	}
	return &models.TablePreview{Mode: "object", Columns: []string{}, Rows: []map[string]interface{}{}, Object: &models.ObjectPreview{Path: "images/private.tif", URL: "https://private.invalid/content"}}, nil
}

func TestNFSTIFFObjectPreviewVisibilityAndFinalRecheck(t *testing.T) {
	// Formal resolver and HTTP owner; only System observations and Provider I/O
	// are fixtures. No running service or real authorization is mutated.
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
		name                  string
		visible, readable     bool
		during                string
		err                   error
		wrong, noChecker      bool
		status, reads, checks int
		private               bool
	}{
		{name: "no target Grant", status: 404, checks: 1},
		{name: "metadata only", visible: true, status: 200, checks: 1},
		{name: "current read", visible: true, readable: true, status: 200, reads: 1, checks: 2, private: true},
		{name: "withdrawn before request", status: 404, checks: 1},
		{name: "expired before request", status: 404, checks: 1},
		{name: "revoke in flight", visible: true, readable: true, during: "hide", status: 404, reads: 1, checks: 2},
		{name: "expire in flight", visible: true, readable: true, during: "hide", status: 404, reads: 1, checks: 2},
		{name: "read withdrawn metadata retained", visible: true, readable: true, during: "metadata", status: 200, reads: 1, checks: 2},
		{name: "dependency before provider", err: client.ErrMetadataQueryUnavailable, status: 503, checks: 1},
		{name: "dependency after provider", visible: true, readable: true, during: "outage", status: 503, reads: 1, checks: 2},
		{name: "credential rejected", err: client.ErrMetadataCredentialRejected, status: 401, checks: 1},
		{name: "function rejected", err: client.ErrMetadataQueryDenied, status: 403, checks: 1},
		{name: "different formal item", visible: true, wrong: true, status: 404, checks: 1},
		{name: "checker unavailable", noChecker: true, status: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attributes := map[string]interface{}{"schema_version": 1, "item": map[string]interface{}{"data_type": "media", "format": "tiff", "layout": "single", "refs": []map[string]interface{}{{"path": "images/private.tif"}}},
				"storage":     map[string]interface{}{"physical_path": "images/private.tif", "content_versions": map[string]interface{}{"sha256": "private-version-digest"}},
				"format_info": map[string]interface{}{"tiff": map[string]interface{}{"page_summary": "private-page-directory"}}, "type_info": map[string]interface{}{"media": map[string]interface{}{"statistics": "private-statistics"}}}
			item, _ := json.Marshal(map[string]interface{}{"id": 1, "tenant_id": 1, "engine_id": 9, "item_type": "file", "name": "private.tif", "full_name": "images/private.tif", "scanned_depth": "deep", "size_bytes": 987654, "row_count": 12345, "attributes": attributes})
			provider := &objectVisibilityProvider{}
			registry := preview.NewPreviewRegistry()
			registry.Register(provider)
			resolver := preview.NewPreviewResolver(registry, apiDownloadTestSystemClient(t, 9, "nfs"), apiDownloadTestMetaItemClient(t, string(item)))
			fixture := &objectVisibilityFixture{visible: tc.visible, readable: tc.readable, err: tc.err, wrong: tc.wrong}
			provider.during = func() {
				switch tc.during {
				case "hide":
					fixture.visible = false
					fixture.readable = false
				case "metadata":
					fixture.readable = false
				case "outage":
					fixture.err = client.ErrMetadataQueryUnavailable
				}
			}
			handler := NewExplorerHandler(nil, resolver, nil, nil, nil)
			if !tc.noChecker {
				handler.SetObjectPreviewChecker(fixture)
			}
			router := gin.New()
			router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 1, 33); c.Next() })
			router.GET("/preview", handler.Preview)
			req := httptest.NewRequest("GET", "/preview?locator="+urlQueryEscape("addp://engine/9/path/images/private.tif?type=file&item_id=1"), nil)
			req.Header.Set("Authorization", "Bearer addp_at_original_user")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status || provider.calls != tc.reads || fixture.calls != tc.checks {
				t.Fatalf("status=%d reads=%d checks=%d body=%s", response.Code, provider.calls, fixture.calls, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cacheable preview")
			}
			raw := response.Body.String()
			if tc.private {
				if !strings.Contains(raw, "private-version-digest") || !strings.Contains(raw, "private.invalid/content") {
					t.Fatal("read preview lost current full response")
				}
				return
			}
			for _, secret := range []string{"private-version-digest", "private-page-directory", "private-statistics", "private.invalid", "987654", "12345", "content_versions", "physical_path", "\"refs\""} {
				if strings.Contains(raw, secret) {
					t.Fatalf("restricted fact escaped: %s", secret)
				}
			}
			var body map[string]interface{}
			if json.Unmarshal(response.Body.Bytes(), &body) != nil {
				t.Fatal("invalid JSON")
			}
			if tc.status != 200 {
				if len(body) > 2 || body["data"] != nil || strings.Contains(raw, "images/private.tif") {
					t.Fatal("denial leaked target")
				}
				return
			}
			if body["preview_type"] != "metadata_only" {
				t.Fatal("metadata was routed to content preview")
			}
			data := body["data"].(map[string]interface{})
			if data["mode"] != "metadata_only" || data["object"] != nil || data["item_meta"] == nil || len(data["rows"].([]interface{})) != 0 {
				t.Fatal("unsafe metadata-only shape")
			}
			meta := data["item_meta"].(map[string]interface{})
			if meta["full_name"] != "images/private.tif" || meta["row_count"] != nil || len(meta["attributes"].([]interface{})) != 2 {
				t.Fatal("metadata allowlist missing or oversized")
			}
		})
	}
}

func objectPreviewFingerprint() string {
	return commonmodels.GenerateItemFingerprint(9, "images/private.tif")
}
