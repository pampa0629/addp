package api

import (
	"github.com/addp/system/internal/models"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	commonauth "github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

// A nil repository intentionally makes any unintended access after a rejected
// context, permission or malformed input fail the test immediately.
func TestEngineDeploymentAPIRejectsUnauthorizedAndMalformedInput(t *testing.T) {
	actor := testIAMActorContext("platform")
	permissions := []string{"platform.engine_deployment.read", "platform.engine_deployment.create", "platform.engine_deployment.update", "platform.host_node.read"}
	grant := func(keys ...string) {
		if len(keys) == 0 {
			actor.Authorization.RoleAssignments = []commonauth.RoleAssignment{}
			return
		}
		keys = append([]string(nil), keys...)
		sort.Strings(keys)
		actor.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "91", RoleKey: "custom.deployment", Scope: commonauth.AssignmentScope{Type: actor.Context.Type, TenantID: actor.Context.TenantID}, Permissions: keys, SourceType: "bootstrap", ValidFrom: actor.Token.IssuedAt}}
	}
	runtime := &IAMRuntime{Authentication: func(c *gin.Context) {
		if err := sharedauth.SetAuthContextForGin(c, actor); err != nil {
			t.Fatal(err)
		}
		c.Next()
	}, UserAccessCredential: func(c *gin.Context) { c.Next() }}
	h := &EngineDeploymentHandler{service: service.NewEngineDeploymentService(repository.NewEngineDeploymentRepository(nil))}
	router := gin.New()
	if err := RegisterEngineDeploymentRoutes(router.Group("/test"), runtime, h); err != nil {
		t.Fatal(err)
	}
	path := "/test/platform/engine_deployments"
	request := func(method, url, body string, want int) {
		t.Helper()
		w := performModuleRegistryRequest(router, method, url, body)
		if w.Code != want {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, url, w.Code, want, w.Body.String())
		}
	}
	for _, scope := range []string{"tenant", "platform"} {
		actor = testIAMActorContext(scope)
		grant(permissions...)
		if scope == "tenant" {
			request("POST", path, "{}", 403)
			request("GET", path+"/discovery", "", 403)
		} else {
			grant()
			request("POST", path, "{}", 403)
			request("GET", path+"/discovery", "", 403)
		}
	}
	actor = testIAMServiceActorContext("platform", "addp-system")
	grant(permissions...)
	request("POST", path, "{}", 403)
	request("GET", path+"/discovery", "", 403)
	actor = testIAMActorContext("platform")
	actor.Token.Type = middleware.IAMTokenTypeDelegatedAccess
	clientID := "addp-monitor"
	actor.Client.ClientID = &clientID
	actor.Client.ScopeMode = "restricted"
	actor.Client.Audiences = []string{"system"}
	actor.Client.Scopes = []string{"workflow.run"}
	actor.Delegation = &commonauth.DelegationFacts{DelegatedByClientID: clientID, AgentRunID: "test-run", ToolCallID: "test-call"}
	grant(permissions...)
	request("POST", path, "{}", 403)
	request("GET", path+"/discovery", "", 403)
	actor = testIAMActorContext("platform")
	grant("platform.engine_deployment.create")
	request("POST", path, "{}", 403)
	request("GET", path+"/discovery", "", 403)
	grant(permissions...)
	request("GET", path+"/discovery", "", 200)
	for _, query := range []string{"?", "?scope=infra", "?path=/private", "?tenant_id=1"} {
		request("GET", path+"/discovery"+query, "", 400)
	}
	request("GET", path+"/discovery", "{}", 400)
	for _, body := range []string{"{}", `{"tenant_id":1}`, `{"engine_id":1}`, `{"password":"secret"}`, `{"inventory":{"source":{"password":"secret"}}}`, `{"enabled":true,"enabled":false}`, `{"Enabled":true}`, `{} {}`, `{"display_name":"` + strings.Repeat("x", 40000) + `"}`} {
		request("POST", path, body, 400)
	}
	complete := engineDeploymentAPIInput()
	for _, body := range []string{
		strings.TrimSuffix(complete, "}") + `,"tenant_id":1}`,
		strings.TrimSuffix(complete, "}") + `,"engine_id":1}`,
		strings.Replace(complete, `"project":"business"`, `"project":"business","password":"secret"`, 1),
		strings.Replace(complete, `"enabled":true`, `"enabled":true,"enabled":false`, 1),
		strings.Replace(complete, `"display_name"`, `"Display_Name"`, 1),
	} {
		request("POST", path, body, 400)
	}

	for _, query := range []string{"page=", "page=0", "page_size=101", "page=1&page=2", "tenant_id=1", "node_id=wrong", "search=%00", "search=%ZZ"} {
		request("GET", path+"?"+query, "", 400)
	}
	request("GET", path, "{}", 400)
	request("GET", path+"/BAD-UUID", "", 400)
	request("GET", path+"/00000000-0000-4000-8000-000000000001?page=1", "", 400)
	request("PUT", path+"/00000000-0000-4000-8000-000000000001", "{}", 400)
	request("PUT", path+"/00000000-0000-4000-8000-000000000001", strings.TrimSuffix(engineDeploymentAPIInput(), "}")+`,"version":7}`, 400)
	validAdmission := `{"items":[{"scope":"infra","service":"postgres","fingerprint":"` + strings.Repeat("a", 64) + `","display_name":"Postgres","enabled":true,"declared_node_id":"10000000-0000-4000-8000-000000000001"}]}`
	request("POST", path, validAdmission, 409)
	request("POST", path+"?", validAdmission, 400)
	request("DELETE", path+"/00000000-0000-4000-8000-000000000001", "", 404)
}

func engineDeploymentAPIInput() string {
	return `{"display_name":"Postgres","enabled":true,"declared_node_id":"10000000-0000-4000-8000-000000000001","inventory":{"schema":"addp.engine-deployment-inventory/v1","observed_at":"2026-10-19T00:00:00Z","source":{"type":"docker_compose","runtime":"desktop_linux_vm","docker_engine_id":"fixture-engine","project":"business"},"services":[{"service":"postgres","engine_type":"postgresql","containers":[{"container_id":"` + strings.Repeat("a", 64) + `","started_at":"2026-10-18T00:00:00Z","restart_count":0,"published_ports":[{"address":"::","port":5433,"container_port":5432,"protocol":"tcp"}]}]}]}}`
}
func TestEngineDeploymentJSONAcceptsCanonicalCreateAndUpdate(t *testing.T) {
	for _, update := range []bool{false, true} {
		body := `{"items":[{"scope":"infra","service":"postgres","fingerprint":"` + strings.Repeat("a", 64) + `","display_name":"Postgres","enabled":true,"declared_node_id":"10000000-0000-4000-8000-000000000001"}]}`
		if update {
			body = `{"display_name":"Postgres","enabled":true,"declared_node_id":"10000000-0000-4000-8000-000000000001","version":7}`
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))
		if update {
			var input models.EngineDeploymentUpdateRequest
			if err := bindEngineDeployment(c, &input); err != nil || input.Version != 7 || input.DisplayName != "Postgres" {
				t.Fatalf("canonical update: %+v %v", input, err)
			}
		} else {
			var input models.EngineDeploymentAdmissionRequest
			if err := bindEngineDeployment(c, &input); err != nil || len(input.Items) != 1 || input.Items[0].Scope != "infra" {
				t.Fatalf("canonical admission: %+v %v", input, err)
			}
		}
	}
}
