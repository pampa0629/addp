package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonauth "github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/infraconfig"
	"github.com/gin-gonic/gin"
)

func TestInfraConfigurationPlatformAndPermissions(t *testing.T) {
	const endpoint = "/platform/infra_configuration"
	const apply = endpoint + "/redis/parameters/slowlog-max-len/apply"
	const read = "system.infra_configuration.read"
	const update = "system.infra_configuration.update"
	for _, tc := range []struct {
		name, scope, method, path, body string
		permissions                     []string
		status                          int
	}{
		{"platform read", "platform", "GET", endpoint, "", []string{read}, 200},
		{"missing read", "platform", "GET", endpoint, "", nil, 403},
		{"tenant cannot access", "tenant", "GET", endpoint, "", []string{read}, 403},
		{"read only", "platform", "POST", apply, `{"value":"30"}`, []string{read}, 403},
		{"unknown input", "platform", "POST", apply, `{"value":"30","endpoint":"other"}`, []string{read, update}, 400},
		{"unknown component", "platform", "POST", endpoint + "/unsupported/parameters/x/apply", `{"value":"30"}`, []string{read, update}, 404},
		{"no binding", "platform", "POST", apply, `{"value":"30"}`, []string{read, update}, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actor := testIAMActorContext(tc.scope)
			if len(tc.permissions) != 0 {
				actor.Authorization.RoleAssignments = []commonauth.RoleAssignment{{
					AssignmentID: "91", RoleKey: "custom.settings", Scope: commonauth.AssignmentScope{
						Type: actor.Context.Type, TenantID: actor.Context.TenantID,
					}, Permissions: tc.permissions, SourceType: "bootstrap", ValidFrom: actor.Token.IssuedAt,
				}}
			}
			runtime := &IAMRuntime{
				Authentication: func(c *gin.Context) {
					if err := sharedauth.SetAuthContextForGin(c, actor); err != nil {
						t.Fatal(err)
					}
					c.Next()
				},
				UserAccessCredential: func(c *gin.Context) { c.Next() },
			}
			router := gin.New()
			handler := &InfraConfigurationHandler{service: infraconfig.New(&config.Config{InfraDatabaseProvider: "postgres"})}
			if err := RegisterInfraConfigurationRoutes(router.Group(""), runtime, handler); err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("got %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}
			if request.Method == http.MethodGet && tc.status == http.StatusOK && !strings.Contains(response.Body.String(), `"status":"unbound"`) {
				t.Fatal(response.Body.String())
			}
		})
	}
}
