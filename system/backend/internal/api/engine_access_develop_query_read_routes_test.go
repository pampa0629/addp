package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type developQueryReadRouteFixture struct{ calls int }

func (f *developQueryReadRouteFixture) CheckDevelopQueryRead(_ context.Context, tenant int64, request client.DevelopQueryReadRequest) (*client.DevelopQueryReadObservation, error) {
	f.calls++
	if tenant != 3 || request.Validate() != nil {
		return nil, commonapi.ErrBadRequest
	}
	now := time.Now().UTC()
	return &client.DevelopQueryReadObservation{ObservedAt: now, ExpiresAt: now.Add(time.Minute)}, nil
}

func TestDevelopQueryReadRequiresFixedMachineBoundary(t *testing.T) {
	set, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(12, "schema", "public", "people"))
	body, _ := json.Marshal(client.DevelopQueryReadRequest{AuthorizationID: "8", ExecutionID: "b26d48a3-74e8-4339-a012-f29767b985ff", EngineID: "12", EngineVersion: "1", ReadSet: set, Attempt: 1, LeaseToken: "5441eed0-1caa-4229-8828-b84ff5c665f2"})
	for _, scenario := range []string{"allowed", "human", "manager", "no-permission", "platform", "expired", "query", "unknown-field"} {
		t.Run(scenario, func(t *testing.T) {
			current := sourceReadCheckAuth()
			current.Principal.Type = "service_principal"
			current.Token.Type = middleware.IAMTokenTypeServiceAccess
			caller := "addp-develop"
			current.Client.ClientID = &caller
			current.Authorization.RoleAssignments[0].Permissions = []string{"system.develop_query_read_check.execute"}
			payload, suffix := string(body), ""
			switch scenario {
			case "human":
				current.Principal.Type = "user"
				current.Token.Type = middleware.IAMTokenTypeFirstPartyAccess
			case "manager":
				caller = "addp-manager"
			case "no-permission":
				current.Authorization.RoleAssignments[0].Permissions = nil
			case "platform":
				current.Context.Type = "platform"
				current.Context.TenantID = nil
				current.Context.TenantMembershipID = nil
			case "expired":
				current.Token.ExpiresAt = current.Token.IssuedAt
			case "query":
				suffix = "?permission=manager.data_item.read"
			case "unknown-field":
				payload = payload[:len(payload)-1] + `,"actor_principal_id":"12"}`
			}
			gin.SetMode(gin.TestMode)
			authenticate, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: &current})
			if err != nil {
				t.Fatal(err)
			}
			credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeServiceAccess)
			if err != nil {
				t.Fatal(err)
			}
			fixture := &developQueryReadRouteFixture{}
			router := gin.New()
			if err := RegisterDevelopQueryReadRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authenticate, ServiceCredential: credential}, &DevelopQueryReadHandler{service: fixture}); err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("POST", "/api/v1/system/engine-access/read-observations/develop-query"+suffix, strings.NewReader(payload))
			request.Header.Set("Authorization", "Bearer addp_at_service")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if scenario == "allowed" {
				if response.Code != 200 || fixture.calls != 1 || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatal(response.Code, response.Body.String(), fixture.calls)
				}
			} else if response.Code == 200 || fixture.calls != 0 {
				t.Fatal("unqualified caller reached source check", response.Code, fixture.calls)
			}
		})
	}
}
