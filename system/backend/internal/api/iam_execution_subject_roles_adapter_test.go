package api

import (
	"net/http"
	"testing"

	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func TestExecutionSubjectRolesHandlerBoundRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		extra  map[string]any
		status int
	}{
		{"valid", nil, 200}, {"no_lease", map[string]any{"lease_token": ""}, 400},
		{"no_attempt", map[string]any{"attempt": 0}, 400}, {"actor_injection", map[string]any{"actor_principal_id": "33"}, 400},
		{"role_injection", map[string]any{"role_keys": []string{"tenant.administrator"}}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeExecutionAuthorizationService{}
			h, err := NewIAMExecutionAuthorizationHandler(fake, fakeExecutionEngineResolver{})
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.POST("/execution-authorizations/:id/subject-roles", func(c *gin.Context) {
				if err := sharedauth.SetAuthContextForGin(c, testIAMServiceActorContext("tenant", "addp-develop")); err != nil {
					t.Fatal(err)
				}
				c.Next()
			}, h.ResolveSubjectRoles)
			payload := map[string]any{"execution_id": "9a21ab1a-2900-42a5-ae91-821339b3fcdd", "attempt": 2, "lease_token": "718a9dcf-75e4-4921-827f-ddb23309689b"}
			for k, v := range tc.extra {
				payload[k] = v
			}
			response := performIAMJSONRequest(t, router, http.MethodPost, "/execution-authorizations/91/subject-roles", payload, nil)
			if response.Code != tc.status || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d headers=%v", response.Code, response.Header())
			}
			if tc.status == 200 {
				if fake.rolesInput.Attempt != 2 || fake.rolesInput.ServiceClientID != "addp-develop" || fake.rolesInput.Audit.PrincipalID == nil {
					t.Fatal("trusted actor/lease missing")
				}
			} else if fake.rolesInput.AuthorizationID != 0 {
				t.Fatal("invalid request reached IAM")
			}
		})
	}
}
