package api

import (
	"encoding/json"
	commonauth "github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRuntimeLogRouteEnforcesIndependentPlatformPermissionAndOfflineIdentity(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
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
		{"offline instance allowed", "platform", "platform.module_log.read", "old", false, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err = db.AutoMigrate(&models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}, &models.ModuleRegistryState{}); err != nil {
				t.Fatal(err)
			}
			if err = db.Create(&models.ModuleRegistryState{ID: 1, Revision: 1}).Error; err != nil {
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
			handler := NewModuleRegistryHandler(registry)
			handler.runtimeLogs = service.NewRuntimeLogService(upstream.URL, "test-read")
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
			}
		})
	}
}
