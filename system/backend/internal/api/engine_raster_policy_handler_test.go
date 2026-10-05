package api

import (
	"encoding/json"
	commonauth "github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRasterPolicyContextPermissionsVersionsAndTenantIsolation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Exec("ATTACH DATABASE ':memory:' AS system").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Engine{}, &models.EngineRasterPolicy{}, &models.EngineRasterQuota{}, &iam.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	engine := models.Engine{ID: 1, Name: "Raster", EngineType: "geopython_workflow", IsBuiltin: true, LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{"protocol": "http", "host": "127.0.0.1", "port": 1}}
	if err := db.Create(&engine).Error; err != nil {
		t.Fatal(err)
	}
	var actor commonauth.AuthContext
	grant := func(scope string, read, write bool) {
		actor = testIAMActorContext(scope)
		permission := []string{}
		if read {
			permission = append(permission, "system.engine_raster_policy.read")
		}
		if write {
			permission = append(permission, "system.engine_raster_policy.update")
		}
		assignmentScope := commonauth.AssignmentScope{Type: scope, TenantID: actor.Context.TenantID}
		if len(permission) == 0 {
			actor.Authorization.RoleAssignments = []commonauth.RoleAssignment{}
			return
		}
		actor.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "91", RoleKey: "custom.raster_reader", Scope: assignmentScope, Permissions: permission, SourceType: "bootstrap", ValidFrom: actor.Token.IssuedAt}}
	}
	runtime := &IAMRuntime{Authentication: func(c *gin.Context) {
		if err := sharedauth.SetAuthContextForGin(c, actor); err != nil {
			t.Fatal(err)
		}
		c.Next()
	}, UserAccessCredential: func(c *gin.Context) { c.Next() }, ServiceCredential: func(c *gin.Context) { c.Next() }}
	router := gin.New()
	if err := RegisterEngineRasterPolicyRoutes(router.Group("/api/v1/system"), runtime, &EngineRasterPolicyHandler{service: service.NewEngineRasterPolicyService(repository.NewEngineRasterPolicyRepository(db))}); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/v1/system/"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s = %d want %d: %s", method, path, rec.Code, want, rec.Body.String())
		}
		return rec
	}
	grant("platform", true, false)
	request("GET", "platform/engine-raster-policies/1", "", 200)
	request("PUT", "platform/engine-raster-policies/1", `{"version":1,"running":2,"waiting":2,"cache_mib":256,"default_tenant_running":2,"default_tenant_waiting":2}`, 403)
	grant("platform", true, true)
	request("PUT", "platform/engine-raster-policies/1", `{"version":1,"running":1,"cache_mib":64,"default_tenant_running":1,"default_tenant_waiting":0}`, 400)
	request("PUT", "platform/engine-raster-policies/1", `{"version":1,"running":1,"waiting":null,"cache_mib":64,"default_tenant_running":1,"default_tenant_waiting":0}`, 400)
	body := `{"version":1,"running":1,"waiting":1,"cache_mib":64,"default_tenant_running":1,"default_tenant_waiting":1}`
	request("PUT", "platform/engine-raster-policies/1", body, 200)
	request("PUT", "platform/engine-raster-policies/1", body, 409)
	grant("tenant", true, true)
	request("GET", "platform/engine-raster-policies/1", "", 403)
	request("PUT", "tenant/engine-raster-policies/1", `{"version":1,"running":1,"waiting":0,"cache_mib":512}`, 400)
	request("PUT", "tenant/engine-raster-policies/1", `{"version":1,"running":2,"waiting":0}`, 400)
	request("PUT", "tenant/engine-raster-policies/1", `{"version":1,"running":1,"waiting":0,"tenant_id":8}`, 400)
	request("PUT", "tenant/engine-raster-policies/1", `{"version":1,"running":1}`, 400)
	request("PUT", "tenant/engine-raster-policies/1", `{"version":1,"running":1,"waiting":0}`, 200)
	response := request("GET", "tenant/engine-raster-policies/1", "", 200)
	var view models.RasterPolicyView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Quota.TenantID != 3 || view.Quota.Version != 2 || view.EffectiveWaiting != 0 {
		t.Fatalf("unexpected tenant view: %#v", view)
	}
	request("PUT", "tenant/engine-raster-policies/1", `{"version":2,"running":null,"waiting":null}`, 200)
	other := "8"
	actor.Context.TenantID = &other
	actor.Authorization.RoleAssignments[0].Scope.TenantID = &other
	response = request("GET", "tenant/engine-raster-policies/1", "", 200)
	json.Unmarshal(response.Body.Bytes(), &view)
	if view.Quota.TenantID != 8 || view.Quota.Version != 1 {
		t.Fatal("tenant isolation", view.Quota)
	}
	grant("tenant", false, false)
	request("GET", "tenant/engine-raster-policies/1", "", 403)
	actor = testIAMServiceActorContext("platform", "addp-geopython")
	actor.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "92", RoleKey: "platform.geopython_runtime", Scope: commonauth.AssignmentScope{Type: "platform"}, Permissions: []string{"system.engine_raster_policy_runtime.read"}, SourceType: "bootstrap", ValidFrom: actor.Token.IssuedAt}}
	request("POST", "runtime/engine-raster-policy", `{"connection_info":{"protocol":"http","host":"127.0.0.1","port":1}}`, 200)
	request("POST", "runtime/engine-raster-policy", `{"connection_info":{"protocol":"http","host":"wrong","port":1}}`, 404)
	request("POST", "runtime/engine-raster-policy", `{"connection_info":{"host":"127.0.0.1","port":1},"running":64}`, 400)
	wrongClient := "addp-document"
	actor.Client.ClientID = &wrongClient
	request("POST", "runtime/engine-raster-policy", `{"connection_info":{"host":"127.0.0.1","port":1}}`, 403)
	grant("platform", true, true)
	request("POST", "runtime/engine-raster-policy", `{"connection_info":{"host":"127.0.0.1","port":1}}`, 403)
	if err := db.Delete(&models.Engine{}, 1).Error; err != nil {
		t.Fatal(err)
	}
	grant("platform", true, false)
	request("GET", "platform/engine-raster-policies/1", "", 404)
}
