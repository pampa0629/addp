package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/models"
)

func TestSystemExecutionAuthorizationClientsUseRequestScopedUserAndServiceBearerTokens(t *testing.T) {
	t.Parallel()
	executionID := "9a21ab1a-2900-42a5-ae91-821339b3fcdd"
	childExecutionID := "2aaeb79d-2bbd-47a2-a8d4-a607ce6d51a5"
	parentExecutionID := "74d980cf-3ced-41ef-81fc-271f89249110"
	definitionVersion := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatal("execution authorization request sent legacy authentication headers")
		}
		switch r.URL.Path {
		case "/api/v1/system/auth/execution-authorizations":
			if r.Header.Get("Authorization") != "Bearer addp_at_user" {
				t.Fatalf("issue Authorization = %q", r.Header.Get("Authorization"))
			}
			var request IssueExecutionAuthorizationRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(IssuedExecutionAuthorization{
				ID: "91", ExecutionID: executionID, Audience: "develop",
				EngineIDs: []string{"12"}, Effects: []string{"read"}, ExpiresAt: expiresAt,
				ActorPrincipalID: "7", TenantID: "5", TenantMembershipID: "8",
				IssuedAuthorizationVersion: "3",
			})
		case "/api/v1/system/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("tenant_id") != "5" {
				t.Fatalf("service token tenant_id = %q", r.Form.Get("tenant_id"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "addp_at_develop_service", "token_type": "Bearer",
				"expires_in": 300, "scope": "addp.api",
			})
		case "/api/v1/system/execution-authorizations/91/engine-accesses":
			if r.Header.Get("Authorization") != "Bearer addp_at_develop_service" {
				t.Fatalf("consume Authorization = %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(ExecutionEngineAccess{
				AuthorizationID: "91", ExecutionID: executionID, Audience: "develop",
				EngineID: "12", Effects: []string{"read"}, ExpiresAt: expiresAt,
				Engine: &models.Engine{ID: 12},
			})
		case "/api/v1/system/runtime/execution-authorizations":
			if r.Header.Get("Authorization") != "Bearer addp_at_develop_service" {
				t.Fatalf("issue from execution Authorization = %q", r.Header.Get("Authorization"))
			}
			var request IssueExecutionAuthorizationFromExecutionRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.ParentExecutionID != parentExecutionID || request.ExecutionID != childExecutionID {
				t.Fatalf("issue from execution request = %#v", request)
			}
			_ = json.NewEncoder(w).Encode(IssuedExecutionAuthorization{
				ID: "92", ExecutionID: childExecutionID, Audience: "develop",
				EngineIDs: []string{"12"}, Effects: []string{"read"}, ExpiresAt: expiresAt,
				ActorPrincipalID: "7", TenantID: "5", TenantMembershipID: "8",
				IssuedAuthorizationVersion: "3",
			})
		case "/api/v1/system/runtime/execution-authorizations/service-definitions":
			var request IssueExecutionAuthorizationFromServiceDefinitionRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(IssuedExecutionAuthorization{
				ID: "93", ExecutionID: request.ExecutionID, Audience: "duckdb",
				EngineIDs: request.EngineIDs, Effects: []string{"read"}, ExpiresAt: expiresAt,
				ActorPrincipalID: "7", TenantID: "5", TenantMembershipID: "8",
				IssuedAuthorizationVersion: "3", SourceType: "service_definition",
				SourceDefinitionID: &request.DefinitionID, SourceDefinitionVersion: &request.DefinitionVersion,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	issuer := NewSystemExecutionAuthorizationClient(server.URL, server.Client())
	issued, err := issuer.Issue(context.Background(), "addp_at_user", IssueExecutionAuthorizationRequest{
		Audience: "develop", ExecutionID: executionID, EngineIDs: []string{"12"},
		Effects: []string{"read"}, ExpiresIn: 600,
	})
	if err != nil || issued.ID != "91" {
		t.Fatalf("Issue() response=%#v error=%v", issued, err)
	}

	source, err := NewOAuthServiceTokenSource(server.URL, "addp-develop", testServiceClientSecret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	serviceClient := NewSystemServiceClient(server.URL, source, server.Client()).WithTenantID(5)
	issuedFromExecution, err := serviceClient.IssueExecutionAuthorizationFromExecution(
		context.Background(), IssueExecutionAuthorizationFromExecutionRequest{
			ParentExecutionID: parentExecutionID, Audience: "develop", ExecutionID: childExecutionID,
			EngineIDs: []string{"12"}, Effects: []string{"read"}, ExpiresIn: 600,
		},
	)
	if err != nil || issuedFromExecution.ID != "92" {
		t.Fatalf("IssueExecutionAuthorizationFromExecution() response=%#v error=%v", issuedFromExecution, err)
	}
	issuedFromDefinition, err := serviceClient.IssueExecutionAuthorizationFromServiceDefinition(
		context.Background(), IssueExecutionAuthorizationFromServiceDefinitionRequest{
			ExecutionID: childExecutionID, EngineIDs: []string{"12"}, DefinitionID: "41",
			DefinitionVersion: definitionVersion, ExpiresIn: 60,
		},
	)
	if err != nil || issuedFromDefinition.ID != "93" {
		t.Fatalf("IssueExecutionAuthorizationFromServiceDefinition() response=%#v error=%v", issuedFromDefinition, err)
	}
	access, err := serviceClient.
		GetExecutionEngineAccess(context.Background(), issued.ID, ExecutionEngineAccessRequest{
			ExecutionID: executionID, EngineID: "12", RequiredEffects: []string{"read"},
		})
	if err != nil || access.Engine == nil || access.Engine.ID != 12 {
		t.Fatalf("GetExecutionEngineAccess() response=%#v error=%v", access, err)
	}
}
