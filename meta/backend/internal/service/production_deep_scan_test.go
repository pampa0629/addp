package service

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/production"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanresource"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func exerciseProductionDeepScan(t *testing.T, db *gorm.DB, concurrent bool) {
	for i, outcome := range []string{"success", "failed", "cancelled", "timeout"} {
		t.Run("deep dependency "+outcome, func(t *testing.T) {
			tenant, engineID := uint(7), uint(99700+i)
			ref := production.Reference{Owner: "transfer", ExecutionID: uuid.NewString()}
			ref.ResultID = ref.ExecutionID
			target := plugin.TabularItemPath(engineID, "schema", "Out.door", " active.table ")
			receipt := production.Receipt{Reference: ref, TenantID: 7, Attempt: 1, TargetEngineVersion: 3, OperationID: ref.ResultID, CommittedAt: time.Now().UTC(), Primary: target, Objects: []production.CommittedObject{{Target: target, Created: true}}, Sources: production.SourceEvidence{Kind: production.SourcesNone}, Origin: &production.UserOrigin{PrincipalID: 31, MembershipID: 51, AuthorizationVersion: 6}}
			registry, err := production.NewRegistry(production.Owner{Module: "transfer", ClientID: "addp-transfer", ReceiptPermission: "transfer.production_receipt.read", ExecutionPermission: "transfer.task.execute", ReadReceipt: func(ctx context.Context, tid uint, requested production.Reference) (*production.Receipt, error) {
				if tid != tenant || requested != ref {
					return nil, production.ErrEvidence
				}
				copy := receipt
				return &copy, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			caps, err := plugin.MarshalEngineCapabilities((&postgresql.PostgreSQLPlugin{}).Capabilities())
			if err != nil {
				t.Fatal(err)
			}
			capJSON := commonmodels.JSONString(caps)
			engines := NewEngineService(db, nil)
			engines.cacheEngine(tenant, &commonmodels.Engine{ID: engineID, TenantID: &tenant, Version: 3, LifecycleState: "active", EngineType: "postgresql", Capabilities: &capJSON})
			svc := NewScanExecutionService(db, nil, engines, nil).WithProductionRegistry(registry)
			// The producer is already terminal before either scan is requested.
			producer := &execution.TaskExecution{TenantID: 7, ExecutionID: ref.ExecutionID, Module: "transfer", TaskType: "sync", Source: "transfer", TriggerType: "scheduled", Status: "success", ExecutionBoundary: "bounded"}
			if err := db.Create(producer).Error; err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Transaction(func(tx *gorm.DB) error {
					var ids []string
					if err := tx.Model(&execution.TaskExecution{}).Where("tenant_id=? AND (execution_id=? OR parent_execution_id=?)", tenant, ref.ExecutionID, ref.ExecutionID).Pluck("execution_id", &ids).Error; err != nil {
						return err
					}
					if err := tx.Where("tenant_id=? AND execution_id IN ?", tenant, ids).Delete(&execution.Event{}).Error; err != nil {
						return err
					}
					if err := tx.Where("tenant_id=? AND execution_id IN ?", tenant, ids).Delete(&execution.TaskExecution{}).Error; err != nil {
						return err
					}
					for _, model := range []interface{}{&execution.TaskExecution{}, &execution.Event{}} {
						var remaining int64
						if err := tx.Model(model).Where("tenant_id=? AND execution_id IN ?", tenant, ids).Count(&remaining).Error; err != nil {
							return err
						}
						if remaining != 0 {
							t.Errorf("owned scan fixture cleanup left %d rows", remaining)
						}
					}
					return nil
				}); err != nil {
					t.Error(err)
				}
			})
			queued, err := svc.EnsureProductionDeepScan(t.Context(), tenant, ref)
			if err != nil {
				t.Fatal(err)
			}
			if queued.Validate() != nil || queued.Status != "pending" {
				t.Fatalf("deep not durably queued: %+v", queued)
			}
			// Reconstruct the service to prove no callback/in-memory state owns it.
			svc = NewScanExecutionService(db, nil, engines, nil).WithProductionRegistry(registry)
			replay, err := svc.EnsureProductionDeepScan(t.Context(), tenant, ref)
			if err != nil || !reflect.DeepEqual(replay, queued) {
				t.Fatalf("replay changed identity: %+v %v", replay, err)
			}
			if concurrent {
				var wg sync.WaitGroup
				for j := 0; j < 2; j++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						got, err := svc.EnsureProductionDeepScan(t.Context(), tenant, ref)
						if err != nil || got.ScanExecutionID != queued.ScanExecutionID {
							t.Errorf("concurrent request duplicated deep: %+v %v", got, err)
						}
					}()
				}
				wg.Wait()
			}
			basic, _, err := svc.ClaimNextBoundedExecution(t.Context(), "basic-worker", time.Now(), time.Minute)
			if err != nil || basic == nil || basic.ExecutionID != queued.BasicScanExecutionID {
				t.Fatalf("basic not first: %+v %v", basic, err)
			}
			blocked, _, err := svc.ClaimNextBoundedExecution(t.Context(), "other-worker", time.Now(), time.Minute)
			if err != nil || blocked != nil {
				t.Fatalf("deep claimed while basic running: %+v %v", blocked, err)
			}
			if outcome == "success" {
				seedAuthorizationFacts(t, db, "schema", "table", engineID)
			}
			if err := db.Model(&execution.TaskExecution{}).Where("execution_id=?", basic.ExecutionID).Update("status", outcome).Error; err != nil {
				t.Fatal(err)
			}
			deep, lease, err := svc.ClaimNextBoundedExecution(t.Context(), "deep-worker", time.Now(), time.Minute)
			if err != nil || deep == nil || deep.ExecutionID != queued.ScanExecutionID || lease == nil {
				t.Fatalf("dependent run not claimable: %+v %v", deep, err)
			}
			precise, err := productionScanRecord(db.Where("execution_id=?", deep.ExecutionID))
			if err != nil {
				t.Fatal(err)
			}
			frozen, err := frozenProductionReceipt(precise)
			if err != nil || !reflect.DeepEqual(frozen, &receipt) || precise.ParentExecutionID == nil || *precise.ParentExecutionID != ref.ExecutionID || precise.ActorPrincipalID == nil || *precise.ActorPrincipalID != 31 || precise.ExecutionAuthorizationID != nil {
				t.Fatalf("deep lost exact scope or origin: %v", err)
			}
			err = svc.checkProductionBasicDependency(t.Context(), db, precise, frozen)
			if (err == nil) != (outcome == "success") {
				t.Fatalf("dependency outcome=%s err=%v", outcome, err)
			}
			if outcome != "success" {
				// The real runner must record dependency failure before source IO.
				svc.BindBoundedLease(deep.ExecutionID, *lease)
				if err := svc.ExecuteScanRun(t.Context(), deep.ExecutionID); err == nil {
					t.Fatal("failed basic dispatched deep")
				}
				stored, err := svc.taskExecutionRepo.GetByExecutionID(t.Context(), deep.ExecutionID, 7)
				if err != nil || stored.Status != "failed" {
					t.Fatalf("dependent failure not persisted: %+v %v", stored, err)
				}
				if execution.FailureCategory(stored.ErrorDetails) == "permission_denied" {
					t.Fatal("failed basic identification was mislabeled as an authorization denial")
				}
			}
			// No extra scans and no producer status changes, even after a replay.
			after, err := svc.EnsureProductionDeepScan(t.Context(), tenant, ref)
			if err != nil || after.ScanExecutionID != queued.ScanExecutionID || after.BasicScanExecutionID != queued.BasicScanExecutionID {
				t.Fatalf("replay revived dependency: %+v %v", after, err)
			}
			var count int64
			if err := db.Model(&execution.TaskExecution{}).Where("tenant_id=7 AND module='meta' AND parent_execution_id=?", ref.ExecutionID).Count(&count).Error; err != nil || count != 2 {
				t.Fatalf("scan count=%d err=%v", count, err)
			}
			if err := db.First(producer, producer.ID).Error; err != nil || producer.Status != "success" {
				t.Fatal("scan changed the producer result", err)
			}
			var item models.MetaItem
			if outcome == "success" {
				if err := db.Where("engine_id=?", engineID).First(&item).Error; err != nil {
					t.Fatal(err)
				}
				// The basic fact remains readable while enhancement is still running.
				identified, err := svc.ReadProductionIdentification(t.Context(), tenant, ref)
				if err != nil || identified.Status != "success" || identified.ScanExecutionID != basic.ExecutionID {
					t.Fatalf("deep blocked basic authorization facts: %+v %v", identified, err)
				}
				basicBinding := identified.Binding
				facts, err := svc.GetScanPlanFacts(t.Context(), tenant, deep.ExecutionID)
				if err != nil || facts.SourceScope == nil || !reflect.DeepEqual(facts.SourceScope.Anchors, []plugin.EngineCatalogPath{target}) {
					t.Fatal("deep plan did not retain its exact typed target", facts, err)
				}
				if concurrent {
					bounded, err := svc.productionReadContext(t.Context(), precise, *lease)
					if err != nil {
						t.Fatal("deep Worker read context unavailable", err)
					}
					if err := scanresource.CheckRefreshContent(bounded, target); err != nil {
						t.Fatal(err)
					}
					if err := scanresource.CheckRefreshContent(bounded, plugin.TabularItemPath(engineID, "schema", "Out.door", "sibling")); err == nil {
						t.Fatal("deep admitted a sibling")
					}
					receipt.TargetEngineVersion++
					_, err = svc.productionReadContext(t.Context(), precise, *lease)
					receipt.TargetEngineVersion--
					if err == nil {
						t.Fatal("changed owner receipt qualified for deep")
					}
					if err := db.Model(&execution.TaskExecution{}).Where("execution_id=?", deep.ExecutionID).Update("lease_token", uuid.NewString()).Error; err != nil {
						t.Fatal(err)
					}
					if err := scanresource.CheckCollectionRead(bounded, nil); err == nil {
						t.Fatal("old deep lease could still read")
					}
				}
				for _, mutate := range []func(*execution.TaskExecution){
					func(r *execution.TaskExecution) {
						r.ExecutionConfig["production_basic_execution_id"] = uuid.NewString()
					},
					func(r *execution.TaskExecution) {
						r.ExecutionConfig["targets"] = []string{"addp://engine/99700/path/Out.door?type=schema"}
					},
					func(r *execution.TaskExecution) { r.TenantID++ },
				} {
					copy := *precise
					copy.ExecutionConfig = commonmodels.JSONMap{}
					for k, v := range precise.ExecutionConfig {
						copy.ExecutionConfig[k] = v
					}
					mutate(&copy)
					proof, err := frozenProductionReceipt(&copy)
					if err == nil {
						err = svc.checkProductionBasicDependency(t.Context(), db, &copy, proof)
					}
					if err == nil {
						t.Fatal("mutated dependency or scope qualified")
					}
				}
				if err := db.Model(&execution.TaskExecution{}).Where("execution_id=?", deep.ExecutionID).Update("status", "failed").Error; err != nil {
					t.Fatal(err)
				}
				replayed, err := svc.EnsureProductionDeepScan(t.Context(), tenant, ref)
				if err != nil || replayed.Status != "failed" || replayed.ScanExecutionID != deep.ExecutionID {
					t.Fatal("deep failure revived on replay", replayed, err)
				}
				identified, err = svc.ReadProductionIdentification(t.Context(), tenant, ref)
				if err != nil || identified.Status != "success" || !reflect.DeepEqual(identified.Binding, basicBinding) {
					t.Fatal("deep failure changed basic facts", identified, err)
				}
			}
		})
	}
}
