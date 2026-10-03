package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	"github.com/google/uuid"
)

func TestFulfillmentBasisRequiresExactSystemTenantService(t *testing.T) {
	system := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer system":        {ClientID: "addp-system", Permissions: []string{"catalog.sharing_fulfillment.read"}},
		"Bearer catalog":       {ClientID: "addp-catalog", Permissions: []string{"catalog.sharing_fulfillment.read"}},
		"Bearer no-permission": {ClientID: "addp-system", Permissions: []string{"catalog.entry.read"}},
	})
	defer system.Close()
	router := SetupRouter(system.URL, modulelifecycle.NewStandalone("catalog"), nil, nil, nil, nil, nil, nil)
	for _, fixture := range []struct {
		token  string
		status int
	}{{"system", 400}, {"catalog", 403}, {"no-permission", 403}, {"unknown", 401}} {
		req := httptest.NewRequest("POST", "/api/v1/catalog/runtime/sharing-fulfillments/"+uuid.NewString()+"/basis", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+fixture.token)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != fixture.status {
			t.Fatalf("identity=%s status=%d want=%d", fixture.token, response.Code, fixture.status)
		}
	}
}
