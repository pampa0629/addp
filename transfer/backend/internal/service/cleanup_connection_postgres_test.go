package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/cleanup"
	"github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/capture"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type connectionPipelineCapture struct {
	*fakeCaptureControl
	db       *gorm.DB
	resolver *capture.DatabasePlanResolver
	native   int
	unknown  bool
}

func (c *connectionPipelineCapture) Stop(ctx context.Context, task *models.TransferTask) error {
	plan, err := c.resolver.ResolveForCleanup(ctx, task)
	if err != nil {
		return err
	}
	if plan.SourceEngineID != 9 || plan.SourceIdentity != "addp://engine/9/path/public/orders?type=table" || plan.SourceDatabase != "business" {
		return errors.New("cleanup pipeline source binding differs")
	}
	return repository.ExecuteCleanupOperation(ctx, c.db, task.TenantID, task.ID, "capture_source", nil, struct {
		Engine      uint
		Fingerprint string
	}{plan.SourceEngineID, plan.SourceConnectionFingerprint}, func(context.Context) error {
		c.native++ // A fake native call; the PG operation and C fences are real.
		if c.unknown {
			return errors.New("unknown native outcome")
		}
		return nil
	})
}

func testCleanupConnectionPipelineAgainstPostgres(t *testing.T, db *gorm.DB) {
	for _, mode := range []string{"complete", "connection_denied", "native_unknown"} {
		t.Run(mode, func(t *testing.T) {
			tx := db.Begin()
			defer tx.Rollback()
			if err := tx.Exec("DROP SCHEMA IF EXISTS transfer CASCADE; CREATE SCHEMA transfer; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
				t.Fatal(err)
			}
			if err := execution.EnsureStore(tx); err != nil {
				t.Fatal(err)
			}
			if err := tx.AutoMigrate(&models.TransferTask{}, &models.DeadLetter{}, &models.RuntimeLease{}, &models.RuntimeSession{}, &models.CleanupOperation{}); err != nil {
				t.Fatal(err)
			}
			if err := cleanup.MigrateResults(tx, "transfer"); err != nil {
				t.Fatal(err)
			}
			config := validPostgreSQLCDCTaskConfig()
			config["source"].(map[string]interface{})["locator"] = "addp://engine/9/path/public/orders?type=table"
			task := models.TransferTask{TenantID: 7, Name: "connection-owned", TaskType: "sync", Config: models.JSONMap(config), Enabled: true, Status: models.TaskStatusIdle, DesiredState: models.TaskDesiredStatePaused}
			if err := tx.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "7", []string{"system.engine.delete"}))
			parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Module: "system", TaskType: "cleanup", Source: "system", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", MaxAttempts: 3, ExecutionConfig: commonmodels.JSONMap{execution.DisplayLanguageKey: "en"}}
			if err := execution.ApplyUserOrigin(ctx, &parent); err != nil {
				t.Fatal(err)
			}
			now, _ := execution.DatabaseNow(tx)
			input := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parent.ExecutionID, TenantID: 7, RequestedBy: 7, Action: "execute", CleanupMode: events.CleanupModePhysical, CauseEvent: events.CleanupCauseEngineDeleting, TriggerType: "manual", ExpectedModules: []string{"transfer"}, BasedOnScan: uuid.NewString(), Context: events.CleanupContext{EngineID: 9, ExternalArtifactPolicy: "delete"}, Confirmation: events.CleanupConfirmation{Confirmed: true, TextProvided: true, RiskLevel: "low", ImpactDigest: strings.Repeat("a", 64)}, RequestedAt: now, Deadline: now.Add(time.Minute)}
			if err := input.Seal(); err != nil {
				t.Fatal(err)
			}
			children, err := execution.CreateCleanupExecutions(ctx, tx, &parent, input)
			if err != nil {
				t.Fatal(err)
			}
			child := children[0]
			if err := execution.AttachPendingAuthorization(ctx, tx, execution.PendingIdentity{ExecutionID: child.ExecutionID, TenantID: 7, Module: "transfer", TaskType: "cleanup_executor", Source: "system"}, execution.PendingAuthorization{ID: 1, ExpiresAt: now.Add(time.Hour), ActorPrincipalID: *child.ActorPrincipalID, ActorTenantMembershipID: *child.ActorTenantMembershipID, IssuedAuthorizationVersion: *child.IssuedAuthorizationVersion}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := execution.ClaimPending(ctx, tx, execution.PendingIdentity{ExecutionID: parent.ExecutionID, TenantID: 7, Module: "system", TaskType: "cleanup", Source: "system"}, execution.ClaimOptions{Module: "system", TaskType: "cleanup", Source: "system", WorkerID: "parent", LeaseDuration: time.Minute}, nil); err != nil {
				t.Fatal(err)
			}
			connectionReads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/system/execution-authorizations/1/cleanup-engine-connection" {
					connectionReads++
					var request client.CleanupEngineConnectionRequest
					if json.NewDecoder(r.Body).Decode(&request) != nil || request.ExecutionID != child.ExecutionID || request.EngineID != "9" || request.Scope.Module != "transfer" || request.Attempt != 1 || request.LeaseToken == "" || r.Header.Get("Authorization") != "Bearer fixture" {
						w.WriteHeader(403)
						return
					}
					if mode == "connection_denied" {
						w.WriteHeader(403)
						return
					}
					var running execution.TaskExecution
					if err := tx.Where("execution_id=?", child.ExecutionID).First(&running).Error; err != nil {
						t.Error(err)
						w.WriteHeader(503)
						return
					}
					_ = json.NewEncoder(w).Encode(client.CleanupEngineConnection{AuthorizationID: "1", ExecutionID: request.ExecutionID, TenantID: "7", Attempt: request.Attempt, Scope: request.Scope, Purpose: execution.CleanupEngineConnectionPurpose, EngineID: "9", ExpiresAt: running.LeaseExpiresAt.Add(-time.Millisecond), Engine: &client.CleanupConnectionEngine{ID: 9, TenantID: 7, EngineType: "postgresql", LifecycleState: "deleting", ConnectionInfo: commonmodels.ConnectionInfo{"host": "fixture", "database": "business", "password": "private-connection-sentinel"}}})
					return
				}
				var request client.CleanupAccessRequest
				if r.URL.Path != "/api/v1/system/execution-authorizations/1/cleanup-accesses" || json.NewDecoder(r.Body).Decode(&request) != nil || request.ExecutionID != child.ExecutionID {
					w.WriteHeader(403)
					return
				}
				_ = json.NewEncoder(w).Encode(client.CleanupAccess{AuthorizationID: "1", ExecutionID: request.ExecutionID, TenantID: "7", Stage: request.Stage, Attempt: request.Attempt, Scope: request.Scope, ExpiresAt: now.Add(20 * time.Second)})
			}))
			defer server.Close()
			svc := NewTransferCleanupService(tx, client.NewSystemServiceClient(server.URL, cleanupRuntimeToken{}, server.Client()), transferCleanupTestConfig())
			control := &connectionPipelineCapture{fakeCaptureControl: &fakeCaptureControl{}, db: tx, resolver: capture.NewDatabasePlanResolver(planner.StaticEngineResolver{}), unknown: mode == "native_unknown"}
			svc.SetCaptureControl(control)
			runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			handle := svc.runtime.Handle
			svc.runtime.Handle = func(ctx context.Context, run *cleanup.Run) error { defer cancel(); return handle(ctx, run) }
			if err := svc.runtime.Serve(runCtx); err != nil {
				t.Fatal(err)
			}
			if err := tx.Where("execution_id=?", child.ExecutionID).First(&child).Error; err != nil {
				t.Fatal(err)
			}
			var stored models.TransferTask
			if err := tx.First(&stored, task.ID).Error; err != nil {
				t.Fatal(err)
			}
			receipt, err := execution.ReadCleanupReceipt(child)
			if err != nil || connectionReads != 1 {
				t.Fatal("connection decision duplicated or receipt invalid")
			}
			var operations []models.CleanupOperation
			if err := tx.Find(&operations).Error; err != nil {
				t.Fatal(err)
			}
			if mode == "complete" {
				if child.Status != "success" || receipt == nil || stored.Enabled || control.native != 1 || len(operations) != 1 || operations[0].Status != "succeeded" {
					t.Fatal("connection/operation/result pipeline did not complete")
				}
			} else {
				if child.Status != "failed" || receipt != nil || !stored.Enabled {
					t.Fatal("failed cleanup committed domain or professional result")
				}
				if mode == "connection_denied" && (control.native != 0 || len(operations) != 0) || mode == "native_unknown" && (control.native != 1 || len(operations) != 1 || operations[0].Status != "unknown") {
					t.Fatal("failure lost operation isolation or sent extra effects")
				}
				observation := execution.Observe(&child)
				if observation.CurrentStep == nil || *observation.CurrentStep != "transfer_cleanup_capture_plan" || len(observation.Steps) != 1 || observation.Steps[0].Phase != "terminal" {
					t.Fatal("failure lost closed Owner step")
				}
			}
			encoded, _ := json.Marshal([]any{child, operations})
			if strings.Contains(string(encoded), "private-connection-sentinel") || strings.Contains(string(encoded), "connection_info") {
				t.Fatal("connection persisted in public execution or operation")
			}
		})
	}
}
