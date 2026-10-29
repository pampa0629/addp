package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/cleanup"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type cleanupRuntimeToken struct{}

func (cleanupRuntimeToken) Token(context.Context, uint) (string, error) { return "fixture", nil }
func (cleanupRuntimeToken) PlatformToken(context.Context) (string, error) {
	return "", errors.New("platform prohibited")
}

func TestIntegrationPostgresTransferCleanupRuntimeAtomicity(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("standard Transfer PG gate required")
	}
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(testpg.ConnInfoFromEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal("open Transfer PG fixture failed")
	}
	pool, _ := db.DB()
	defer pool.Close()
	for _, mode := range []string{"complete", "effect_denied", "result_rollback"} {
		t.Run(mode, func(t *testing.T) {
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			if err := tx.Exec("DROP SCHEMA IF EXISTS transfer CASCADE; CREATE SCHEMA transfer; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
				t.Fatal(err)
			}
			if err := execution.EnsureStore(tx); err != nil {
				t.Fatal(err)
			}
			if err := tx.AutoMigrate(&models.TransferTask{}, &models.DeadLetter{}); err != nil {
				t.Fatal(err)
			}
			if err := cleanup.MigrateResults(tx, "transfer"); err != nil {
				t.Fatal(err)
			}
			task := createTransferCleanupTestTask(t, tx, 7, "owned", 9, 10, true)
			other := createTransferCleanupTestTask(t, tx, 8, "other-tenant", 9, 10, true)
			unrelated := createTransferCleanupTestTask(t, tx, 7, "other-engine", 11, 12, true)
			for _, id := range []uint{task.ID, other.ID, unrelated.ID} {
				if err := tx.Model(&models.TransferTask{}).Where("id=?", id).Update("status", models.TaskStatusIdle).Error; err != nil {
					t.Fatal(err)
				}
			}
			ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "7", []string{"system.cleanup.execute"}))
			parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Module: "system", TaskType: "cleanup", Source: "system", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", ExecutionConfig: commonmodels.JSONMap{execution.DisplayLanguageKey: "en"}}
			if err := execution.ApplyUserOrigin(ctx, &parent); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			input := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parent.ExecutionID, TenantID: 7, RequestedBy: 7, Action: "execute", CleanupMode: events.CleanupModeLogical, TriggerType: "manual", ExpectedModules: []string{"transfer"}, BasedOnScan: uuid.NewString(), Context: events.CleanupContext{EngineID: 9}, Confirmation: events.CleanupConfirmation{Confirmed: true, RiskLevel: "low", ImpactDigest: strings.Repeat("a", 64)}, RequestedAt: now, Deadline: now.Add(time.Minute)}
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
			if mode == "result_rollback" {
				if err := tx.Exec("CREATE FUNCTION transfer.reject_cleanup_result() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture result failure'; END; $$; CREATE TRIGGER reject_cleanup_result BEFORE INSERT ON transfer.cleanup_results FOR EACH ROW EXECUTE FUNCTION transfer.reject_cleanup_result()").Error; err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req client.CleanupAccessRequest
				if r.URL.Path != "/api/v1/system/execution-authorizations/1/cleanup-accesses" || json.NewDecoder(r.Body).Decode(&req) != nil || req.ExecutionID != child.ExecutionID || req.Scope.Module != "transfer" || r.Header.Get("Authorization") != "Bearer fixture" {
					w.WriteHeader(403)
					return
				}
				if mode == "effect_denied" && req.Stage == "effect" {
					w.WriteHeader(403)
					return
				}
				_ = json.NewEncoder(w).Encode(client.CleanupAccess{AuthorizationID: "1", ExecutionID: child.ExecutionID, TenantID: "7", Stage: req.Stage, Attempt: req.Attempt, Scope: req.Scope, ExpiresAt: time.Now().Add(time.Minute)})
			}))
			defer server.Close()
			svc := NewTransferCleanupService(tx, client.NewSystemServiceClient(server.URL, cleanupRuntimeToken{}, server.Client()), transferCleanupTestConfig())
			// A rolled-back PG fixture uses one sql.Tx connection. Run the owner
			// synchronously; polling that same transaction from another goroutine would
			// test concurrent driver misuse, not the production multi-connection runner.
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

			var stored, otherStored models.TransferTask
			if err := tx.First(&stored, task.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.First(&otherStored, other.ID).Error; err != nil {
				t.Fatal(err)
			}
			var results int64
			if err := tx.Table("transfer.cleanup_results").Count(&results).Error; err != nil {
				t.Fatal(err)
			}
			receipt, err := execution.ReadCleanupReceipt(child)
			if err != nil {
				t.Fatal(err)
			}
			var unrelatedStored models.TransferTask
			if err := tx.First(&unrelatedStored, unrelated.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !unrelatedStored.Enabled {
				t.Fatal("cross-Engine cleanup")
			}
			if !otherStored.Enabled {
				t.Fatal("other Tenant changed")
			}
			if mode == "complete" {
				if stored.Enabled || results != 1 || receipt == nil || child.Status != "success" || receipt.Summary.DisabledTaskDefinitions != 1 {
					t.Fatal("domain/result/receipt not atomic", stored.Enabled, results, child.Status)
				}
			} else if !stored.Enabled || results != 0 || receipt != nil || child.Status != "failed" {
				t.Fatal("rejected transaction retained effects or result")
			}
		})
	}
}
