package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestHostNodeAPIContextVersionAndAuditAtomicity(t *testing.T) {
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
	if err := db.AutoMigrate(&models.HostNode{}, &iam.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	actor := testIAMActorContext("platform")
	grant := func(scope string, permissions ...string) {
		actor = testIAMActorContext(scope)
		actor.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "91", RoleKey: "custom.node", Scope: commonauth.AssignmentScope{Type: scope, TenantID: actor.Context.TenantID}, Permissions: permissions, SourceType: "bootstrap", ValidFrom: actor.Token.IssuedAt}}
	}
	runtime := &IAMRuntime{Authentication: func(c *gin.Context) {
		if err := sharedauth.SetAuthContextForGin(c, actor); err != nil {
			t.Fatal(err)
		}
		c.Next()
	}, UserAccessCredential: func(c *gin.Context) { c.Next() }}
	h := &HostNodeHandler{service: service.NewHostNodeService(repository.NewHostNodeRepository(db))}
	router := gin.New()
	if err := RegisterHostNodeRoutes(router.Group("/nodes"), runtime, h); err != nil {
		t.Fatal(err)
	}
	path := "/nodes/platform/host_nodes"
	request := func(method, url, body string, want int) string {
		t.Helper()
		resp := performModuleRegistryRequest(router, method, url, body)
		if resp.Code != want {
			t.Fatalf("%s %s: %d want%d %s", method, url, resp.Code, want, resp.Body.String())
		}
		return resp.Body.String()
	}
	input := `{"display_name":"host-a","node_kind":"virtual","addresses":["HOST-A.EXAMPLE","::ffff:192.0.2.7"],"enabled":true,"allowed_module_bindings":[{"client_id":"addp-manager","module_name":"manager"}]}`
	grant("platform", "platform.host_node.read")
	request("POST", path, input, 403)
	grant("tenant", "platform.host_node.create")
	request("POST", path, input, 403)
	grant("platform", "platform.host_node.create", "platform.host_node.read", "platform.host_node.update")
	body := request("POST", path, input, 201)
	var node models.HostNode
	if err := json.Unmarshal([]byte(body), &node); err != nil {
		t.Fatal(err)
	}
	if node.Version != 1 || node.NodeID == "" || node.Addresses[0] != "host-a.example" || node.Addresses[1] != "192.0.2.7" {
		t.Fatalf("created=%s", body)
	}
	request("POST", path, strings.TrimSuffix(input, "}")+`,"version":1}`, 400)
	request("POST", path, strings.Replace(input, `"enabled":true,`, "", 1), 400)
	request("POST", path, strings.Replace(input, `"addresses":["HOST-A.EXAMPLE","::ffff:192.0.2.7"]`, `"addresses":null`, 1), 400)
	request("POST", path, strings.Replace(input, "addp-manager", "addp-meta", 1), 400)
	request("POST", path, input+"{}", 400)
	request("POST", path, strings.Replace(input, "host-a", strings.Repeat("x", 65536), 1), 400)
	request("POST", path, strings.Replace(input, "host-a", `host\u0000`, 1), 400)
	request("GET", path+"?search=%00", "", 400)
	request("GET", path+"?search=%ZZ", "", 400)
	request("GET", path+"?page=", "", 400)
	request("GET", path+"?page_size=101", "", 400)
	request("GET", path+"?search=a&search=b", "", 400)
	request("GET", path+"?filter=all", "", 400)
	request("GET", path+"/00000000-0000-4000-8000-000000000001", "", 404)
	var page models.HostNodePage
	body = request("GET", path+"?search=host-a&page_size=1", "", 200)
	if err := json.Unmarshal([]byte(body), &page); err != nil || page.Total != 1 || len(page.Data) != 1 {
		t.Fatalf("page=%s err=%v", body, err)
	}
	request("GET", path+"?search=%25", "", 200)
	update := strings.TrimSuffix(input, "}") + `,"version":1}`
	body = request("PUT", path+"/"+node.NodeID, update, 200)
	if !strings.Contains(body, `"version":2`) {
		t.Fatal(body)
	}
	body = request("PUT", path+"/"+node.NodeID, update, 409)
	if !strings.Contains(body, "resource_version_conflict") {
		t.Fatal(body)
	}
	request("PUT", path+"/"+node.NodeID, input, 400)
	// Audit failures must roll back the entire node aggregate, including its version and allowlist.
	if err := db.Migrator().DropTable(&iam.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	request("PUT", path+"/"+node.NodeID, strings.Replace(update, `"version":1`, `"version":2`, 1), 500)
	body = request("GET", path+"/"+node.NodeID, "", 200)
	if !strings.Contains(body, `"version":2`) {
		t.Fatal(body)
	}
	request("DELETE", path+"/"+node.NodeID, "", 404)
	actor = testIAMServiceActorContext("platform", "addp-manager")
	actor.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "91", RoleKey: "custom.node", Scope: commonauth.AssignmentScope{Type: "platform"}, Permissions: []string{"platform.host_node.create"}, SourceType: "bootstrap", ValidFrom: actor.Token.IssuedAt}}
	request("POST", path, input, 403)
}

func TestModuleRegistrationUsesVerifiedClientAndRejectsInjectedSource(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&models.HostNode{}, &models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}, &models.ModuleRegistryState{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ModuleRegistryState{ID: 1, Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	node := models.HostNode{NodeID: uuid.NewString(), DisplayName: "host", NodeKind: "virtual", Enabled: true, Version: 1, Addresses: []string{}, AllowedModuleBindings: []models.HostNodeModuleBinding{{ClientID: "addp-manager", ModuleName: "manager"}}}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	registry := service.NewModuleRegistryService(repository.NewModuleRegistryRepository(db))
	h := NewModuleRegistryHandler(registry, []byte("test-key"))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if err := sharedauth.SetAuthContextForGin(c, testIAMServiceActorContext("platform", "addp-manager")); err != nil {
			t.Fatal(err)
		}
		c.Next()
	})
	router.POST("/runtime/modules", h.RegisterService)
	payload := map[string]interface{}{"node_id": node.NodeID, "module_name": "manager", "instance_id": "a", "role": "worker", "route_prefix": "/manager", "process_started_at": time.Now().UTC()}
	send := func(want int) {
		t.Helper()
		data, _ := json.Marshal(payload)
		response := performModuleRegistryRequest(router, "POST", "/runtime/modules", string(data))
		if response.Code != want {
			t.Fatalf("status=%d want=%d %s", response.Code, want, response.Body.String())
		}
	}
	payload["registration_client_id"] = "addp-meta"
	send(400)
	delete(payload, "registration_client_id")
	payload["node_id"] = "invalid-uuid"
	send(400)
	payload["node_id"] = node.NodeID
	send(200)
	var instance models.ModuleRuntimeInstance
	if err := db.First(&instance).Error; err != nil || instance.RegistrationClientID != "addp-manager" {
		t.Fatalf("source=%q err=%v", instance.RegistrationClientID, err)
	}
	module, err := registry.GetModule("manager")
	if err != nil || module.Instances[0].NodeBindingState != "bound" {
		t.Fatalf("module=%+v err=%v", module, err)
	}
}
