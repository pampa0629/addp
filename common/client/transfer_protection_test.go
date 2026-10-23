package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/google/uuid"
)

func TestTransferProtectionUsesOnlyCurrentUserAndStrictBoundedResponses(t *testing.T) {
	for _, mode := range []string{"valid", "unknown", "duplicate", "redirect", "forbidden", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer addp_at_current" || r.Header.Get("X-Tenant-ID") != "" {
					t.Error("borrowed a service or tenant selector")
				}
				if mode == "redirect" {
					w.Header().Set("Location", "/other")
					w.WriteHeader(302)
					return
				}
				if mode == "forbidden" {
					w.WriteHeader(403)
					w.Write([]byte("PRIVATE_RESPONSE"))
					return
				}
				if mode == "oversize" {
					w.Write([]byte(strings.Repeat("x", 257<<10)))
					return
				}
				if r.Method == "GET" {
					if r.URL.Query().Get("source_locator") != "addp://engine/11/path/public/persons?type=table" {
						t.Error("source lost")
					}
					if mode == "unknown" {
						w.Write([]byte(`{"fields":[],"roles":[]}`))
						return
					}
					if mode == "duplicate" {
						w.Write([]byte(`{"fields":[],"fields":[]}`))
						return
					}
					w.Write([]byte(`{"fields":[]}`))
					return
				}
				var req TransferSourceProtectionSelection
				if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Choices) != 1 {
					t.Error("selection changed")
				}
				if mode == "unknown" {
					w.Write([]byte(`{"valid":true,"roles":[]}`))
					return
				}
				if mode == "duplicate" {
					w.Write([]byte(`{"valid":false,"valid":true}`))
					return
				}
				w.Write([]byte(`{"valid":true}`))
			}))
			defer server.Close()
			c := NewTransferClient(server.URL, nil)
			_, err := c.SourceProtectionOptions(t.Context(), "addp_at_current", "addp://engine/11/path/public/persons?type=table")
			if (err == nil) != (mode == "valid") {
				t.Fatal("response accepted incorrectly", err)
			}
			err = c.ValidateSourceProtection(t.Context(), "addp_at_current", &TransferSourceProtectionSelection{SourceLocator: "source", Choices: []dataprotection.FieldRoleChoice{{Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item"}, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: "policy", PolicyRevision: 1, AssessmentRevision: 1, OptionID: "hash"}}}})
			if (err == nil) != (mode == "valid") {
				t.Fatal("check response accepted incorrectly", err)
			}
			if calls != 2 {
				t.Fatal("followed redirect, retried, or cached current qualification")
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE_RESPONSE") {
				t.Fatal("private response leaked")
			}
		})
	}
}
func TestTransferAuthorizationBindingAndDigestIncludesAuthority(t *testing.T) {
	id := uuid.NewString()
	a := &IssuedExecutionAuthorization{ID: "81", ExecutionID: id, Audience: "transfer", TenantID: "7", ActorPrincipalID: "9", TenantMembershipID: "13", IssuedAuthorizationVersion: "2", SourceType: "user", ExpiresAt: time.Now().Add(time.Minute)}
	if err := ValidateTransferUserAuthorization(a, 7, 9, id); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*IssuedExecutionAuthorization){func(a *IssuedExecutionAuthorization) { a.SourceType = "service_definition" }, func(a *IssuedExecutionAuthorization) { a.ExecutionID = uuid.NewString() }, func(a *IssuedExecutionAuthorization) { a.ActorPrincipalID = "10" }, func(a *IssuedExecutionAuthorization) { a.TenantID = "8" }, func(a *IssuedExecutionAuthorization) { a.ExpiresAt = time.Now().Add(-time.Second) }} {
		bad := *a
		mutate(&bad)
		if ValidateTransferUserAuthorization(&bad, 7, 9, id) == nil {
			t.Fatal("mismatched authority accepted")
		}
	}
	req := &CreateTransferExecutionRequest{Name: "export", ExecutionAuthorization: a}
	before, err := ExportRequestDigest(req)
	if err != nil {
		t.Fatal(err)
	}
	a.ID = "82"
	after, _ := ExportRequestDigest(req)
	if before == after {
		t.Fatal("authority excluded from request digest")
	}
}
