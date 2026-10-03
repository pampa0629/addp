package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestFulfillmentAcceptanceAndBasisUseTenantServiceIdentity(t *testing.T) {
	id := uuid.New()
	binding := authorization.SharingFulfillmentBinding{CallerPrincipalID: 9007199254740993,
		Operator: authorization.SharingFulfillmentOperator{PrincipalID: 4, MembershipID: 5, AuthorizationVersion: 6},
		Path:     plugin.TabularItemPath(12, "schema", "public", "源.表"), DecisionID: uuid.New(), RequirementVersion: 1,
		RecipientType: "user", RecipientID: 7, Action: "read", ExpiryMode: authorization.SharingExpiryUntilRevoked}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer fixture-service-token" || r.Header.Get("X-Tenant-ID") != "" {
			t.Error("runtime identity must use the service Bearer, not a tenant header")
		}
		var actual authorization.SharingFulfillmentBinding
		if err := json.NewDecoder(r.Body).Decode(&actual); err != nil || actual.CallerPrincipalID != binding.CallerPrincipalID {
			t.Fatalf("incomplete or imprecise binding: %+v %v", actual, err)
		}
		switch r.URL.Path {
		case "/api/v1/system/runtime/engine-access-fulfillments/" + id.String() + "/accept":
			_ = json.NewEncoder(w).Encode(authorization.SharingFulfillmentResolution{RequestID: id, TenantID: 7, Binding: actual, Outcome: "accepted"})
		case "/api/v1/catalog/runtime/sharing-fulfillments/" + id.String() + "/basis":
			_ = json.NewEncoder(w).Encode(authorization.SharingFulfillmentBasis{RequestID: id, TenantID: 7, Binding: actual, Confirmation: binding.Operator})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	system := NewSystemFulfillmentClient(server.URL, fulfillmentTestTokens{}, server.Client()).WithTenantID(7)
	accepted, err := system.Accept(context.Background(), id, binding)
	if err != nil || accepted.RequestID != id || accepted.Binding.CallerPrincipalID != binding.CallerPrincipalID {
		t.Fatalf("accept=%+v %v", accepted, err)
	}
	catalog := NewCatalogFulfillmentClient(server.URL, fulfillmentTestTokens{}, server.Client())
	basis, err := catalog.ReadFulfillmentBasis(context.Background(), 7, id, binding)
	if err != nil || basis.RequestID != id || basis.Confirmation != binding.Operator {
		t.Fatalf("basis=%+v %v", basis, err)
	}
	if _, err := system.Accept(context.Background(), uuid.Nil, binding); err == nil {
		t.Fatal("missing request ID accepted")
	}
}

func TestFulfillmentCallerComesFromSystemAuthContext(t *testing.T) {
	for _, fixture := range []struct {
		principalType, clientID, tenant string
		valid                           bool
	}{
		{"service_principal", "addp-catalog", "7", true},
		{"user", "addp-catalog", "7", false},
		{"service_principal", "addp-meta", "7", false},
		{"service_principal", "addp-catalog", "8", false},
	} {
		t.Run(fixture.principalType+fixture.clientID+fixture.tenant, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/system/auth/context" || r.Method != "GET" {
					t.Error("wrong identity endpoint")
				}
				_ = json.NewEncoder(w).Encode(authorization.AuthContext{
					Principal: authorization.AuthPrincipal{Type: fixture.principalType, ID: "9007199254740993"},
					Context:   authorization.AuthSessionContext{Type: "tenant", TenantID: &fixture.tenant},
					Client:    authorization.ClientConstraints{ClientID: &fixture.clientID},
				})
			}))
			defer server.Close()
			id, err := NewSystemFulfillmentClient(server.URL, fulfillmentTestTokens{}, server.Client()).WithTenantID(7).CurrentPrincipal(context.Background())
			if fixture.valid && (err != nil || id != 9007199254740993) || !fixture.valid && err == nil {
				t.Fatalf("caller=%d %v", id, err)
			}
		})
	}
}
