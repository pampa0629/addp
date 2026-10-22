package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

// A real shared SDK talks to an HTTP protocol fixture. This proves the owner
// boundary and repeated consumption, not a real System Grant or FTP read.
func TestGLBSourceAccessUsesOfficialSDKForEachExactReadAndCheckpoint(t *testing.T) {
	source, item := model3DGLBCandidateFixture("3ds")
	candidate, err := model3DGLBCandidateFromMeta(7, source, item)
	if err != nil {
		t.Fatal(err)
	}
	tenant := uint(7)
	engine := &commonModels.Engine{ID: 33, TenantID: &tenant, EngineType: "ftp", Version: 5, LifecycleState: "active", ConnectionStatus: "online", ConnectionInfo: commonModels.ConnectionInfo{"host": "fixture.invalid", "password": "fixture-only"}}
	core, _ := json.Marshal(map[string]any{"source": map[string]any{"item_locator": source.ItemLocator, "source_engine_id": 33, "item_id": 41, "item_fingerprint": source.ItemFingerprint, "format": "3ds"}, "result": map[string]any{"storage_ref": "previous", "file_name": "model.glb"}, "options": map[string]any{"number": json.Number("9007199254740993")}})
	executionID := uuid.NewString()
	frozen, err := freezeFileModel3DGLBConfig(&repository.Model3DGLBTaskSnapshot{TaskID: 17, TenantID: 7, Version: 3, Config: core}, engine, candidate, executionID, "manager")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(frozen)
	header, _ := decodeModel3DGLBFrozenHeader(raw)
	expected, err := commonExecution.NewManagerModel3DReadScope(raw, "17", "3", "5", header.Composition)
	if err != nil {
		t.Fatal(err)
	}
	actor, principal, membership, version, authorization := 31, int64(31), int64(41), int64(1), int64(51)
	expires := time.Now().Add(time.Hour)
	lease := commonExecution.Lease{ExecutionID: executionID, TenantID: 7, Attempt: 1, Token: uuid.NewString(), Owner: "model-source-protocol-test"}
	row := &commonExecution.TaskExecution{ExecutionID: executionID, TenantID: 7, Module: commonExecution.ModuleManager, TaskType: commonExecution.TaskTypeModel3DGLBGeneration, Source: commonExecution.ModuleManager, SourceTaskID: commonExecution.NewSourceTaskIDFromUint(17), TriggerType: commonExecution.TriggerTypeManual, ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, Status: commonExecution.ExecutionStatusRunning, Attempt: 1, TriggeredBy: &actor, ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version, ExecutionAuthorizationID: &authorization, AuthorizationExpiresAt: &expires, LeaseToken: &lease.Token, LeaseOwner: &lease.Owner, LeaseExpiresAt: &expires}
	ctx := commonExecution.ContextWithLease(t.Context(), lease)
	var calls atomic.Int64
	var status atomic.Int32
	status.Store(http.StatusOK)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/api/v1/system/execution-authorizations/51/manager-model3d-glb-accesses" || r.Header.Get("Authorization") != "Bearer addp_at_current_manager_service" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Error("incorrect fixed consumption protocol")
		}
		var request commonClient.ManagerModel3DAccessRequest
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.ExecutionID != executionID || request.Attempt != 1 || request.LeaseToken != lease.Token || !reflect.DeepEqual(&request.SourceReadScope, expected) {
			t.Error("changed scope or lease on wire")
		}
		w.Header().Set("Cache-Control", "no-store")
		responseStatus := int(status.Load())
		if responseStatus != http.StatusOK {
			w.WriteHeader(responseStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "must-not-escape-upstream-body"})
			return
		}
		responseEngine := *engine
		responseEngine.ConnectionInfo = commonModels.ConnectionInfo{"host": "fixture.invalid", "observation": call}
		_ = json.NewEncoder(w).Encode(commonClient.ManagerModel3DAccessObservation{ObservedAt: time.Now().UTC(), Engine: &responseEngine})
	}))
	t.Cleanup(server.Close)
	tokens := &profileServiceTokenForTest{}
	client := commonClient.NewSystemServiceClient(server.URL, tokens, server.Client())
	guard, err := newModel3DGLBSourceAuthorization(ctx, client, row, raw, candidate)
	if err != nil {
		t.Fatal(err)
	}
	conversion, err := decodeFileModel3DGLBConversion(raw)
	if err != nil || guard.ValidateConversion(conversion) != nil {
		t.Fatal("same raw conversion did not match")
	}
	for _, field := range []string{"version", "source", "result", "options"} {
		changed, _ := decodeFileModel3DGLBConversion(raw)
		switch field {
		case "version":
			changed.ConfigVersion = "unknown"
		case "source":
			changed.Source.ItemID++
		case "result":
			changed.Result.StorageRef = "different-output"
		case "options":
			changed.Options["number"] = json.Number("9007199254740992")
		}
		if err := guard.ValidateConversion(changed); !errors.Is(err, ErrModel3DGLBSourceAuthorizationRequired) || calls.Load() != 0 {
			t.Fatal("changed conversion reached consumption")
		}
	}
	target, _ := candidate.composition.PrimaryTarget()
	first, err := guard.BeforeSourceRead(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	second, err := guard.BeforeSourceRead(ctx, target)
	if err != nil || first.ConnectionInfo["observation"] == second.ConnectionInfo["observation"] || calls.Load() != 2 {
		t.Fatal("source read reused a prior observation")
	}
	copy := guard.Composition()
	copy.Refs[0].Target.EngineID++
	declarations, _ := guard.DeclaredTextureRefs()
	declarations[0] = "mutated.png"
	if again, _ := guard.DeclaredTextureRefs(); again[0] != "TEST.PNG" {
		t.Fatal("declarations were mutable through getter")
	}
	if err := guard.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := guard.BeforePublish(ctx); err != nil || calls.Load() != 4 {
		t.Fatalf("checkpoints did not consume: %v", err)
	}
	if _, err := guard.BeforeSourceRead(ctx, plugin.FileItemPath(33, "models/not-in-composition.png")); !errors.Is(err, ErrModel3DGLBSourceAuthorizationRequired) || calls.Load() != 4 {
		t.Fatal("unapproved target reached System or source")
	}
	changed := lease
	changed.Token = uuid.NewString()
	if err := guard.Checkpoint(commonExecution.ContextWithLease(context.Background(), changed)); !errors.Is(err, ErrModel3DGLBSourceAuthorizationRequired) || calls.Load() != 4 {
		t.Fatal("changed lease reached consumption")
	}
	status.Store(http.StatusForbidden)
	if err := guard.Checkpoint(ctx); !errors.Is(err, ErrModel3DGLBSourceAuthorizationRequired) || calls.Load() != 5 || strings.Contains(err.Error(), "must-not-escape") {
		t.Fatal("revoked scope was reused or leaked upstream diagnostics")
	}
	status.Store(http.StatusServiceUnavailable)
	if err := guard.BeforePublish(ctx); !errors.Is(err, ErrModel3DGLBSourceAuthorizationUnavailable) || calls.Load() != 6 {
		t.Fatal("temporary failure became an Allow or retry")
	}
	if !reflect.DeepEqual(tokens.tenants, []uint{7, 7, 7, 7, 7, 7}) {
		t.Fatal("consumption changed tenant")
	}
}
