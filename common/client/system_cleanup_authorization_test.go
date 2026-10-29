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
	"github.com/google/uuid"
)

func TestCleanupAuthorizationHTTPBoundary(t *testing.T) {
	scope := execution.CleanupAuthorizationScope{RequestID: uuid.NewString(), ParentExecutionID: uuid.NewString(), RequestDigest: strings.Repeat("a", 64), Module: "meta"}
	request := CleanupAccessRequest{ExecutionID: uuid.NewString(), Stage: "effect", Attempt: 1, LeaseToken: uuid.NewString(), Scope: scope}
	response := CleanupAccess{AuthorizationID: "1", ExecutionID: request.ExecutionID, TenantID: "5", Stage: "effect", Attempt: 1, Scope: scope, ExpiresAt: time.Now().Add(time.Minute)}
	mode := "ok"
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected = true
		t.Error("authorization redirect followed")
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/oauth/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "addp_at_runtime", "token_type": "Bearer", "expires_in": 300, "scope": "addp.api"})
			return
		}
		if r.URL.Path != "/api/v1/system/execution-authorizations/1/cleanup-accesses" || r.Header.Get("Authorization") != "Bearer addp_at_runtime" {
			t.Error("runtime boundary changed")
			w.WriteHeader(403)
			return
		}
		var actual CleanupAccessRequest
		if json.NewDecoder(r.Body).Decode(&actual) != nil || actual != request {
			t.Error("cleanup request boundary changed")
		}
		switch mode {
		case "redirect":
			http.Redirect(w, r, target.URL, 307)
		case "oversized":
			_, _ = w.Write([]byte(strings.Repeat(" ", 17*1024) + `{}`))
		case "extra":
			encoded, _ := json.Marshal(response)
			_, _ = w.Write(append(encoded[:len(encoded)-1], []byte(`,"engine":{"password":"private"}}`)...))
		case "multiple":
			_ = json.NewEncoder(w).Encode(response)
			_, _ = w.Write([]byte(`{}`))
		default:
			_ = json.NewEncoder(w).Encode(response)
		}
	}))
	defer server.Close()
	source, err := NewOAuthServiceTokenSource(server.URL, "addp-meta", testServiceClientSecret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client := NewSystemServiceClient(server.URL, source, server.Client()).WithTenantID(5)
	if _, err := client.GetCleanupAccess(t.Context(), "1", request); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*CleanupAccess){
		func(a *CleanupAccess) { a.AuthorizationID = "2" }, func(a *CleanupAccess) { a.ExecutionID = uuid.NewString() }, func(a *CleanupAccess) { a.TenantID = "6" }, func(a *CleanupAccess) { a.Scope.Module = "graph" }, func(a *CleanupAccess) { a.Scope.RequestDigest = strings.Repeat("b", 64) }, func(a *CleanupAccess) { a.Stage = "claim" }, func(a *CleanupAccess) { a.Attempt++ }, func(a *CleanupAccess) { a.ExpiresAt = time.Now().Add(-time.Minute) },
	} {
		saved := response
		mutate(&response)
		if _, err := client.GetCleanupAccess(context.Background(), "1", request); err == nil {
			t.Fatal("changed grant accepted")
		}
		response = saved
	}
	for _, value := range []string{"redirect", "oversized", "extra", "multiple"} {
		mode = value
		if _, err := client.GetCleanupAccess(t.Context(), "1", request); err == nil {
			t.Fatal("invalid response accepted", value)
		}
	}
	if redirected {
		t.Fatal("redirect target reached")
	}
	mode = "ok"
	request.Stage = "claim"
	request.Attempt = 0
	request.LeaseToken = ""
	response.Stage = "claim"
	response.Attempt = 0
	if _, err := client.GetCleanupAccess(t.Context(), "1", request); err != nil {
		t.Fatal("claim", err)
	}
	request.LeaseToken = uuid.NewString()
	if _, err := client.GetCleanupAccess(t.Context(), "1", request); err == nil {
		t.Fatal("claim with lease accepted")
	}
}
