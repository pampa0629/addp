package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/google/uuid"
)

func testGraphScanBaselineRuntime(t *testing.T) {
	for _, window := range []string{"before_validation", "unchanged", "after_validation", "removed_after_validation", "swapped_after_validation", "missing_scan"} {
		t.Run(window, func(t *testing.T) {
			f := newReviewPGFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
			defer cancel()
			ctx = execution.WithUserOrigin(ctx, authtest.NewTenantUserAuthContext(strconv.Itoa(int(f.task.TenantID)), "7", []string{"system.cleanup.execute", "system.engine.delete"}))
			var authorizationID int64
			submit := func(action, scanID, digest string) (events.CleanupRequest, execution.TaskExecution) {
				t.Helper()
				authorizationID++
				now, err := execution.DatabaseNow(f.db)
				if err != nil {
					t.Fatal(err)
				}
				parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(f.task.TenantID), Module: "system", TaskType: "cleanup", Source: "system", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", ExecutionConfig: commonmodels.JSONMap{execution.DisplayLanguageKey: "en"}}
				if err := execution.ApplyUserOrigin(ctx, &parent); err != nil {
					t.Fatal(err)
				}
				input := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parent.ExecutionID, TenantID: f.task.TenantID, RequestedBy: 7, Action: action, TriggerType: "manual", CauseEvent: events.CleanupCauseEngineDeleting, ExpectedModules: []string{"graph"}, Context: events.CleanupContext{EngineID: f.graph.EngineID}, RequestedAt: now, Deadline: now.Add(time.Minute)}
				if action == "execute" {
					input.CleanupMode = events.CleanupModePhysical
					input.BasedOnScan = scanID
					input.Context.ExternalArtifactPolicy = "delete"
					input.Confirmation = events.CleanupConfirmation{Confirmed: true, TextProvided: true, RiskLevel: "low", ImpactDigest: digest}
				}
				if err := input.Seal(); err != nil {
					t.Fatal(err)
				}
				children, err := execution.CreateCleanupExecutions(ctx, f.db, &parent, input)
				if err != nil {
					t.Fatal(err)
				}
				f.child = children[0]
				identity := execution.PendingIdentity{ExecutionID: f.child.ExecutionID, TenantID: f.child.TenantID, Module: "graph", TaskType: "cleanup_executor", Source: "system"}
				if err := execution.AttachPendingAuthorization(ctx, f.db, identity, execution.PendingAuthorization{ID: authorizationID, ExpiresAt: now.Add(time.Hour), ActorPrincipalID: *f.child.ActorPrincipalID, ActorTenantMembershipID: *f.child.ActorTenantMembershipID, IssuedAuthorizationVersion: *f.child.IssuedAuthorizationVersion}); err != nil {
					t.Fatal(err)
				}
				_, parentLease, err := execution.ClaimPending(ctx, f.db, execution.PendingIdentity{ExecutionID: parent.ExecutionID, TenantID: parent.TenantID, Module: "system", TaskType: "cleanup", Source: "system"}, execution.ClaimOptions{Module: "system", TaskType: "cleanup", Source: "system", WorkerID: "scan-baseline-parent", LeaseDuration: time.Minute}, nil)
				if err != nil {
					t.Fatal(err)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var request client.CleanupAccessRequest
					if json.NewDecoder(r.Body).Decode(&request) != nil || request.ExecutionID != f.child.ExecutionID || r.Header.Get("Authorization") != "Bearer fixture" {
						w.WriteHeader(403)
						return
					}
					_ = json.NewEncoder(w).Encode(client.CleanupAccess{AuthorizationID: strconv.FormatInt(authorizationID, 10), ExecutionID: f.child.ExecutionID, TenantID: strconv.Itoa(f.child.TenantID), Stage: request.Stage, Attempt: request.Attempt, Scope: request.Scope, ExpiresAt: time.Now().Add(time.Minute)})
				}))
				defer server.Close()
				f.svc = NewCleanupService(f.db, client.NewSystemServiceClient(server.URL, cleanupRuntimeToken{}, nil))
				f.start(t, ctx)
				defer f.svc.Stop()
				waitReviewCleanupPG(t, ctx, f.db, "SELECT status IN ('success','failed','timeout','cancelled') FROM common.task_executions WHERE execution_id = ?", f.child.ExecutionID)
				var child execution.TaskExecution
				if err := f.db.First(&child, "execution_id = ?", f.child.ExecutionID).Error; err != nil {
					t.Fatal(err)
				}
				if child.Status == "success" {
					if err := execution.CompleteWithLease(ctx, f.db, *parentLease, "success", nil); err != nil {
						t.Fatal(err)
					}
				}
				return input, child
			}
			_, preflight := submit("scan", "", "")
			first, err := execution.ReadCleanupReceipt(preflight)
			if err != nil || first == nil || first.Status != "success" {
				t.Fatal("preflight failed", err)
			}
			mutate := func() {
				task := models.BuildTask{TenantID: f.task.TenantID, GraphID: f.graph.ID, Name: "scan-drift-added", Status: models.BuildStatusPending, Stats: []byte(`{}`)}
				if err := repository.NewBuildRepository(f.db).CreateTask(&task); err != nil {
					t.Fatal("production CreateTask", err)
				}
			}
			if window == "before_validation" {
				mutate()
			}
			scan, validation := submit("scan", "", "")
			second, err := execution.ReadCleanupReceipt(validation)
			if err != nil || second == nil || second.Status != "success" {
				t.Fatal("validation failed", err)
			}
			if window == "before_validation" {
				if first.ImpactDigest == second.ImpactDigest {
					t.Fatal("validation failed to detect added task")
				}
				var kg models.KnowledgeGraph
				if err := f.db.First(&kg, f.graph.ID).Error; err != nil || kg.Status != f.graph.Status || kg.AdmissionClosed {
					t.Fatal("scan changed graph", err)
				}
				t.Log("actual second scan digest detects task added before validation; scan remains read-only")
				return
			}
			if first.ImpactDigest != second.ImpactDigest {
				t.Fatal("unchanged scans differed")
			}
			aggregate, err := events.BuildCleanupImpactData([]events.CleanupImpactItem{{StableRef: "graph:" + second.ImpactDigest, Disposition: events.CleanupImpactWillDelete}}, "")
			if err != nil {
				t.Fatal(err)
			}
			if window == "after_validation" {
				mutate()
			}
			if window == "removed_after_validation" || window == "swapped_after_validation" {
				var mat models.BuildMaterial
				if err := f.db.Where("task_id = ?", f.task.ID).First(&mat).Error; err != nil {
					t.Fatal(err)
				}
				repo := repository.NewBuildRepository(f.db)
				if err := repo.DeleteMaterial(ctx, mat.ID, mat.TaskID, mat.GraphID, mat.TenantID); err != nil {
					t.Fatal(err)
				}
				if window == "swapped_after_validation" {
					mat.ID = 0
					if err := repo.CreateMaterial(ctx, &mat); err != nil {
						t.Fatal(err)
					}
				}
			}
			if window == "missing_scan" {
				scan.RequestID = uuid.NewString()
			}
			before := readDeleteCleanupFacts(t, f.db, f.task.TenantID)
			_, child := submit("execute", scan.RequestID, aggregate.Digest)
			receipt, err := execution.ReadCleanupReceipt(child)
			if err != nil {
				t.Fatal(err)
			}
			var kg models.KnowledgeGraph
			if err := f.db.First(&kg, f.graph.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(readDeleteCleanupFacts(t, f.db, f.task.TenantID), before) {
				t.Fatal("execute changed retained domain/build facts")
			}
			if window == "unchanged" {
				if child.Status != "success" || receipt == nil || receipt.Status != "success" || kg.Status != "archived" || !kg.AdmissionClosed {
					t.Fatal("unchanged baseline did not complete")
				}
			} else {
				if child.Status != "failed" || !reflect.DeepEqual(kg, f.graph) {
					t.Fatal("drift/source failure changed original graph or accepted old confirmation")
				}
				if window == "missing_scan" {
					if receipt != nil {
						t.Fatal("missing scan invented a result receipt")
					}
				} else if receipt == nil || receipt.Reason != "scope_changed" || receipt.Summary.AffectedRecords != 0 {
					t.Fatal("drift failure lost safe classification or invented effects")
				}
			}

		})
	}
}

func testGraphScanBaselineFinal(t *testing.T) {
	for _, mode := range []string{"late_candidate", "empty_scope"} {
		t.Run(mode, func(t *testing.T) {
			f := newReviewPGFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
			defer cancel()
			if mode == "empty_scope" {
				if err := repository.NewKnowledgeGraphRepository(f.db).DeleteDefinition(ctx, f.graph.ID, f.task.TenantID, f.graph.Version); err != nil {
					t.Fatal(err)
				}
			}
			prepareReviewCleanupPGFixture(t, f, time.Minute)
			before := readDeleteCleanupFacts(t, f.db, f.task.TenantID)
			var closed models.KnowledgeGraph
			var release func()
			if mode == "late_candidate" {
				var barrier <-chan struct{}
				barrier, release = pauseCleanupAfterClosurePG(t, ctx, f)
				defer release()
				f.start(t, ctx)
				select {
				case <-barrier:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if err := f.db.First(&closed, f.graph.ID).Error; err != nil {
					t.Fatal(err)
				}
				task := models.BuildTask{TenantID: f.task.TenantID, GraphID: f.graph.ID, Name: "late-pg-fault", Status: models.BuildStatusPending, Stats: []byte(`{}`)}
				// The supported entry refuses a closed graph. Direct PG fault injection
				// then exercises the final guard independently of that earlier fence.
				if err := repository.NewBuildRepository(f.db).CreateTask(&task); err == nil {
					t.Fatal("closed graph admitted task")
				}
				if err := f.db.Create(&task).Error; err != nil {
					t.Fatal(err)
				}
				var persisted models.BuildTask
				if err := f.db.First(&persisted, task.ID).Error; err != nil {
					t.Fatal(err)
				}
				before.tasks = append(before.tasks, persisted)
				release()
			} else {
				f.start(t, ctx)
			}
			want := "success"
			if mode == "late_candidate" {
				want = "failed"
			}
			child := f.waitTerminal(t, ctx, want)
			receipt, err := execution.ReadCleanupReceipt(*child)
			if err != nil || receipt == nil || receipt.Status != want {
				t.Fatal("incorrect final receipt", err)
			}
			if mode == "late_candidate" {
				var after models.KnowledgeGraph
				if err := f.db.First(&after, f.graph.ID).Error; err != nil || !reflect.DeepEqual(closed, after) || receipt.Reason != "scope_changed" {
					t.Fatal("final mismatch rewrote closure or archived graph", err)
				}
			}
			if !reflect.DeepEqual(readDeleteCleanupFacts(t, f.db, f.task.TenantID), before) {
				t.Fatal("final guard changed retained facts")
			}
		})
	}
}
