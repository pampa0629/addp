package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/transfer/internal/service"
)

func TestCDCRiskInventoryRequiresExactPlatformService(t *testing.T) {
	permission := "transfer.cdc_risk_observation.read"
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/auth/context" {
			http.NotFound(w, r)
			return
		}
		identity := authtest.NewTenantUserAuthContext("7", "9", []string{permission})
		token := r.Header.Get("Authorization")
		if token != "Bearer tenant" {
			identity.Context = authorization.AuthSessionContext{Type: "platform"}
			identity.Authorization.RoleAssignments[0].Scope = authorization.AssignmentScope{Type: "platform"}
			identity.Authorization.RoleAssignments[0].RoleKey = "platform.monitor_runtime"
		}
		if token == "Bearer user" {
			identity.Authentication.AssuranceLevel = "aal2"
			identity.Authentication.Methods = []string{"password", "totp"}
		}
		if token != "Bearer user" {
			identity.Principal.Type = "service_principal"
			client := "addp-monitor"
			if token == "Bearer wrong-client" {
				client = "addp-meta"
			}
			identity.Client.ClientID = &client
			identity.Token.Type = "service_access_token"
			identity.Authentication.Methods = []string{"service_secret"}
			identity.Authentication.AssuranceLevel = "not_applicable"
		}
		if token == "Bearer no-permission" {
			identity.Authorization.RoleAssignments[0].Permissions = []string{"transfer.configuration.read"}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(identity); err != nil {
			t.Error(err)
		}
	}))
	defer auth.Close()
	db := newTransferTaskHandlerTestDB(t)
	router := SetupRouter(NewCleanupResultHandler(nil), service.NewTaskService(db, nil, nil), nil, nil, nil, auth.URL, "", nil, nil, nil, modulelifecycle.NewStandalone("transfer"))
	for _, tc := range []struct {
		token, query string
		want         int
	}{
		{"monitor", "", 200}, {"monitor", "?after_id=0", 200},
		{"user", "", 403}, {"tenant", "", 403}, {"wrong-client", "", 403}, {"no-permission", "", 403},
		{"monitor", "?tenant_id=7", 400}, {"monitor", "?after_id=0&after_id=1", 400},
		{"monitor", "?after_id=-1", 400}, {"monitor", "?after_id=01", 400},
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/transfer/runtime/cdc-risk-observations"+tc.query, nil)
		r.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s %s got %d want %d: %s", tc.token, tc.query, w.Code, tc.want, w.Body.String())
		}
	}
}
