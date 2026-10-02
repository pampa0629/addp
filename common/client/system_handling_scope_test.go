package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/authorization"
)

func TestSystemHandlingScopeUsesOnlyRequestUserBearerAndExactResponse(t *testing.T) {
	want := authorization.EngineAccessHandlingScope{TenantID: 7, EngineID: 9007199254740993,
		Operator: authorization.SharingFulfillmentOperator{PrincipalID: 40, MembershipID: 41, AuthorizationVersion: 42}, VerifiedAt: time.Now().UTC()}
	result := want
	status, calls := 200, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/api/v1/system/engines/9007199254740993/access_handling_scope" || r.Header.Get("Authorization") != "Bearer addp_at_user" ||
			r.Header.Get("X-Tenant-ID") != "" || r.URL.RawQuery != "" {
			t.Errorf("unexpected user scope request %s %s", r.Method, r.URL)
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("unused-machine-token"), server.Client())
	view, err := client.GetEngineAccessHandlingScope(context.Background(), want.EngineID, "addp_at_user")
	if err != nil || view.EngineID != want.EngineID || view.Operator != want.Operator {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	for _, token := range []string{"", "addp_at_", "arbitrary", "addp_at_user another"} {
		if _, err := client.GetEngineAccessHandlingScope(context.Background(), want.EngineID, token); err == nil {
			t.Fatal("invalid user credential accepted")
		}
	}
	if calls != 1 {
		t.Fatal("invalid input made an HTTP call")
	}
	for _, mutate := range []func(*authorization.EngineAccessHandlingScope){func(v *authorization.EngineAccessHandlingScope) { v.EngineID++ }, func(v *authorization.EngineAccessHandlingScope) { v.TenantID = 0 },
		func(v *authorization.EngineAccessHandlingScope) { v.Operator.MembershipID = 0 }, func(v *authorization.EngineAccessHandlingScope) { v.VerifiedAt = time.Time{} }} {
		result = want
		mutate(&result)
		if _, err := client.GetEngineAccessHandlingScope(context.Background(), want.EngineID, "addp_at_user"); err == nil {
			t.Fatal("malformed scope accepted")
		}
	}
	status, before := 401, calls
	// The assignment above is deliberately outside a retry helper: human 401
	// must propagate, never acquire a machine token.
	if _, err := client.GetEngineAccessHandlingScope(context.Background(), want.EngineID, "addp_at_user"); err == nil || calls != before+1 {
		t.Fatalf("401 retried or swallowed: %v calls=%d", err, calls)
	}
}
