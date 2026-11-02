package cleanup

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/google/uuid"
)

func TestCleanupConnectionRechecksCAndNeverPersistsCredentials(t *testing.T) {
	f := newRuntimeFixture(t)
	db := f.runner.DB
	parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Module: "system", TaskType: "cleanup", Source: "system", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", MaxAttempts: 3, ExecutionConfig: models.JSONMap{execution.DisplayLanguageKey: "en"}}
	if err := execution.CopyActorOrigin(&parent, &f.parent); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	input := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parent.ExecutionID, TenantID: 7, RequestedBy: 7, Action: "execute", CleanupMode: events.CleanupModePhysical, TriggerType: "manual", CauseEvent: events.CleanupCauseEngineDeleting, ExpectedModules: []string{"transfer"}, BasedOnScan: uuid.NewString(), Context: events.CleanupContext{EngineID: 9, ExternalArtifactPolicy: "delete"}, Confirmation: events.CleanupConfirmation{Confirmed: true, TextProvided: true, RiskLevel: "low", ImpactDigest: strings.Repeat("a", 64)}, RequestedAt: now, Deadline: now.Add(time.Minute)}
	if err := input.Seal(); err != nil {
		t.Fatal(err)
	}
	children, err := execution.CreateCleanupExecutions(t.Context(), db, &parent, input)
	if err != nil {
		t.Fatal(err)
	}
	child := children[0]
	identity := func(e execution.TaskExecution) execution.PendingIdentity {
		return execution.PendingIdentity{ExecutionID: e.ExecutionID, TenantID: e.TenantID, Module: e.Module, TaskType: e.TaskType, Source: e.Source}
	}
	if err := execution.AttachPendingAuthorization(t.Context(), db, identity(child), execution.PendingAuthorization{ID: 2, ExpiresAt: now.Add(time.Hour), ActorPrincipalID: *child.ActorPrincipalID, ActorTenantMembershipID: *child.ActorTenantMembershipID, IssuedAuthorizationVersion: *child.IssuedAuthorizationVersion}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execution.ClaimPending(t.Context(), db, identity(parent), execution.ClaimOptions{Module: "system", TaskType: "cleanup", Source: "system", WorkerID: "parent-connection", LeaseDuration: time.Minute}, nil); err != nil {
		t.Fatal(err)
	}
	current, lease, err := execution.ClaimCleanupChild(t.Context(), db, identity(child), execution.ClaimOptions{Module: "transfer", TaskType: "cleanup_executor", Source: "system", WorkerID: "owner-connection", LeaseDuration: time.Minute, RequireAuthorization: true}, nil)
	if err != nil || lease == nil {
		t.Fatal(err)
	}
	mode, reads := "ok", 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads++
		var request client.CleanupEngineConnectionRequest
		if r.URL.Path != "/api/v1/system/execution-authorizations/2/cleanup-engine-connection" || json.NewDecoder(r.Body).Decode(&request) != nil {
			w.WriteHeader(403)
			return
		}
		// The fixture has exactly one DB connection. This read would deadlock if
		// the Owner held C while asking System for its connection decision.
		var stored execution.TaskExecution
		if err := db.Where("execution_id=?", child.ExecutionID).First(&stored).Error; err != nil {
			t.Error(err)
			w.WriteHeader(503)
			return
		}
		if mode == "close-c" {
			if err := execution.CompleteWithLease(t.Context(), db, *lease, "failed", nil); err != nil {
				t.Error(err)
			}
		}
		_ = json.NewEncoder(w).Encode(client.CleanupEngineConnection{AuthorizationID: "2", ExecutionID: request.ExecutionID, TenantID: "7", Attempt: request.Attempt, Scope: request.Scope, Purpose: execution.CleanupEngineConnectionPurpose, EngineID: request.EngineID, ExpiresAt: stored.LeaseExpiresAt.Add(-time.Millisecond), Engine: &client.CleanupConnectionEngine{ID: 9, TenantID: 7, EngineType: "postgresql", LifecycleState: "deleting", ConnectionInfo: models.ConnectionInfo{"password": "in-memory-only"}}})
	}))
	defer server.Close()
	runner := &Runner{DB: db, Owner: "transfer", System: client.NewSystemServiceClient(server.URL, runtimeToken{}, server.Client())}
	run := &Run{runner: runner, Execution: *current, Input: input, Lease: *lease}
	if grant, err := run.EngineConnection(t.Context(), 9); err != nil || grant.Engine.ConnectionInfo["password"] != "in-memory-only" {
		t.Fatal("live connection decision failed")
	}
	mode = "close-c"
	if grant, err := run.EngineConnection(t.Context(), 9); err == nil || grant != nil {
		t.Fatal("connection escaped after C became terminal during HTTP")
	}
	var stored execution.TaskExecution
	if err := db.Where("execution_id=?", child.ExecutionID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(stored)
	if strings.Contains(string(encoded), "in-memory-only") || strings.Contains(string(encoded), "connection_info") {
		t.Fatal("connection persisted in execution")
	}
	before := reads
	run.Input.Action = "scan"
	if _, err := run.EngineConnection(t.Context(), 9); err == nil || reads != before {
		t.Fatal("scan requested connection credentials")
	}
}
