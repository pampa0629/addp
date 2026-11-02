package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/cleanup"
	"github.com/addp/common/client"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/schema"
	"github.com/addp/manager/internal/config"
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
			var lastLease execution.Lease
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
				if r.URL.Path == "/api/v1/system/runtime/engine-descriptors/9" && r.Method == "GET" {
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
					lastLease = run.Lease
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
			if child.Status == execution.ExecutionStatusFailed {
				observed := execution.Observe(&child)
				if observed.CurrentStep == nil || len(observed.Steps) != 1 || observed.Steps[0].ID != *observed.CurrentStep || observed.Steps[0].Status != "failed" || observed.Steps[0].Phase != "terminal" {
					t.Fatalf("missing safe Cleanup failure stage: mode=%s", mode)
				}
				wantStage := map[string]string{"effect_denied": "manager_cleanup_finalize", "result_rollback": "manager_cleanup_finalize", "index_result_rollback": "manager_cleanup_finalize", "changed_target": "manager_cleanup_confirmation", "index_unknown": "manager_cleanup_index_effect"}[mode]
				if wantStage == "" || *observed.CurrentStep != wantStage {
					t.Fatalf("incorrect failure stage mode=%s stage=%s", mode, *observed.CurrentStep)
				}
				var beforeEvents, afterEvents int64
				if err := tx.Model(&execution.Event{}).Where("execution_id=?", child.ExecutionID).Count(&beforeEvents).Error; err != nil {
					t.Fatal(err)
				}
				before, _ := json.Marshal(observed)
				if err := svc.recordCleanupFailureStage(t.Context(), lastLease, "manager_cleanup_plan"); err == nil {
					t.Fatal("terminal lease appended diagnostic")
				}
				var after execution.TaskExecution
				if err := tx.Where("execution_id=?", child.ExecutionID).First(&after).Error; err != nil {
					t.Fatal(err)
				}
				afterJSON, _ := json.Marshal(execution.Observe(&after))
				if err := tx.Model(&execution.Event{}).Where("execution_id=?", child.ExecutionID).Count(&afterEvents).Error; err != nil {
					t.Fatal(err)
				}
				if string(before) != string(afterJSON) || beforeEvents != afterEvents {
					t.Fatal("stale diagnostic changed terminal evidence")
				}
				encoded, _ := json.Marshal(observed)
				for _, raw := range []string{"fixture result failure", "SELECT", "Bearer fixture"} {
					if strings.Contains(string(encoded), raw) {
						t.Fatal("raw failure leaked through Cleanup stage")
					}
				}
			}
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

// Exercise production schema/repository composition and the gate-owned native
// Meilisearch. The existing runtime fixture intentionally stubs these boundaries.
func TestIntegrationPostgresManagerCleanupProductionRepositories(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("standard Manager PG gate required")
	}
	endpoint, key := os.Getenv("MANAGER_MEILISEARCH_TEST_URL"), os.Getenv("MANAGER_MEILISEARCH_TEST_KEY")
	if endpoint == "" || key == "" {
		t.Fatal("gate-owned Meilisearch required")
	}
	endpoint = nativeContentIndexProxy(t, endpoint)
	target, err := url.Parse(os.Getenv("MANAGER_POSTGRES_TEST_DSN"))
	if err != nil || target.User == nil {
		t.Fatal("test DSN required")
	}
	cfg := &config.Config{DBSchema: "manager", MeilisearchURL: endpoint, MeilisearchMasterKey: key, MeilisearchManagerContentIndex: "cleanup-production-" + uuid.NewString()}
	cfg.DBHost, cfg.DBPort, cfg.DBName = target.Hostname(), target.Port(), strings.TrimPrefix(target.Path, "/")
	cfg.DBUser = target.User.Username()
	cfg.DBPassword, _ = target.User.Password()
	if cfg.DBName != "addp_test" && !strings.Contains(cfg.DBName, "disposable") {
		t.Fatal("disposable database required")
	}
	// Production startup has already run System's shared schema migration.
	bootstrap, err := gorm.Open(postgres.Open(os.Getenv("MANAGER_POSTGRES_TEST_DSN")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	bootstrapPool, _ := bootstrap.DB()
	if err := schema.InitializeCommon(bootstrap); err != nil {
		_ = bootstrapPool.Close()
		t.Fatal(err)
	}
	_ = bootstrapPool.Close()
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		t.Fatalf("production repository initialization: %v", err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	store, err := projectionstore.Migrate(tx, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	deliveries := repository.NewContentIndexDeliveryRepository(tx, cfg.MeilisearchManagerContentIndex)
	search, err := NewHybridSearchService(cfg, repository.NewEmbeddingRepository(tx), nil, nil, nil, deliveries, store)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		task, e := search.client.DeleteIndexWithContext(cleanupCtx, cfg.MeilisearchManagerContentIndex)
		if e != nil {
			t.Error("owned index cleanup", e)
			return
		}
		final, e := search.client.WaitForTaskWithContext(cleanupCtx, task.TaskUID, 20*time.Millisecond)
		if e != nil || final.Status != meilisearch.TaskStatusSucceeded {
			t.Error("owned index cleanup incomplete", e)
		}
	}()
	if !search.initialized.Load() {
		t.Fatal("real content index initialization incomplete")
	}
	svc := &CleanupService{db: tx, search: search,
		systemClient:     client.NewSystemServiceClient("http://127.0.0.1:1", cleanupIndexToken{}, nil),
		previewStateRepo: repository.NewPreviewStateRepository(tx), embeddingRepo: repository.NewEmbeddingRepository(tx),
		tileCacheSvc:        &TileCacheTaskService{tileCacheRepo: repository.NewTileCacheRepository(tx)},
		optimizationSvc:     &VectorMaterializedViewTaskService{repo: repository.NewVectorMaterializedViewRepository(tx)},
		taskDefinitionRepo:  repository.NewCleanupTaskDefinitionRepository(tx, repository.ManagerTaskDefinitionSpecs()),
		managedArtifactRepo: repository.NewCleanupManagedArtifactRepository(tx, repository.ManagerManagedArtifactSpecs())}
	tenant := uint(942000007)
	snapshot, err := svc.freezeCleanupPlan(ctx, tenant, map[string]interface{}{"engine_id": uint(9)})
	if err != nil {
		t.Fatalf("production cleanup plan: %v", err)
	}
	input := events.CleanupRequest{RequestID: uuid.NewString(), TenantID: tenant, RequestedBy: 7, Action: events.CleanupActionScan, Context: events.CleanupContext{EngineID: 9}}
	plan, inventory, err := svc.assessCleanupIndex(ctx, input)
	if err != nil {
		t.Fatalf("native cleanup inventory: %v", err)
	}
	if plan.CandidateCount != 0 {
		t.Fatal("empty Tenant acquired foreign candidates")
	}
	if err := svc.verifyCleanupPlain(ctx, tx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := deliveries.VerifyInventoryGuard(ctx, tx, inventory.Guard); err != nil {
		t.Fatal(err)
	}
	digest, err := snapshot.digest()
	if err != nil {
		t.Fatal(err)
	}
	plan.DomainDigest = digest
	if err := tx.Create(plan).Error; err != nil {
		t.Fatal(err)
	}
	// Continue through real Common claim, Manager effect/result/receipt commit;
	// only System's authority response is controlled in this Owner-level gate.
	authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request client.CleanupAccessRequest
		if r.Method != "POST" || r.URL.Path != "/api/v1/system/execution-authorizations/1/cleanup-accesses" || json.NewDecoder(r.Body).Decode(&request) != nil || request.Scope.Module != "manager" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(client.CleanupAccess{AuthorizationID: "1", ExecutionID: request.ExecutionID,
			TenantID: strconv.FormatUint(uint64(tenant), 10), Stage: request.Stage, Attempt: request.Attempt,
			Scope: request.Scope, ExpiresAt: time.Now().Add(time.Minute)})
	}))
	defer authority.Close()
	svc.systemClient = client.NewSystemServiceClient(authority.URL, cleanupIndexToken{}, authority.Client())
	actorCtx := execution.WithUserOrigin(ctx, authtest.NewTenantUserAuthContext(strconv.FormatUint(uint64(tenant), 10), "7", []string{"system.engine.delete"}))
	parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(tenant), Module: "system", TaskType: "cleanup", Source: "system", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", MaxAttempts: 1, ExecutionConfig: commonmodels.JSONMap{execution.DisplayLanguageKey: "en"}}
	if err := execution.ApplyUserOrigin(actorCtx, &parent); err != nil {
		t.Fatal(err)
	}
	now, err := execution.DatabaseNow(tx)
	if err != nil {
		t.Fatal(err)
	}
	request := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parent.ExecutionID, TenantID: tenant, RequestedBy: 7,
		Action: events.CleanupActionScan, TriggerType: "manual", CauseEvent: events.CleanupCauseEngineDeleting,
		ExpectedModules: []string{"manager"}, Context: events.CleanupContext{EngineID: 9}, RequestedAt: now, Deadline: now.Add(30 * time.Second)}
	if err := request.Seal(); err != nil {
		t.Fatal(err)
	}
	children, err := execution.CreateCleanupExecutions(actorCtx, tx, &parent, request)
	if err != nil {
		t.Fatal(err)
	}
	child := children[0]
	if err := execution.AttachPendingAuthorization(actorCtx, tx, execution.PendingIdentity{ExecutionID: child.ExecutionID, TenantID: int(tenant), Module: "manager", TaskType: "cleanup_executor", Source: "system"}, execution.PendingAuthorization{ID: 1, ExpiresAt: now.Add(time.Hour), ActorPrincipalID: *child.ActorPrincipalID, ActorTenantMembershipID: *child.ActorTenantMembershipID, IssuedAuthorizationVersion: *child.IssuedAuthorizationVersion}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execution.ClaimPending(actorCtx, tx, execution.PendingIdentity{ExecutionID: parent.ExecutionID, TenantID: int(tenant), Module: "system", TaskType: "cleanup", Source: "system"}, execution.ClaimOptions{Module: "system", TaskType: "cleanup", Source: "system", WorkerID: "production-parent", LeaseDuration: time.Minute}, nil); err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithTimeout(actorCtx, 10*time.Second)
	defer stop()
	var ownerErr error
	runner := cleanup.Runner{DB: tx, Owner: "manager", System: svc.systemClient, Handle: func(ctx context.Context, run *cleanup.Run) error {
		defer stop()
		ownerErr = svc.runCleanupExecution(ctx, run)
		return ownerErr
	}}
	if err := runner.Serve(runCtx); err != nil {
		t.Fatal(err)
	}
	if ownerErr != nil {
		t.Fatalf("production Manager handler: %v", ownerErr)
	}
	if err := tx.Where("execution_id=?", child.ExecutionID).First(&child).Error; err != nil {
		t.Fatal(err)
	}
	if child.Status != "success" || child.Attempt != 1 || child.StartedAt == nil {
		t.Fatalf("production child outcome: %s", child.Status)
	}
	if _, err := execution.ReadCleanupReceipt(child); err != nil {
		t.Fatal(err)
	}
}
