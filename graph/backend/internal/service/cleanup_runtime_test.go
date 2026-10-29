package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/cleanup"
	"github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/graph/internal/models"
	"github.com/google/uuid"
)

type cleanupRuntimeToken struct{}

func (cleanupRuntimeToken) Token(context.Context, uint) (string, error) { return "fixture", nil }
func (cleanupRuntimeToken) PlatformToken(context.Context) (string, error) {
	return "", errors.New("platform prohibited")
}

func TestGraphCleanupUsesAdmissionCancellationAndAtomicFinalTransaction(t *testing.T) {
	testGraphCleanupRuntime(t, false)
}
func testGraphCleanupRuntime(t *testing.T, pg bool) {
	for _, mode := range []string{"complete", "parent_closed", "unknown_effect", "result_rollback"} {
		t.Run(mode, func(t *testing.T) {
			db, task, build := cleanupRuntimeDomainFixture(t, pg)
			tenant := int(task.TenantID)
			if err := cleanup.MigrateResults(db, "graph"); err != nil {
				t.Fatal(err)
			}
			ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext(strconv.Itoa(tenant), "7", []string{"system.cleanup.execute"}))
			parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: tenant, Module: "system", TaskType: "cleanup", Source: "system", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", ExecutionConfig: commonmodels.JSONMap{execution.DisplayLanguageKey: "en"}}
			if err := execution.ApplyUserOrigin(ctx, &parent); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			input := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parent.ExecutionID, TenantID: uint(tenant), RequestedBy: 7, Action: "execute", CleanupMode: events.CleanupModeLogical, TriggerType: "manual", ExpectedModules: []string{"graph"}, BasedOnScan: uuid.NewString(), Context: events.CleanupContext{EngineID: 1}, Confirmation: events.CleanupConfirmation{Confirmed: true, RiskLevel: "low", ImpactDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, RequestedAt: now, Deadline: now.Add(time.Minute)}
			if err := input.Seal(); err != nil {
				t.Fatal(err)
			}
			children, err := execution.CreateCleanupExecutions(ctx, db, &parent, input)
			if err != nil {
				t.Fatal(err)
			}
			child := children[0]
			if err := execution.AttachPendingAuthorization(ctx, db, execution.PendingIdentity{ExecutionID: child.ExecutionID, TenantID: tenant, Module: "graph", TaskType: "cleanup_executor", Source: "system"}, execution.PendingAuthorization{ID: 1, ExpiresAt: now.Add(time.Hour), ActorPrincipalID: *child.ActorPrincipalID, ActorTenantMembershipID: *child.ActorTenantMembershipID, IssuedAuthorizationVersion: *child.IssuedAuthorizationVersion}); err != nil {
				t.Fatal(err)
			}
			_, lease, err := execution.ClaimPending(ctx, db, execution.PendingIdentity{ExecutionID: parent.ExecutionID, TenantID: tenant, Module: "system", TaskType: "cleanup", Source: "system"}, execution.ClaimOptions{Module: "system", TaskType: "cleanup", Source: "system", WorkerID: "parent", LeaseDuration: time.Minute}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "parent_closed" {
				if err := execution.CompleteWithLease(ctx, db, *lease, "timeout", nil); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "unknown_effect" {
				if err := db.Create(&models.GraphOperation{ID: uuid.NewString(), RunnerID: "old-review", TenantID: uint(tenant), GraphID: task.GraphID, Kind: "review", State: models.GraphOperationUncertain}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if mode == "result_rollback" {
				if pg {
					name := fmt.Sprintf("cleanup_result_probe_%d", tenant)
					if err := db.Exec("CREATE FUNCTION graph." + name + "() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.tenant_id = " + strconv.Itoa(tenant) + " THEN RAISE EXCEPTION 'fixture result failure'; END IF; RETURN NEW; END; $$; CREATE TRIGGER " + name + " BEFORE INSERT ON graph.cleanup_results FOR EACH ROW EXECUTE FUNCTION graph." + name + "()").Error; err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						if err := db.Exec("DROP TRIGGER IF EXISTS " + name + " ON graph.cleanup_results; DROP FUNCTION IF EXISTS graph." + name + "()").Error; err != nil {
							t.Error(err)
						}
					})
				} else if err := db.Exec("CREATE TRIGGER graph.reject_result BEFORE INSERT ON cleanup_results BEGIN SELECT RAISE(ABORT,'fixture result failure'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request client.CleanupAccessRequest
				if json.NewDecoder(r.Body).Decode(&request) != nil || request.ExecutionID != child.ExecutionID || r.Header.Get("Authorization") != "Bearer fixture" {
					w.WriteHeader(403)
					return
				}
				_ = json.NewEncoder(w).Encode(client.CleanupAccess{AuthorizationID: "1", ExecutionID: child.ExecutionID, TenantID: strconv.Itoa(tenant), Stage: request.Stage, Attempt: request.Attempt, Scope: request.Scope, ExpiresAt: time.Now().Add(time.Minute)})
			}))
			defer server.Close()
			svc := NewCleanupService(db, client.NewSystemServiceClient(server.URL, cleanupRuntimeToken{}, nil))
			if err := svc.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			defer svc.Stop()
			if pg {
				peer := NewCleanupService(db, client.NewSystemServiceClient(server.URL, cleanupRuntimeToken{}, nil))
				if err := peer.Start(t.Context()); err != nil {
					t.Fatal(err)
				}
				defer peer.Stop()
			}
			deadline := time.Now().Add(4 * time.Second)
			for {
				var got execution.TaskExecution
				if err := db.Where("execution_id = ?", child.ExecutionID).First(&got).Error; err != nil {
					t.Fatal(err)
				}
				child = got
				if child.Status == "success" || child.Status == "failed" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("Cleanup did not converge", child.Status)
				}
				time.Sleep(10 * time.Millisecond)
			}
			var kg models.KnowledgeGraph
			if err := db.First(&kg, task.GraphID).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Where("execution_id = ?", build.ExecutionID).First(build).Error; err != nil {
				t.Fatal(err)
			}
			var rows int64
			if err := db.Table("graph.cleanup_results").Where("tenant_id = ?", tenant).Count(&rows).Error; err != nil {
				t.Fatal(err)
			}
			receipt, err := execution.ReadCleanupReceipt(child)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "parent_closed":
				if kg.AdmissionClosed || build.Status != "pending" || rows != 0 || child.Attempt != 0 || child.StartedAt != nil || receipt != nil {
					t.Fatal("rejected C touched Graph")
				}
			case "complete":
				if !kg.AdmissionClosed || kg.Status != "archived" || build.Status != "cancelled" || build.StartedAt != nil || build.Attempt != 0 || rows != 1 || receipt == nil || child.Status != "success" || child.Attempt != 1 {
					t.Fatal("three-phase completion incomplete")
				}
			case "unknown_effect":
				if !kg.AdmissionClosed || kg.Status == "archived" || child.Status != "failed" || rows != 1 || receipt == nil || receipt.Reason != "external_effect_uncertain" {
					t.Fatal("unknown effect was erased or ignored")
				}
				var ops int64
				if err := db.Model(&models.GraphOperation{}).Where("graph_id = ? AND state = ?", kg.ID, models.GraphOperationUncertain).Count(&ops).Error; err != nil || ops != 1 {
					t.Fatal("operation evidence deleted")
				}
			case "result_rollback":
				if !kg.AdmissionClosed || kg.Status == "archived" || rows != 0 || receipt != nil || child.Status != "failed" {
					t.Fatal("result failure left final domain mutations")
				}
			}
		})
	}
}
