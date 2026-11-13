package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/middleware"
)

func TestManagerPGHandoffRequiresExactMachineQualification(t *testing.T) {
	binding, err := authorizationfacts.NativeCandidateBinding(plugin.TabularItemPath(12, "schema", "public", "C"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(client.ManagerPGReadCheckRequest{UserCredential: "addp_at_test", Sources: []authorizationfacts.Binding{binding}})
	for _, route := range []string{"manager-pg-preview", "manager-pg-profile-result"} {
		for _, scenario := range []string{"allowed", "human", "other-client", "no-permission", "platform", "expired", "query", "unknown-field", "missing-user"} {
			t.Run(route+"/"+scenario, func(t *testing.T) {
				current := sourceReadCheckAuth()
				current.Principal.Type = "service_principal"
				current.Token.Type = middleware.IAMTokenTypeServiceAccess
				clientID := "addp-manager"
				current.Client.ClientID = &clientID
				current.Authorization.RoleAssignments[0].Permissions = []string{"system.manager_pg_read_check.execute"}
				token := "addp_at_test"
				suffix := ""
				payload := string(body)
				switch scenario {
				case "human":
					current.Principal.Type = "user"
					current.Token.Type = middleware.IAMTokenTypeFirstPartyAccess
				case "other-client":
					other := "addp-meta"
					current.Client.ClientID = &other
				case "no-permission":
					current.Authorization.RoleAssignments[0].Permissions = nil
				case "platform":
					current.Context.Type = "platform"
					current.Context.TenantID = nil
					current.Context.TenantMembershipID = nil
				case "expired":
					current.Token.ExpiresAt = current.Token.IssuedAt
				case "query":
					suffix = "?tenant_id=3"
				case "unknown-field":
					payload = payload[:len(payload)-1] + `,"permission":"manager.data_item.read"}`
				case "missing-user":
					payload = strings.ReplaceAll(payload, "addp_at_test", "")
				}
				fixture := &sourceReadCheckFixture{}
				router := sourceReadCheckRouter(t, fixture, current)
				req := httptest.NewRequest("POST", "/api/v1/system/engine-access/read-checks/"+route+suffix, strings.NewReader(payload))
				req.Header.Set("Authorization", "Bearer "+token)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				if scenario == "allowed" {
					if response.Code != 200 || fixture.calls != 1 || response.Header().Get("Cache-Control") != "no-store" {
						t.Fatal(response.Code, response.Body.String(), fixture.calls)
					}
				} else if response.Code == 200 || fixture.calls != 0 {
					t.Fatal("unqualified handoff reached service", response.Code, fixture.calls)
				}
				if strings.Contains(response.Body.String(), "addp_at_test") {
					t.Fatal("original credential leaked")
				}
			})
		}
	}
	// Removed human profile route cannot remain as a compatibility path.
	fixture := &sourceReadCheckFixture{}
	router := sourceReadCheckRouter(t, fixture, sourceReadCheckAuth())
	req := httptest.NewRequest("POST", "/api/v1/system/engine-access/read-checks/manager-profile-result", strings.NewReader(string(body)))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != 404 || fixture.calls != 0 {
		t.Fatal("legacy route remains", response.Code)
	}
}
