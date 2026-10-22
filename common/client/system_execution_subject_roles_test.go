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

func TestExecutionSubjectRolesClient(t *testing.T) {
	auth := &IssuedExecutionAuthorization{ID: "9", ExecutionID: profileClientExecution, Audience: "develop", SourceType: "user", TenantID: "3", ActorPrincipalID: "33", TenantMembershipID: "34", IssuedAuthorizationVersion: "5", ExpiresAt: time.Now().Add(time.Hour)}
	req := ExecutionSubjectRolesRequest{ExecutionID: auth.ExecutionID, Attempt: 2, LeaseToken: profileClientLease}
	response := func() execution.SubjectRoles {
		return execution.SubjectRoles{AuthorizationID: "9", ExecutionID: auth.ExecutionID, Attempt: 2, Owner: "develop", TenantID: "3", ActorPrincipalID: "33", TenantMembershipID: "34", AuthorizationVersion: "5", RoleKeys: []string{"tenant.data_engineer"}, ObservedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*execution.SubjectRoles)
	}{
		{"valid", nil}, {"empty_roles", func(r *execution.SubjectRoles) { r.RoleKeys = []string{} }},
		{"actor", func(r *execution.SubjectRoles) { r.ActorPrincipalID = "1" }},
		{"tenant", func(r *execution.SubjectRoles) { r.TenantID = "4" }},
		{"membership", func(r *execution.SubjectRoles) { r.TenantMembershipID = "99" }},
		{"version", func(r *execution.SubjectRoles) { r.AuthorizationVersion = "6" }},
		{"attempt", func(r *execution.SubjectRoles) { r.Attempt++ }},
		{"execution", func(r *execution.SubjectRoles) { r.ExecutionID = profileClientLease }},
		{"owner", func(r *execution.SubjectRoles) { r.Owner = "transfer" }},
		{"authorization", func(r *execution.SubjectRoles) { r.AuthorizationID = "10" }},
		{"expired", func(r *execution.SubjectRoles) { r.ExpiresAt = time.Now().Add(-time.Second) }},
		{"unbounded", func(r *execution.SubjectRoles) { r.ExpiresAt = auth.ExpiresAt.Add(time.Second) }},
		{"unobserved", func(r *execution.SubjectRoles) { r.ObservedAt = time.Time{} }},
		{"null_roles", func(r *execution.SubjectRoles) { r.RoleKeys = nil }},
		{"duplicate_roles", func(r *execution.SubjectRoles) { r.RoleKeys = []string{"tenant.administrator", "tenant.administrator"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/v1/system/execution-authorizations/9/subject-roles" || r.Header.Get("Authorization") != "Bearer addp_at_current_service" {
					t.Error("wrong transport")
				}
				var got ExecutionSubjectRolesRequest
				if json.NewDecoder(r.Body).Decode(&got) != nil || got != req {
					t.Error("missing exact lease")
				}
				body := response()
				if tc.mutate != nil {
					tc.mutate(&body)
				}
				json.NewEncoder(w).Encode(body)
			}))
			defer server.Close()
			client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_current_service"), server.Client()).WithTenantID(3)
			for i := 0; i < 2; i++ {
				got, err := client.ResolveExecutionSubjectRoles(context.Background(), auth, "develop", req)
				valid := tc.name == "valid" || tc.name == "empty_roles"
				if valid && (err != nil || got == nil) || !valid && (err == nil || got != nil) {
					t.Fatalf("roles=%+v err=%v", got, err)
				}
			}
			if calls != 2 {
				t.Fatal("observation cached or retried")
			}
		})
	}
	for _, tc := range []string{"redirect", "unknown", "duplicate", "forbidden"} {
		t.Run(tc, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch tc {
				case "redirect":
					w.Header().Set("Location", "/redirected")
					w.WriteHeader(307)
				case "unknown":
					b, _ := json.Marshal(response())
					w.Write(append(b[:len(b)-1], []byte(`,"permissions":["private"]}`)...))
				case "duplicate":
					b, _ := json.Marshal(response())
					w.Write(append(b[:len(b)-1], []byte(`,"attempt":2}`)...))
				default:
					w.WriteHeader(403)
					w.Write([]byte(`{"error":"private raw detail"}`))
				}
			}))
			defer server.Close()
			client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_current_service"), server.Client()).WithTenantID(3)
			got, err := client.ResolveExecutionSubjectRoles(context.Background(), auth, "develop", req)
			if got != nil || err == nil || strings.Contains(err.Error(), "private") || calls != 1 {
				t.Fatalf("unsafe transport: calls=%d result=%+v err=%v", calls, got, err)
			}
		})
	}
}
