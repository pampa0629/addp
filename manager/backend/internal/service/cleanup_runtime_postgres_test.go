package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/cleanup"
	"github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	"github.com/meilisearch/meilisearch-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// This test uses real PG and controlled authority/index HTTP responses. Native
// Meilisearch delivery receipts are tested by the separate content snapshot gate.
func TestIntegrationPostgresManagerCleanupRuntimeAtomicity(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("standard Manager PG gate required")
	}
	db, err := gorm.Open(postgres.Open(os.Getenv("MANAGER_POSTGRES_TEST_DSN")), &gorm.Config{})
	if err != nil {
		t.Fatal("open PG failed")
	}
	pool, _ := db.DB()
	defer pool.Close()
	for _, mode := range []string{"complete", "effect_denied", "result_rollback", "changed_target", "index_complete", "index_result_rollback", "index_unknown"} {
		t.Run(mode, func(t *testing.T) {
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			if err := tx.Exec("DROP SCHEMA IF EXISTS manager CASCADE; CREATE SCHEMA manager; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
				t.Fatal(err)
			}
			if err := execution.EnsureStore(tx); err != nil {
				t.Fatal(err)
			}
			if err := tx.AutoMigrate(&models.PreviewState{}); err != nil {
				t.Fatal(err)
			}
			if err := repository.MigrateContentIndexDeliverySchema(tx); err != nil {
				t.Fatal(err)
			}
			if err := repository.MigrateCleanupArtifactSchema(tx); err != nil {
				t.Fatal(err)
			}
			if err := cleanup.MigrateResults(tx, "manager"); err != nil {
				t.Fatal(err)
			}
			owned := models.PreviewState{TenantID: 7, ItemFingerprint: "owned", Locator: "addp://engine/9/path/owned?type=table", ViewState: commonmodels.JSONMap{}}
			other := models.PreviewState{TenantID: 8, ItemFingerprint: "other", Locator: "addp://engine/9/path/other?type=table", ViewState: commonmodels.JSONMap{}}
			if err := tx.Create(&owned).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Create(&other).Error; err != nil {
				t.Fatal(err)
			}
			token, err := repository.ObserveCleanupTarget(t.Context(), tx, repository.CleanupTargetKey{TenantID: 7, Kind: repository.CleanupTargetPreviewState, Identity: "owned"})
			if err != nil {
				t.Fatal(err)
			}
			executing := false
			indexScenario := strings.HasPrefix(mode, "index_")
			deleteCalls := 0
			correlation := ""
			providerTime := "2026-10-29T00:00:00Z"
			authID := int64(0)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/indexes/content/documents/fetch" && r.Method == "POST" {
					docs := []any{}
					if indexScenario && deleteCalls == 0 {
						docs = append(docs, map[string]any{"id": "orphan", "document_id": "orphan", "tenant_id": 7, "engine_id": 9})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"results": docs, "total": len(docs), "offset": 0, "limit": 256})
					return
				}
				if r.URL.Path == "/api/v1/system/engines/9" && r.Method == "GET" {
					w.WriteHeader(404)
					return
				}
				if r.URL.Path == "/indexes/content/documents/delete" && r.Method == "POST" {
					deleteCalls++
					correlation = r.URL.Query().Get("customMetadata")
					var body struct {
						Filter string `json:"filter"`
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil || body.Filter != "tenant_id = 7 AND engine_id = 9" || correlation == "" {
						t.Error("external delete escaped exact registered scope")
					}
					if mode == "index_unknown" {
						w.WriteHeader(503)
						return
					}
					w.WriteHeader(http.StatusAccepted)
					fmt.Fprintf(w, `{"taskUid":1,"indexUid":"content","type":"documentDeletion","enqueuedAt":%q}`, providerTime)
					return
				}
				if r.URL.Path == "/tasks/1" && r.Method == "GET" {
					fmt.Fprintf(w, `{"uid":1,"indexUid":"content","type":"documentDeletion","status":"succeeded","enqueuedAt":%q,"customMetadata":%q}`, providerTime, correlation)
					return
				}
				var req client.CleanupAccessRequest
				if r.URL.Path != fmt.Sprintf("/api/v1/system/execution-authorizations/%d/cleanup-accesses", authID) || json.NewDecoder(r.Body).Decode(&req) != nil || req.Scope.Module != "manager" {
					w.WriteHeader(403)
					return
				}
				if executing && mode == "effect_denied" && req.Stage == "effect" {
					w.WriteHeader(403)
					return
				}
				_ = json.NewEncoder(w).Encode(client.CleanupAccess{AuthorizationID: strconv.FormatInt(authID, 10), ExecutionID: req.ExecutionID, TenantID: "7", Stage: req.Stage, Attempt: req.Attempt, Scope: req.Scope, ExpiresAt: time.Now().Add(time.Minute)})
			}))
			defer server.Close()
			deliveries := repository.NewContentIndexDeliveryRepository(tx, "content")
			endpoint := contentIndexEndpointID(server.URL)
			epoch, err := deliveries.Open(t.Context(), endpoint, true)
			if err != nil {
				t.Fatal(err)
			}
			if err := deliveries.ActivateIfSettled(t.Context(), epoch); err != nil {
				t.Fatal(err)
			}
			search := &HybridSearchService{client: meilisearch.New(server.URL, meilisearch.WithCustomClient(server.Client()), meilisearch.DisableRetries()), contentIndex: "content", enabled: true, deliveries: deliveries, epoch: epoch, endpointID: endpoint, log: slog.Default()}
			search.initialized.Store(true)
			system := client.NewSystemServiceClient(server.URL, cleanupIndexToken{}, server.Client())
			svc := &CleanupService{db: tx, search: search, systemClient: system, previewStateRepo: repository.NewPreviewStateRepository(tx)}
			runOwner := func(action, basedOn string) (events.CleanupRequest, execution.TaskExecution) {
				t.Helper()
				authID++
				ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "7", []string{"system.cleanup.execute"}))
				parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Module: "system", TaskType: "cleanup", Source: "system", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", ExecutionConfig: commonmodels.JSONMap{execution.DisplayLanguageKey: "en"}}
				if err := execution.ApplyUserOrigin(ctx, &parent); err != nil {
					t.Fatal(err)
				}
				now := time.Now().UTC()
				input := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parent.ExecutionID, TenantID: 7, RequestedBy: 7, Action: action, TriggerType: "manual", ExpectedModules: []string{"manager"}, Context: events.CleanupContext{EngineID: 9}, RequestedAt: now, Deadline: now.Add(time.Minute)}
				if action == events.CleanupActionExecute {
					input.CleanupMode = events.CleanupModeLogical
					input.BasedOnScan = basedOn
					input.Confirmation = events.CleanupConfirmation{Confirmed: true, RiskLevel: "low", ImpactDigest: strings.Repeat("a", 64)}
				}
				if err := input.Seal(); err != nil {
					t.Fatal(err)
				}
				children, err := execution.CreateCleanupExecutions(ctx, tx, &parent, input)
				if err != nil {
					t.Fatal(err)
				}
				child := children[0]
				if err := execution.AttachPendingAuthorization(ctx, tx, execution.PendingIdentity{ExecutionID: child.ExecutionID, TenantID: 7, Module: "manager", TaskType: "cleanup_executor", Source: "system"}, execution.PendingAuthorization{ID: authID, ExpiresAt: now.Add(time.Hour), ActorPrincipalID: *child.ActorPrincipalID, ActorTenantMembershipID: *child.ActorTenantMembershipID, IssuedAuthorizationVersion: *child.IssuedAuthorizationVersion}); err != nil {
					t.Fatal(err)
				}
				if _, _, err := execution.ClaimPending(ctx, tx, execution.PendingIdentity{ExecutionID: parent.ExecutionID, TenantID: 7, Module: "system", TaskType: "cleanup", Source: "system"}, execution.ClaimOptions{Module: "system", TaskType: "cleanup", Source: "system", WorkerID: "parent", LeaseDuration: time.Minute}, nil); err != nil {
					t.Fatal(err)
				}
				runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				runner := cleanup.Runner{DB: tx, Owner: "manager", System: system, Handle: func(ctx context.Context, run *cleanup.Run) error {
					defer cancel()
					return svc.runCleanupExecution(ctx, run)
				}}
				if err := runner.Serve(runCtx); err != nil {
					t.Fatal(err)
				}
				if err := tx.Where("execution_id=?", child.ExecutionID).First(&child).Error; err != nil {
					t.Fatal(err)
				}
				return input, child
			}
			scan, scanChild := runOwner(events.CleanupActionScan, "")
			if scanChild.Status != "success" {
				t.Fatal("scan failed", scanChild.Status, scanChild.ErrorDetails)
			}
			var scanTargets int64
			if err := tx.Model(&models.CleanupTarget{}).Count(&scanTargets).Error; err != nil || scanTargets != 0 {
				t.Fatalf("scan mutated target generations: %d %v", scanTargets, err)
			}
			if mode == "result_rollback" || mode == "index_result_rollback" {
				if err := tx.Exec("CREATE FUNCTION manager.reject_cleanup_result() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture result failure'; END; $$; CREATE TRIGGER reject_cleanup_result BEFORE INSERT ON manager.cleanup_results FOR EACH ROW EXECUTE FUNCTION manager.reject_cleanup_result()").Error; err != nil {
					t.Fatal(err)
				}
			}
			if mode == "changed_target" {
				if err := tx.Model(&owned).Update("preferred_mode", models.PreviewModeMapQuickView).Error; err != nil {
					t.Fatal(err)
				}
			}
			executing = true
			_, child := runOwner(events.CleanupActionExecute, scan.RequestID)
			var ownedCount, otherCount, results int64
			if err := tx.Model(&models.PreviewState{}).Where("id=? AND tenant_id=7", owned.ID).Count(&ownedCount).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Model(&models.PreviewState{}).Where("id=? AND tenant_id=8", other.ID).Count(&otherCount).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Table("manager.cleanup_results").Count(&results).Error; err != nil {
				t.Fatal(err)
			}
			receipt, err := execution.ReadCleanupReceipt(child)
			if err != nil {
				t.Fatal(err)
			}
			if otherCount != 1 {
				t.Fatal("cross-Tenant mutation")
			}
			staleErr := tx.Transaction(func(probe *gorm.DB) error { return repository.GuardCleanupTargetWrite(t.Context(), probe, token) })
			if mode == "complete" || mode == "index_complete" {
				if ownedCount != 0 || results != 2 || receipt == nil || child.Status != "success" || staleErr == nil {
					t.Fatalf("non-atomic completion: owned=%d results=%d status=%s stale=%v", ownedCount, results, child.Status, staleErr)
				}
			} else if ownedCount != 1 || results != 1 || receipt != nil || child.Status != "failed" || staleErr != nil {
				t.Fatalf("effects escaped rejection: owned=%d results=%d status=%s stale=%v", ownedCount, results, child.Status, staleErr)
			}
			if indexScenario {
				if deleteCalls != 1 {
					t.Fatalf("delete dispatch count=%d", deleteCalls)
				}
				var op models.CleanupIndexOperation
				if err := tx.First(&op).Error; err != nil {
					t.Fatal(err)
				}
				var delivery models.ContentIndexDelivery
				if err := tx.First(&delivery).Error; err != nil {
					t.Fatal(err)
				}
				wantStatus := repository.IndexDeliverySucceeded
				if mode == "index_unknown" {
					wantStatus = repository.IndexDeliveryUnknown
				}
				if delivery.Status != wantStatus {
					t.Fatalf("external fact lost: %s", delivery.Status)
				}
				if mode == "index_complete" {
					if op.State != "completed" {
						t.Fatal("index finalization missing")
					}
					if err := deliveries.ActivateIfSettled(t.Context(), epoch); err != nil {
						t.Fatal(err)
					}
				} else {
					if op.State != "prepared" {
						t.Fatal("result rollback released index operation")
					}
					if !errors.Is(deliveries.ActivateIfSettled(t.Context(), epoch), repository.ErrContentIndexIsolated) {
						t.Fatal("unsettled effect reopened index")
					}
					if err := search.reconcileContentDeliveries(t.Context()); err != nil && !errors.Is(err, repository.ErrContentIndexIsolated) {
						t.Fatal(err)
					}
					if deleteCalls != 1 {
						t.Fatal("recovery replayed Cleanup delete")
					}
				}
				late := &models.ContentIndexDelivery{TenantID: 7, EngineIDs: []uint{9}, DocumentIDs: []string{"late"}, Kind: repository.IndexDeliveryWrite}
				if err := tx.Transaction(func(probe *gorm.DB) error { return deliveries.Register(t.Context(), probe, epoch, late) }); !errors.Is(err, repository.ErrContentIndexIsolated) {
					t.Fatal("late target write accepted", err)
				}
			}
		})
	}
}
