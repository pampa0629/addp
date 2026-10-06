package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
)

const profileClientExecution = "05aa6b01-7947-4301-a5a6-00bb9ebd4300"
const profileClientLease = "718a9dcf-75e4-4921-827f-ddb23309689b"

func profileClientScope(t *testing.T) execution.ManagerProfileReadScope {
	t.Helper()
	set, err := plugin.NewQueryReadSet(plugin.TabularItemPath(12, "schema", "public", "C"))
	if err != nil {
		t.Fatal(err)
	}
	return execution.ManagerProfileReadScope{ConfigDigest: strings.Repeat("a", 64), ReadSet: *set}
}

func profileClientIssued(t *testing.T) IssuedManagerProfileAuthorization {
	return IssuedManagerProfileAuthorization{ID: "9", ExecutionID: profileClientExecution, TenantID: "3", Audience: "manager", SourceReadScope: profileClientScope(t), ExpiresAt: time.Now().Add(15 * time.Minute)}
}

func profileClientRequest(t *testing.T) ManagerProfileAccessRequest {
	return ManagerProfileAccessRequest{ExecutionID: profileClientExecution, Attempt: 2, LeaseToken: profileClientLease, SourceReadScope: profileClientScope(t)}
}

func TestManagerProfileClientFixedCredentialContracts(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.RawQuery != "" || r.Header.Get("X-Tenant-ID") != "" || r.Header.Get("X-Internal-API-Key") != "" {
			t.Error("expanded request")
		}
		switch r.URL.Path {
		case "/api/v1/system/auth/execution-authorizations/manager-profiles":
			if r.Header.Get("Authorization") != "Bearer addp_at_current_user" {
				t.Error("wrong issuer credential")
			}
			var request map[string]json.RawMessage
			if json.NewDecoder(r.Body).Decode(&request) != nil || len(request) != 1 || request["execution_id"] == nil {
				t.Error("self reported issuance scope")
			}
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(profileClientIssued(t))
		case "/api/v1/system/execution-authorizations/9/manager-profile-accesses":
			if r.Header.Get("Authorization") != "Bearer addp_at_current_service" {
				t.Error("saved user credential used")
			}
			var request ManagerProfileAccessRequest
			if json.NewDecoder(r.Body).Decode(&request) != nil || request.ExecutionID != profileClientExecution || request.LeaseToken != profileClientLease || request.Attempt != 2 || request.SourceReadScope.Validate() != nil {
				t.Error("missing frozen binding")
			}
			json.NewEncoder(w).Encode(ManagerProfileAccessObservation{ObservedAt: time.Now()})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	issued, err := NewSystemExecutionAuthorizationClient(server.URL, server.Client()).IssueManagerProfile(context.Background(), "addp_at_current_user", IssueManagerProfileAuthorizationRequest{ExecutionID: profileClientExecution})
	if err != nil || !issued.Matches(3, profileClientScope(t)) || issued.Matches(4, profileClientScope(t)) {
		t.Fatalf("issuance=%+v error=%v", issued, err)
	}
	changed := profileClientScope(t)
	changed.ConfigDigest = strings.Repeat("b", 64)
	if issued.Matches(3, changed) {
		t.Fatal("changed configuration matched")
	}
	service := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_current_service"), server.Client()).WithTenantID(3)
	for i := 0; i < 2; i++ {
		observed, err := service.CheckManagerProfileAccess(context.Background(), issued.ID, profileClientRequest(t))
		if err != nil || observed.ObservedAt.IsZero() {
			t.Fatalf("consume=%+v error=%v", observed, err)
		}
	}
	if calls != 3 {
		t.Fatal("current observation was cached")
	}
}

func TestManagerProfileClientRejectsExpandedAndInvalidResponses(t *testing.T) {
	for _, mutate := range []func(*IssuedManagerProfileAuthorization){
		func(r *IssuedManagerProfileAuthorization) { r.ID = "09" },
		func(r *IssuedManagerProfileAuthorization) { r.TenantID = "0" },
		func(r *IssuedManagerProfileAuthorization) { r.Audience = "develop" },
		func(r *IssuedManagerProfileAuthorization) { r.ExecutionID = profileClientLease },
		func(r *IssuedManagerProfileAuthorization) { r.SourceReadScope.ConfigDigest = "" },
		func(r *IssuedManagerProfileAuthorization) { r.ExpiresAt = time.Now().Add(-time.Second) },
		func(r *IssuedManagerProfileAuthorization) { r.ExpiresAt = time.Now().Add(2 * time.Hour) },
	} {
		response := profileClientIssued(t)
		mutate(&response)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201); json.NewEncoder(w).Encode(response) }))
		issued, err := NewSystemExecutionAuthorizationClient(server.URL, server.Client()).IssueManagerProfile(context.Background(), "addp_at_user", IssueManagerProfileAuthorizationRequest{ExecutionID: profileClientExecution})
		server.Close()
		if err == nil || issued != nil {
			t.Fatal("invalid issuance returned")
		}
	}
	for _, body := range []string{`{}`, `{"observed_at":"2026-10-06T00:00:00Z","connection":"secret"}`, `{"observed_at":"2026-10-06T00:00:00Z","access_lease":"secret"}`, `{"observed_at":"2026-10-06T00:00:00Z"}{}`, strings.Repeat("x", 513<<10)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		observed, err := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).WithTenantID(3).CheckManagerProfileAccess(context.Background(), "9", profileClientRequest(t))
		server.Close()
		if err == nil || observed != nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("expanded observation accepted: %v", err)
		}
	}
}

func TestManagerProfileClientNeverRetriesOrLeaksErrorBody(t *testing.T) {
	for _, status := range []int{400, 401, 403, 409, 500} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(status)
			w.Write([]byte(`{"error":"private upstream password","error_code":"unsafe_private_code"}`))
		}))
		observed, err := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).WithTenantID(3).CheckManagerProfileAccess(context.Background(), "9", profileClientRequest(t))
		server.Close()
		var apiError *SystemAPIError
		if observed != nil || !errors.As(err, &apiError) || apiError.StatusCode != status || calls != 1 || apiError.ErrorMessage != "" || apiError.ResponseBody != "" || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "addp_at_") {
			t.Fatalf("unsafe/retried error status=%d calls=%d error=%v", status, calls, err)
		}
	}
	redirectCalls := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirectCalls++; w.Write([]byte(`{}`)) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer server.Close()
	transport := server.Client()
	_, err := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), transport).WithTenantID(3).CheckManagerProfileAccess(context.Background(), "9", profileClientRequest(t))
	if err == nil || redirectCalls != 0 || transport.CheckRedirect != nil {
		t.Fatal("redirect followed or caller client mutated")
	}
}

func TestManagerProfileClientRejectsBadInputBeforeNetwork(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer server.Close()
	issuer := NewSystemExecutionAuthorizationClient(server.URL, server.Client())
	for _, tc := range []struct {
		credential, id string
		ttl            int64
	}{
		{"", profileClientExecution, 0}, {"addp_dat_delegate", profileClientExecution, 0}, {"addp_at_", profileClientExecution, 0},
		{"addp_at_user", strings.ToUpper(profileClientExecution), 0}, {"addp_at_user", profileClientExecution, -1}, {"addp_at_user", profileClientExecution, 3601},
	} {
		if _, err := issuer.IssueManagerProfile(context.Background(), tc.credential, IssueManagerProfileAuthorizationRequest{ExecutionID: tc.id, ExpiresIn: tc.ttl}); err == nil {
			t.Fatal("bad issuance input accepted")
		}
	}
	for _, token := range []string{"", "addp_dat_delegate", "addp_at_", "addp_at_bad\nvalue"} {
		if _, err := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource(token), server.Client()).WithTenantID(3).CheckManagerProfileAccess(context.Background(), "9", profileClientRequest(t)); err == nil {
			t.Fatal("bad service credential accepted")
		}
	}
	service := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).WithTenantID(3)
	if _, err := service.CheckManagerProfileAccess(context.Background(), "09", profileClientRequest(t)); err == nil {
		t.Fatal("noncanonical authorization accepted")
	}
	request := profileClientRequest(t)
	request.Attempt = 0
	if _, err := service.CheckManagerProfileAccess(context.Background(), "9", request); err == nil {
		t.Fatal("invalid attempt accepted")
	}
	if _, err := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).CheckManagerProfileAccess(context.Background(), "9", profileClientRequest(t)); err == nil {
		t.Fatal("unbound Tenant client accepted")
	}
	if calls != 0 {
		t.Fatal("invalid input reached network")
	}
}
