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

type fulfillmentTestTokens struct{}

func (fulfillmentTestTokens) Token(_ context.Context, tenant uint) (string, error) {
	return "fixture-service-token", nil
}

func TestSystemFulfillmentLookupDoesNotInterpretFailuresAsMiss(t *testing.T) {
	binding := authorization.SharingFulfillmentBinding{CallerPrincipalID: 9007199254740993,
		Operator: authorization.SharingFulfillmentOperator{PrincipalID: 4, MembershipID: 5, AuthorizationVersion: 6},
		Path:     plugin.TabularItemPath(12, "schema", "public", "源.表"), DecisionID: uuid.New(), RequirementVersion: 1,
		RecipientType: "user", RecipientID: 7, Action: "read", ExpiryMode: authorization.SharingExpiryUntilRevoked}
	requestID := uuid.New()
	for _, scenario := range []struct {
		status  int
		body    string
		success bool
	}{
		{200, `{"found":false,"resolution":null}`, true},
		{200, `{}`, false}, {200, `{"found":null}`, false},
		{200, `{"found":true,"resolution":null}`, false}, {200, `invalid`, false},
		{404, `{"error_code":"not_found"}`, false}, {401, `{}`, false}, {403, `{}`, false}, {500, `{}`, false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/system/runtime/engine-access-fulfillments/"+requestID.String()+"/resolve" || r.Method != "POST" ||
				r.Header.Get("Authorization") != "Bearer fixture-service-token" || r.Header.Get("X-Tenant-ID") != "" {
				t.Error("invalid runtime request")
			}
			var actual authorization.SharingFulfillmentBinding
			if err := json.NewDecoder(r.Body).Decode(&actual); err != nil || actual.CallerPrincipalID != binding.CallerPrincipalID {
				t.Error("binding lost precision")
			}
			w.WriteHeader(scenario.status)
			_, _ = w.Write([]byte(scenario.body))
		}))
		c := NewSystemFulfillmentClient(server.URL, fulfillmentTestTokens{}, server.Client())
		result, err := c.WithTenantID(7).Resolve(context.Background(), requestID, binding)
		server.Close()
		if scenario.success {
			if err != nil || result == nil || result.Found {
				t.Fatalf("miss result=%+v err=%v", result, err)
			}
		} else if err == nil {
			t.Fatalf("failure treated as miss: %d %s", scenario.status, scenario.body)
		}
	}
}
