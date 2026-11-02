package capture

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
)

type captureConnectionToken struct{}

func (captureConnectionToken) Token(context.Context, uint) (string, error) { return "fixture", nil }

func cleanupConnectionTask() *models.TransferTask {
	return &models.TransferTask{ID: 3, TenantID: 7, TaskType: "sync", Config: commonmodels.JSONMap{
		"runtime":    map[string]interface{}{"boundary": "continuous", "record_failure": map[string]interface{}{"mode": "block"}},
		"load":       map[string]interface{}{"mode": "incremental", "change_detection": map[string]interface{}{"type": "cdc", "bootstrap": "initial_snapshot"}},
		"source":     map[string]interface{}{"locator": "addp://engine/12/path/public/orders?type=table", "data_type": "table", "representation": "native"},
		"target":     map[string]interface{}{"parent_locator": "addp://engine/20/path/public?type=schema", "name": "orders_cdc", "data_type": "table", "representation": "native", "policy": map[string]interface{}{"apply_mode": "upsert_delete", "keys": []interface{}{"id"}}},
		"transforms": []interface{}{map[string]interface{}{"type": "field_mapping", "version": "v1", "mode": "project", "fields": []interface{}{map[string]interface{}{"source": "id", "target": "id", "target_type": "bigint", "nullable": false}}}},
	}}
}

func cleanupConnectionGrant() *client.CleanupEngineConnection {
	return &client.CleanupEngineConnection{EngineID: "12", Purpose: execution.CleanupEngineConnectionPurpose, ExpiresAt: time.Now().Add(time.Minute), Engine: &client.CleanupConnectionEngine{ID: 12, TenantID: 7, EngineType: "postgresql", LifecycleState: "deleting", ConnectionInfo: commonmodels.ConnectionInfo{"host": "fixture", "database": "business", "password": "private-connection-sentinel"}}}
}

func TestCleanupPlanUsesExactPurposeWithoutOrdinaryFallback(t *testing.T) {
	ordinary := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ordinary++
		if r.URL.Path != "/api/v1/system/engines/12" || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("ordinary source escaped exact Tenant Engine")
		}
		w.WriteHeader(409) // Real System connection semantics for deleting.
	}))
	defer server.Close()
	resolver := NewDatabasePlanResolver(planner.NewSystemEngineResolver(client.NewSystemClient(server.URL, captureConnectionToken{})))
	task := cleanupConnectionTask()
	if _, err := resolver.ResolveForCleanup(t.Context(), task); err == nil || ordinary != 1 {
		t.Fatal("ordinary deleting source should remain blocked")
	}
	grant := cleanupConnectionGrant()
	ctx := WithCleanupEngineConnection(t.Context(), 12, func(_ context.Context, id uint) (*client.CleanupEngineConnection, error) {
		if id != 12 {
			t.Fatal("connection scope broadened")
		}
		return grant, nil
	})
	plan, err := resolver.ResolveForCleanup(ctx, task)
	if err != nil || plan.SourceEngineID != 12 || plan.SourceIdentity != "addp://engine/12/path/public/orders?type=table" || plan.cleanupExpiresAt != grant.ExpiresAt || ordinary != 1 {
		t.Fatal("exact cleanup connection was not the sole plan route")
	}
	for _, mode := range []string{"wrong_engine", "wrong_tenant", "wrong_purpose", "wrong_lifecycle", "expired"} {
		grant = cleanupConnectionGrant()
		switch mode {
		case "wrong_engine":
			grant.Engine.ID = 13
		case "wrong_tenant":
			grant.Engine.TenantID = 8
		case "wrong_purpose":
			grant.Purpose = "data_read"
		case "wrong_lifecycle":
			grant.Engine.LifecycleState = "active"
		case "expired":
			grant.ExpiresAt = time.Now().Add(-time.Second)
		}
		if _, err := resolver.ResolveForCleanup(ctx, task); err == nil || ordinary != 1 {
			t.Fatalf("invalid %s accepted or fell back", mode)
		}
	}
	denied := WithCleanupEngineConnection(t.Context(), 12, func(context.Context, uint) (*client.CleanupEngineConnection, error) { return nil, errors.New("denied") })
	if _, err := resolver.ResolveForCleanup(denied, task); err == nil || ordinary != 1 {
		t.Fatal("failed purpose decision fell back to ordinary connection")
	}
	// A different active source belongs to the original ordinary purpose. The
	// cleanup target must never be substituted for that source.
	other := WithCleanupEngineConnection(t.Context(), 20, func(context.Context, uint) (*client.CleanupEngineConnection, error) {
		t.Fatal("cross-target connection requested")
		return nil, nil
	})
	if _, err := resolver.ResolveForCleanup(other, task); err == nil || ordinary != 2 {
		t.Fatal("other source bypassed ordinary availability")
	}
}

type cleanupDeadlineSource struct {
	*fakeSourceResources
	deadline time.Time
	failure  error
	t        *testing.T
}

func (s *cleanupDeadlineSource) DropOwnedResources(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error {
	deadline, ok := ctx.Deadline()
	if !ok || deadline.After(s.deadline) {
		s.t.Fatal("source call outlived connection decision")
	}
	s.calls++
	return s.failure
}

func TestCleanupCaptureValidatesFrozenSourceBeforeEffects(t *testing.T) {
	for _, mode := range []string{"success", "source_changed", "plan_denied", "provider_failed"} {
		t.Run(mode, func(t *testing.T) {
			grant := cleanupConnectionGrant()
			ctx := WithCleanupEngineConnection(t.Context(), 12, func(context.Context, uint) (*client.CleanupEngineConnection, error) {
				if mode == "plan_denied" {
					return nil, errors.New("private-connection-sentinel")
				}
				return grant, nil
			})
			// The ordinary resolver is intentionally unusable for this purpose.
			resolver := NewDatabasePlanResolver(planner.StaticEngineResolver{})
			task := cleanupConnectionTask()
			plan, err := resolver.ResolveForCleanup(ctx, task)
			if mode == "plan_denied" {
				if err == nil {
					t.Fatal("denied plan accepted")
				}
				planCtx := WithCleanupEngineConnection(t.Context(), 12, func(context.Context, uint) (*client.CleanupEngineConnection, error) { return grant, nil })
				plan, err = resolver.ResolveForCleanup(planCtx, task)
			}
			if err != nil {
				t.Fatal("fixture plan invalid")
			}
			resource := &models.CaptureResource{ID: 9, TaskID: 3, TenantID: 7, Generation: 1, Status: models.CaptureStatusRunning, ConnectorName: "owned", SourceEngineID: plan.SourceEngineID, SourceType: plan.SourceType, SourceIdentity: plan.SourceIdentity, SourceConnectionFingerprint: plan.SourceConnectionFingerprint, SourceDatabase: plan.SourceDatabase, SourceSchema: plan.SourceSchema, SourceTable: plan.SourceTable}
			if mode == "source_changed" {
				resource.SourceConnectionFingerprint = "changed"
			}
			store := &fakeCaptureStore{resource: resource}
			connect, topics := &fakeConnectControl{state: "RUNNING"}, &fakeTopicControl{}
			source := &cleanupDeadlineSource{fakeSourceResources: &fakeSourceResources{}, deadline: grant.ExpiresAt, t: t}
			if mode == "provider_failed" {
				source.failure = errors.New("private-connection-sentinel")
			}
			supervisor, err := NewSupervisor(store, resolver, connect, topics, source, SupervisorConfig{TopicRetention: time.Hour, TopicReplication: 1, ProvisioningTimeout: time.Second, StatusPollInterval: time.Millisecond, MonitorInterval: time.Second}, slog.Default())
			if err != nil {
				t.Fatal(err)
			}
			stage := ""
			ctx = WithCleanupObserver(ctx, func(s string) { stage = s })
			err = supervisor.Stop(ctx, task)
			if mode == "success" {
				if err != nil || !connect.deleted || source.calls != 1 || !topics.deleted || resource.Status != models.CaptureStatusStopped {
					t.Fatal("native cleanup did not complete")
				}
			} else {
				if err == nil || strings.Contains(err.Error(), "private-connection-sentinel") || strings.Contains(resource.ConnectorError, "private-connection-sentinel") {
					t.Fatal("failure hidden or credentials retained")
				}
				if mode == "provider_failed" {
					if stage != "transfer_cleanup_capture_source" || !connect.deleted || topics.deleted {
						t.Fatal("source failure phase or operation order lost")
					}
				} else if stage != "transfer_cleanup_capture_plan" || connect.deleted || source.calls != 0 || topics.deleted {
					t.Fatal("invalid plan deleted a native resource")
				}
			}
		})
	}
}
