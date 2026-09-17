package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/execution"
)

func TestInternalTaskAuthorizationHTTPBoundary(t *testing.T) {
	scope := execution.InternalTaskScope{TaskType: "semantic_projection", ResourceID: "beijing_outdoor", Revision: "1", Digest: strings.Repeat("a", 64), Generation: "12345678-1234-1234-1234-123456789abc"}
	issued := IssuedExecutionAuthorization{ID: "1", ExecutionID: "22345678-1234-1234-1234-123456789abc", Audience: "ontology", InternalTask: &scope,
		TenantID: "5", ActorPrincipalID: "7", TenantMembershipID: "8", IssuedAuthorizationVersion: "3", SourceType: "user", ExpiresAt: time.Now().Add(time.Minute)}
	request := InternalTaskAccessRequest{ExecutionID: issued.ExecutionID, Attempt: 1, LeaseToken: "32345678-1234-1234-1234-123456789abc", InternalTask: scope}
	access := InternalTaskAccess{AuthorizationID: "1", ExecutionID: issued.ExecutionID, TenantID: "5", Audience: "ontology", Attempt: 1, InternalTask: scope, ExpiresAt: issued.ExpiresAt}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "addp_at_runtime", "token_type": "Bearer", "expires_in": 300, "scope": "addp.api"})
		case "/api/v1/system/auth/execution-authorizations":
			if r.Header.Get("Authorization") != "Bearer addp_at_user" {
				t.Error("user bearer lost")
			}
			var body IssueExecutionAuthorizationRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.InternalTask == nil || *body.InternalTask != scope || len(body.Accesses) != 0 {
				t.Error("scope changed")
			}
			_ = json.NewEncoder(w).Encode(issued)
		case "/api/v1/system/execution-authorizations/1/internal-task-accesses":
			if r.Header.Get("Authorization") != "Bearer addp_at_runtime" {
				t.Error("runtime bearer lost")
			}
			var body InternalTaskAccessRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body != request {
				t.Error("lease boundary changed")
			}
			_ = json.NewEncoder(w).Encode(access)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	issuer := NewSystemExecutionAuthorizationClient(server.URL, server.Client())
	issueRequest := IssueExecutionAuthorizationRequest{Audience: "ontology", ExecutionID: issued.ExecutionID, InternalTask: &scope}
	if _, err := issuer.Issue(context.Background(), "addp_at_user", issueRequest); err != nil {
		t.Fatal(err)
	}
	source, err := NewOAuthServiceTokenSource(server.URL, "addp-ontology", testServiceClientSecret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	runtime := NewSystemServiceClient(server.URL, source, server.Client()).WithTenantID(5)
	if _, err := runtime.GetInternalTaskAccess(context.Background(), "1", request); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*InternalTaskAccess){func(a *InternalTaskAccess) { a.Attempt++ }, func(a *InternalTaskAccess) { a.TenantID = "6" }, func(a *InternalTaskAccess) { a.InternalTask.Digest = strings.Repeat("b", 64) }, func(a *InternalTaskAccess) { a.ExpiresAt = time.Now().Add(-time.Minute) }} {
		original := access
		mutate(&access)
		if _, err := runtime.GetInternalTaskAccess(context.Background(), "1", request); err == nil {
			t.Fatal("tampered response accepted")
		}
		access = original
	}
	issued.Accesses = []ExecutionEngineAccessScope{{EngineID: "1", Effects: []string{"read"}}}
	if _, err := issuer.Issue(context.Background(), "addp_at_user", issueRequest); err == nil {
		t.Fatal("mixed grant accepted")
	}
}
