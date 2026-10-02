package api

import (
	"encoding/json"
	commonauth "github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/runtimelog"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRuntimeLogRouteEnforcesIndependentPlatformPermissionAndOfflineIdentity(t *testing.T) {
	upstreamCalls := 0
	populated := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		if !populated {
			w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
			return
		}
		values := [][]string{}
		for _, id := range []string{"old:1", "old:2"} {
			body, _ := json.Marshal(runtimelog.Entry{InstanceID: "old", Module: "manager", ID: id, Timestamp: "2026-10-01T00:00:01Z", Level: "info"})
			values = append(values, []string{"1790812801000000000", string(body)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "streams", "result": []any{map[string]any{"values": values}}}})
	}))
	defer upstream.Close()
	for _, tc := range []struct {
		name, context, permission, instance string
		service                             bool
		want                                int
	}{
		{"module reader denied", "platform", "platform.module.read", "old", false, 403},
		{"tenant denied", "tenant", "platform.module_log.read", "old", false, 403},
		{"service credential denied", "platform", "platform.module_log.read", "old", true, 403},
		{"unknown instance denied", "platform", "platform.module_log.read", "foreign", false, 404},
		{"unregistered source allowed", "platform", "platform.module_log.read", "startup", false, 200},
		{"offline instance allowed", "platform", "platform.module_log.read", "old", false, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err = db.AutoMigrate(&models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}, &models.ModuleRegistryState{}, &models.ModuleLogSource{}); err != nil {
				t.Fatal(err)
			}
			if err = db.Create(&models.ModuleRegistryState{ID: 1, Revision: 1}).Error; err != nil {
				t.Fatal(err)
			}
			if err = db.Create(&models.ModuleLogSource{InstanceID: "startup", ModuleName: "manager", Role: "backend", HostNodeName: "host", CaptureStartedAt: time.Now().Add(-time.Minute), ObservedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
				t.Fatal(err)
			}
			registry := service.NewModuleRegistryService(repository.NewModuleRegistryRepository(db))
			if err = registry.Register(&models.ModuleRegistrationRequest{ModuleName: "manager", InstanceID: "old", Role: "backend", ModuleURL: "http://manager:8081", RoutePrefix: "/manager", ProcessStartedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			if err = db.Model(&models.ModuleRuntimeInstance{}).Where("instance_id = ?", "old").Updates(map[string]any{"status": "down", "lease_expires_at": time.Now().Add(-time.Hour)}).Error; err != nil {
				t.Fatal(err)
			}
			runtime, err := NewIAMRuntime(db, testIAMRuntimeConfig(), testIAMSecurityPolicy())
			if err != nil {
				t.Fatal(err)
			}
			actor := testIAMActorContext(tc.context)
			if tc.service {
				actor = testIAMServiceActorContext(tc.context, "addp-manager")
			}
			scope := commonauth.AssignmentScope{Type: tc.context}
			if tc.context == "tenant" {
				scope.TenantID = actor.Context.TenantID
			}
			actor.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "901", RoleKey: "platform.system_administrator", Scope: scope, Permissions: []string{tc.permission}, SourceType: "bootstrap", ValidFrom: actor.Token.IssuedAt.Add(-time.Second)}}
			runtime.Authentication = func(c *gin.Context) {
				if err := sharedauth.SetAuthContextForGin(c, actor); err != nil {
					t.Fatal(err)
				}
				c.Next()
			}
			runtime.FirstPartyCredential, err = middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess)
			if err != nil {
				t.Fatal(err)
			}
			handler := NewModuleRegistryHandler(registry, []byte("test-key"))
			handler.runtimeLogs = service.NewRuntimeLogService(upstream.URL, "test-read", []byte("test-key"))
			router := gin.New()
			if err = RegisterIAMManagementRoutes(router.Group("/api/v1/system"), runtime, handler); err != nil {
				t.Fatal(err)
			}
			before := upstreamCalls
			response := performModuleRegistryRequest(router, "GET", "/api/v1/system/platform/modules/manager/instances/"+tc.instance+"/logs?from=2026-10-01T00:00:00Z&to=2026-10-01T01:00:00Z", "")
			if response.Code != tc.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tc.want != 200 && upstreamCalls != before {
				t.Fatal("denied request reached storage")
			}
			if tc.want == 200 {
				var value service.RuntimeLogResult
				if err = json.Unmarshal(response.Body.Bytes(), &value); err != nil {
					t.Fatal(err)
				}
				if value.CollectionState != "unknown" || value.Returned != 0 {
					t.Fatal(value)
				}
				populated = true
				queryURL := "/api/v1/system/platform/modules/manager/instances/old/logs?from=2026-10-01T00:00:00Z&to=2026-10-01T01:00:00Z&limit=1"
				first := performModuleRegistryRequest(router, "GET", queryURL, "")
				var page service.RuntimeLogResult
				if first.Code != 200 || json.Unmarshal(first.Body.Bytes(), &page) != nil || page.NextCursor == "" {
					t.Fatalf("no legitimate cursor: %s", first.Body.String())
				}
				actor.Authorization.RoleAssignments[0].Permissions = []string{"platform.module.read"}
				before := upstreamCalls
				denied := performModuleRegistryRequest(router, "GET", queryURL+"&cursor="+url.QueryEscape(page.NextCursor), "")
				if denied.Code != 403 || upstreamCalls != before {
					t.Fatal("prior cursor bypassed current permission")
				}
				actor.Authorization.RoleAssignments[0].Permissions = []string{"platform.module_log.read"}
				invalid := performModuleRegistryRequest(router, "GET", queryURL+"&cursor=invalid", "")
				if invalid.Code != 400 || upstreamCalls != before {
					t.Fatal("invalid cursor reached storage")
				}
				populated = false
			}
		})
	}
}
