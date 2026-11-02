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
	"github.com/addp/common/models"
	"github.com/google/uuid"
)

type cleanupConnectionToken struct{}

func (cleanupConnectionToken) Token(context.Context, uint) (string, error) { return "fixture", nil }
func (cleanupConnectionToken) InvalidateToken(uint, string)                {}
func (cleanupConnectionToken) PlatformToken(context.Context) (string, error) {
	return "", context.Canceled
}

func TestCleanupConnectionRejectsForeignBindingsAndSensitiveErrors(t *testing.T) {
	request := CleanupEngineConnectionRequest{ExecutionID: uuid.NewString(), Attempt: 1, LeaseToken: uuid.NewString(), EngineID: "9", Scope: execution.CleanupAuthorizationScope{RequestID: uuid.NewString(), ParentExecutionID: uuid.NewString(), RequestDigest: strings.Repeat("a", 64), Module: "transfer"}}
	valid := func() CleanupEngineConnection {
		return CleanupEngineConnection{AuthorizationID: "1", ExecutionID: request.ExecutionID, TenantID: "7", Attempt: 1, Scope: request.Scope, Purpose: execution.CleanupEngineConnectionPurpose, EngineID: "9", ExpiresAt: time.Now().Add(time.Minute), Engine: &CleanupConnectionEngine{ID: 9, TenantID: 7, EngineType: "postgresql", LifecycleState: "deleting", ConnectionInfo: models.ConnectionInfo{"password": "private-connection-sentinel"}}}
	}
	mode := "valid"
	var mutate func(*CleanupEngineConnection)
	reads, redirects := 0, 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects++ }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads++
		var body CleanupEngineConnectionRequest
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/system/execution-authorizations/1/cleanup-engine-connection" || r.Header.Get("Authorization") != "Bearer fixture" || json.NewDecoder(r.Body).Decode(&body) != nil || body != request {
			t.Error("connection request escaped exact binding")
			w.WriteHeader(403)
			return
		}
		if mode == "redirect" {
			http.Redirect(w, r, target.URL, 302)
			return
		}
		if mode == "unauthorized" {
			w.WriteHeader(401)
			return
		}
		if mode == "error" {
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"message":"private-connection-sentinel"}`))
			return
		}
		if mode == "oversize" {
			_, _ = w.Write([]byte(strings.Repeat("private-connection-sentinel", 1024)))
			return
		}
		value := valid()
		if mutate != nil {
			mutate(&value)
		}
		if mode == "extra" {
			raw, _ := json.Marshal(value)
			_, _ = w.Write(append(raw[:len(raw)-1], []byte(`,"private":"private-connection-sentinel"}`)...))
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	}))
	defer server.Close()
	c := NewSystemServiceClient(server.URL, cleanupConnectionToken{}, server.Client()).WithTenantID(7)
	if value, err := c.GetCleanupEngineConnection(t.Context(), "1", request); err != nil || value.Engine.ID != 9 {
		t.Fatal("valid cleanup connection rejected")
	}
	for _, change := range []func(*CleanupEngineConnection){
		func(v *CleanupEngineConnection) { v.AuthorizationID = "2" },
		func(v *CleanupEngineConnection) { v.ExecutionID = uuid.NewString() },
		func(v *CleanupEngineConnection) { v.TenantID = "8" },
		func(v *CleanupEngineConnection) { v.Attempt = 2 },
		func(v *CleanupEngineConnection) { v.Scope.RequestDigest = strings.Repeat("b", 64) },
		func(v *CleanupEngineConnection) { v.Purpose = "data_read" },
		func(v *CleanupEngineConnection) { v.EngineID = "10" },
		func(v *CleanupEngineConnection) { v.Engine.ID = 10 },
		func(v *CleanupEngineConnection) { v.Engine.TenantID = 8 },
		func(v *CleanupEngineConnection) { v.Engine.LifecycleState = "active" },
		func(v *CleanupEngineConnection) { v.Engine.ConnectionInfo = nil },
		func(v *CleanupEngineConnection) { v.ExpiresAt = time.Now().Add(-time.Minute) },
	} {
		mutate = change
		if _, err := c.GetCleanupEngineConnection(t.Context(), "1", request); err == nil || strings.Contains(err.Error(), "private-connection-sentinel") {
			t.Fatal("foreign binding accepted or credentials retained in error")
		}
	}
	mutate = nil
	for _, value := range []string{"redirect", "error", "oversize", "extra"} {
		mode = value
		if _, err := c.GetCleanupEngineConnection(t.Context(), "1", request); err == nil || strings.Contains(err.Error(), "private-connection-sentinel") {
			t.Fatal("unsafe response accepted or raw body retained")
		}
	}
	if redirects != 0 {
		t.Fatal("credential request followed redirect")
	}
	mode = "unauthorized"
	before := reads
	if _, err := c.GetCleanupEngineConnection(t.Context(), "1", request); err == nil || reads != before+1 {
		t.Fatal("connection decision retried after 401")
	}
	before = reads
	request.Scope.Module = "manager"
	if _, err := c.GetCleanupEngineConnection(t.Context(), "1", request); err == nil || reads != before {
		t.Fatal("foreign Owner reached connection endpoint")
	}
}
