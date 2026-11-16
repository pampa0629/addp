package api

import (
	"context"
	"encoding/json"
	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/rustfs"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type rustfsLiveVisibility struct {
	facts             authorizationfacts.AuthorizationFacts
	visible, readable bool
}

func (f *rustfsLiveVisibility) CheckManagerObjectPreview(_ context.Context, credential string, targets []plugin.EngineCatalogPath) (*authorization.SourceObjectPreviewCheck, error) {
	if credential != "addp_at_original_user" || len(targets) != 1 {
		return nil, client.ErrMetadataCredentialRejected
	}
	if _, err := f.facts.ProvePrimaryTarget(targets[0]); err != nil {
		return nil, err
	}
	fp := ""
	if f.visible {
		fp = f.facts.ItemFingerprint
	}
	return &authorization.SourceObjectPreviewCheck{ObservedAt: time.Now(), Targets: []authorization.SourceVisibilityTargetCheck{{SourceMetadataTargetCheck: authorization.SourceMetadataTargetCheck{Target: targets[0], Visible: f.visible}, ContentReadable: f.readable, ItemFingerprint: fp}}}, nil
}

type rustfsLivePreview struct {
	provider preview.PreviewProvider
	reads    int
	after    func()
}

func (p *rustfsLivePreview) Name() string { return p.provider.Name() }
func (p *rustfsLivePreview) Preview(ctx context.Context, req *preview.PreviewRequest) (*models.TablePreview, error) {
	p.reads++
	r, e := p.provider.Preview(ctx, req)
	if p.after != nil {
		p.after()
	}
	return r, e
}

func TestIntegrationRustFSAuthorizedPreview(t *testing.T) {
	if os.Getenv("ADDP_RUSTFS_INTEGRATION") != "1" {
		t.Skip("owned RustFS T2 required")
	}
	plugin.Register(&rustfs.RustFSPlugin{})
	item, err := os.ReadFile(os.Getenv("ADDP_RUSTFS_TEST_META_ITEM"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(os.Getenv("ADDP_RUSTFS_TEST_META_ITEM") + ".facts")
	if err != nil {
		t.Fatal(err)
	}
	var facts authorizationfacts.AuthorizationFacts
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 91, "name": "RustFS T2", "tenant_id": 1, "version": 1, "engine_type": "rustfs", "lifecycle_state": "active", "connection_status": "online", "connection_info": map[string]any{"endpoint": os.Getenv("ADDP_RUSTFS_TEST_ENDPOINT"), "access_key": os.Getenv("ADDP_RUSTFS_TEST_ACCESS_KEY"), "secret_key": os.Getenv("ADDP_RUSTFS_TEST_SECRET_KEY"), "use_ssl": false}})
	}))
	defer system.Close()
	meta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(item)
	}))
	defer meta.Close()
	tokens := client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "rustfs-t2-service", nil })
	sys := client.NewSystemClient(system.URL, tokens)
	mc := client.NewMetaClient(meta.URL, tokens)
	for _, scenario := range []string{"no grant", "metadata only", "read", "withdraw during read"} {
		t.Run(scenario, func(t *testing.T) {
			observation := &rustfsLiveVisibility{facts: facts, visible: scenario != "no grant", readable: scenario == "read" || scenario == "withdraw during read"}
			p := &rustfsLivePreview{provider: preview.NewFileTablePreviewProvider()}
			if scenario == "withdraw during read" {
				p.after = func() { observation.readable = false }
			}
			registry := preview.NewPreviewRegistry()
			registry.Register(p)
			resolver := preview.NewPreviewResolver(registry, sys, mc)
			h := NewExplorerHandler(nil, resolver, nil, nil, nil)
			h.SetObjectPreviewChecker(observation)
			router := gin.New()
			router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 1, 1); c.Next() })
			router.GET("/preview", h.Preview)
			locator := "addp://engine/91/path/" + facts.FullName + "?type=object"
			req := httptest.NewRequest(http.MethodGet, "/preview?locator="+urlQueryEscape(locator), nil)
			req.Header.Set("Authorization", "Bearer addp_at_original_user")
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			want := http.StatusOK
			if scenario == "no grant" {
				want = http.StatusNotFound
			}
			if out.Code != want {
				t.Fatalf("status=%d want=%d body=%s", out.Code, want, out.Body.String())
			}
			body := out.Body.String()
			if scenario == "read" {
				if p.reads != 1 || !strings.Contains(body, "长沙") || !strings.Contains(body, "株洲") {
					t.Fatal("real CSV preview missing", body)
				}
			} else if strings.Contains(body, "长沙") || strings.Contains(body, "株洲") {
				t.Fatal("restricted content escaped", body)
			}
			if (scenario == "no grant" || scenario == "metadata only") && p.reads != 0 {
				t.Fatal("read source without content grant")
			}
		})
	}
}
