package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/client"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

func TestCentralScanCreateRequiresCurrentScopeAndBasicConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, depth, scope string
		remote, want       int
		wrongIdentity      bool
	}{
		{name: "allowed", depth: "basic", scope: `{"type":"engine"}`, want: 201},
		{name: "scope denied", depth: "basic", scope: `{"type":"engine"}`, remote: 403, want: 403},
		{name: "scope unavailable", depth: "basic", scope: `{"type":"engine"}`, remote: 503, want: 503},
		{name: "different user", depth: "basic", scope: `{"type":"engine"}`, wrongIdentity: true, want: 403},
		{name: "deep rejected", depth: "deep", scope: `{"type":"engine"}`, want: 403},
		{name: "selector rejected", depth: "basic", scope: `{"type":"catalog_path","catalog_paths":["public"]}`, want: 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := metatest.OpenMetadataDB(t)

			metatest.CreateScanTaskTable(t, db)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/system/oauth/token" {
					json.NewEncoder(w).Encode(map[string]any{"access_token": "addp_at_machine", "token_type": "bearer", "expires_in": 300, "scope": "addp.api"})
					return
				}
				if r.URL.Path != "/api/v1/system/engines/9" || r.Header.Get("Authorization") != "Bearer addp_at_machine" {
					t.Error("unexpected engine fact request")
					w.WriteHeader(400)
					return
				}
				tenant := uint(7)
				json.NewEncoder(w).Encode(commonmodels.Engine{ID: 9, TenantID: &tenant, EngineType: "postgresql", Version: 1})
			}))
			defer upstream.Close()
			tokens, err := client.NewOAuthServiceTokenSource(upstream.URL, "addp-meta", "meta-scope-fixture-32-byte-secret", upstream.Client())
			if err != nil {
				t.Fatal(err)
			}
			engine := service.NewEngineService(db, client.NewSystemServiceClient(upstream.URL, tokens, upstream.Client()))
			calls := 0
			h := &Handler{engineService: engine, taskService: service.NewScanTaskService(db), scanManagementScope: managementCheckFunc(func(_ context.Context, id int64, op, token string) (*authorization.EngineAccessHandlingScope, error) {
				calls++
				if id != 9 || op != "create" || token != "addp_at_exact_user" {
					t.Fatal("current user/operation/target lost")
				}
				if test.remote != 0 {
					return nil, &client.TenantAPIError{StatusCode: test.remote, Path: "secret-upstream-details"}
				}
				user := int64(91)
				if test.wrongIdentity {
					user = 92
				}
				return &authorization.EngineAccessHandlingScope{TenantID: 7, EngineID: 9, Operator: authorization.SharingFulfillmentOperator{PrincipalID: user, MembershipID: 1, AuthorizationVersion: 1}, VerifiedAt: time.Now()}, nil
			})}
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware(), func(c *gin.Context) {
				if err := commonauth.SetAuthContextForGin(c, authtest.NewTenantUserAuthContext("7", "91", []string{"meta.scan_task.create"})); err != nil {
					t.Fatal(err)
				}
			})
			router.POST("/tasks", commonauth.MustNewPermissionGuard("meta.scan_task.create"), h.CreateScanTask)
			req := httptest.NewRequest("POST", "/tasks", strings.NewReader(`{"name":"central","engine_id":9,"scan_depth":"`+test.depth+`","scope":`+test.scope+`,"enabled":false}`))
			req.Header.Set("Authorization", "Bearer addp_at_exact_user")
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != test.want || strings.Contains(response.Body.String(), "secret-upstream-details") {
				t.Fatal(response.Code, response.Body.String())
			}
			var count int64
			db.Model(&models.ScanTask{}).Count(&count)
			want := int64(0)
			if test.want == 201 {
				want = 1
			}
			if count != want {
				t.Fatal("denied create persisted", count)
			}
			if test.depth == "deep" || test.name == "selector rejected" {
				if calls != 0 {
					t.Fatal("unsupported plan reached System")
				}
			}
		})
	}
}

func TestPGPolicyDisableUsesCurrentUserScopeAndTrustedEngineFacts(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	metatest.CreateScanTaskTable(t, db)
	task := models.ScanTask{TenantID: 7, EngineID: 9, Name: "saved PG plan", OwnerModule: "system", OwnerRef: "engine:9", Enabled: true, Schedule: "0 3 * * *", Scope: models.JSONMap{"type": "engine"}, Parameters: models.JSONMap{"scan_depth": "basic"}}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/oauth/token" {
			json.NewEncoder(w).Encode(map[string]any{"access_token": "addp_at_machine", "token_type": "bearer", "expires_in": 300, "scope": "addp.api"})
			return
		}
		if r.URL.Path != "/api/v1/system/engines/9" || r.Header.Get("Authorization") != "Bearer addp_at_machine" {
			t.Error("unexpected engine lookup", r.URL.Path)
			w.WriteHeader(400)
			return
		}
		tenant := uint(7)
		json.NewEncoder(w).Encode(commonmodels.Engine{ID: 9, TenantID: &tenant, EngineType: "postgresql", Version: 1})
	}))
	defer upstream.Close()
	tokens, err := client.NewOAuthServiceTokenSource(upstream.URL, "addp-meta", "meta-scope-fixture-32-byte-secret", upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	allowed := false
	calls := 0
	h := &Handler{engineService: service.NewEngineService(db, client.NewSystemServiceClient(upstream.URL, tokens, upstream.Client())), taskService: service.NewScanTaskService(db), scanManagementScope: managementCheckFunc(func(_ context.Context, id int64, op, token string) (*authorization.EngineAccessHandlingScope, error) {
		calls++
		if id != 9 || op != "update" || token != "addp_at_exact_user" {
			t.Fatal("wrong scope request", id, op, token)
		}
		if !allowed {
			return nil, &client.TenantAPIError{StatusCode: 403}
		}
		return &authorization.EngineAccessHandlingScope{TenantID: 7, EngineID: 9, Operator: authorization.SharingFulfillmentOperator{PrincipalID: 91, MembershipID: 1, AuthorizationVersion: 1}, VerifiedAt: time.Now()}, nil
	})}
	router := gin.New()
	router.Use(commoni18n.I18nMiddleware(), func(c *gin.Context) {
		if err := commonauth.SetAuthContextForGin(c, authtest.NewTenantUserAuthContext("7", "91", []string{"meta.scan_task.update"})); err != nil {
			t.Fatal(err)
		}
	})
	router.PUT("/tasks/engines/:engine_id", commonauth.MustNewPermissionGuard("meta.scan_task.update"), h.UpsertEngineScanTask)
	for _, want := range []int{403, 200} {
		allowed = want == 200
		req := httptest.NewRequest("PUT", "/tasks/engines/9", strings.NewReader(`{"engine_name":"PG","engine_type":"minio","scan_policy":{"enabled":false,"scheduled_scan":false,"scan_depth":"deep"}}`))
		req.Header.Set("Authorization", "Bearer addp_at_exact_user")
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatal(res.Code, res.Body.String())
		}
		var stored models.ScanTask
		if err := db.First(&stored, task.ID).Error; err != nil || stored.Enabled != !allowed || stored.Schedule != task.Schedule || stored.Parameters["scan_depth"] != "basic" {
			t.Fatal("disabled PG task deleted, or configuration overwritten", stored, err)
		}
	}
	if calls != 2 {
		t.Fatal("scope not rechecked for each request", calls)
	}
}
