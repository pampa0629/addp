package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
)

func TestCurrentSubjectRolesClientBindingAndSingleTransport(t *testing.T) {
	current := authtest.NewTenantUserAuthContext("3", "33", []string{"develop.task.create"})
	for _, test := range []string{"valid", "empty", "actor", "tenant", "membership", "version", "expired", "duplicate", "unknown", "redirect", "forbidden"} {
		t.Run(test, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/api/v1/system/auth/subject-roles" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer addp_at_request_user" || r.Header.Get("X-Tenant-ID") != "" || r.ContentLength > 0 {
					t.Error("role request used wrong identity or transport")
				}
				roles := authorization.CurrentSubjectRoles{TenantID: "3", ActorPrincipalID: "33", TenantMembershipID: *current.Context.TenantMembershipID, AuthorizationVersion: current.Authorization.AuthorizationVersion, RoleKeys: []string{"custom.analysis"}, ObservedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}
				switch test {
				case "empty":
					roles.RoleKeys = []string{}
				case "actor":
					roles.ActorPrincipalID = "1"
				case "tenant":
					roles.TenantID = "4"
				case "membership":
					roles.TenantMembershipID = "9"
				case "version":
					roles.AuthorizationVersion = "99"
				case "expired":
					roles.ExpiresAt = time.Now().Add(-time.Second)
				case "redirect":
					w.Header().Set("Location", "/redirected")
					w.WriteHeader(302)
					return
				case "forbidden":
					w.WriteHeader(403)
					w.Write([]byte(`{"error":"PRIVATE_UPSTREAM"}`))
					return
				}
				body, _ := json.Marshal(roles)
				if test == "duplicate" {
					body = []byte(strings.TrimSuffix(string(body), "}") + `,"tenant_id":"3"}`)
				}
				if test == "unknown" {
					body = []byte(strings.TrimSuffix(string(body), "}") + `,"unexpected":true}`)
				}
				w.Write(body)
			}))
			defer server.Close()
			client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_must_not_use_service"), server.Client()).WithTenantID(999)
			for i := 0; i < 2; i++ {
				result, err := client.ResolveCurrentSubjectRoles(context.Background(), "addp_at_request_user", &current)
				valid := test == "valid" || test == "empty"
				if valid && (err != nil || result == nil) || !valid && (err == nil || result != nil) {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				if err != nil && strings.Contains(err.Error(), "PRIVATE_UPSTREAM") {
					t.Fatal("upstream private body leaked")
				}
			}
			if calls != 2 {
				t.Fatal("role observation cached, redirected or retried")
			}
		})
	}
}
