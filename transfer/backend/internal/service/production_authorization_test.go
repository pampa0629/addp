package service

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type productionRuntimeFixture struct {
	lookup func(context.Context, uint, string) (*authorization.InitialProductionLookup, error)
	write  func(context.Context, uint, string) (*authorization.InitialProductionResult, error)
}

func (f productionRuntimeFixture) resolve(ctx context.Context, tenant uint, id string) (*authorization.InitialProductionLookup, error) {
	return f.lookup(ctx, tenant, id)
}
func (f productionRuntimeFixture) issue(ctx context.Context, tenant uint, id string) (*authorization.InitialProductionResult, error) {
	return f.write(ctx, tenant, id)
}

type basicScannerFixture func(context.Context, uint, string) (*client.MetaProductionBasicScan, error)

func (f basicScannerFixture) EnsureTransferProductionBasicScan(ctx context.Context, tenant uint, id string) (*client.MetaProductionBasicScan, error) {
	return f(ctx, tenant, id)
}

func initialAuthorizationFixture(t *testing.T, db *gorm.DB) (*ExecutionService, execution.TaskExecution) {
	t.Helper()
	now := time.Now().UTC()
	by := 31
	principal, member, version := int64(31), int64(51), int64(6)
	item := execution.TaskExecution{TenantID: 710000097, ExecutionID: uuid.NewString(), Module: "transfer", TaskType: "sync", Source: "transfer", TriggerType: "manual", Status: "running", TriggeredBy: &by,
		ActorPrincipalID: &principal, ActorTenantMembershipID: &member, IssuedAuthorizationVersion: &version, Metadata: commonmodels.JSONMap{}}
	r := models.TargetProduction{SchemaVersion: models.TargetProductionSchema, TenantID: item.TenantID, ExecutionID: item.ExecutionID, Attempt: 1, TargetLocator: "addp://engine/9/path/public/new_result?type=table", Phase: models.TargetProductionCommitted,
		Preparation: plugin.TableTargetCreated, CreatedAndCommitted: true, StartedAt: now, PreparedAt: &now, CommittedAt: &now}
	r.SourceReadSet, _ = plugin.NewQueryReadSet(plugin.TabularItemPath(9, "schema", "public", "source"))
	encoded, err := models.EncodeTargetProduction(r)
	if err != nil {
		t.Fatal(err)
	}
	item.Metadata[models.TargetProductionMetadataKey] = encoded
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Where("tenant_id=? AND execution_id=?", item.TenantID, item.ExecutionID).Delete(&execution.Event{}).Error; err != nil {
			t.Error(err)
		}
		if err := db.Where("tenant_id=? AND execution_id=?", item.TenantID, item.ExecutionID).Delete(&execution.TaskExecution{}).Error; err != nil {
			t.Error(err)
		}
		var count int64
		if err := db.Model(&execution.TaskExecution{}).Where("execution_id=?", item.ExecutionID).Count(&count).Error; err != nil || count != 0 {
			t.Errorf("initial authorization fixture residual=%d error=%v", count, err)
		}
	})
	svc := NewExecutionService(db, execution.NewTaskExecutionRepository(db))
	bindExecutionServiceTestLease(t, db, svc, &item)
	if err := svc.FinishExecution(t.Context(), uint(item.ID), models.ExecutionStatusSuccess, nil, nil); err != nil {
		t.Fatal(err)
	}
	return svc, item
}

func TestInitialProductionAuthorizationContinuation(t *testing.T) {
	for _, scenario := range []string{"transient", "scan", "unclassified503", "denied", "issued_history"} {
		t.Run(scenario, func(t *testing.T) {
			db := newExecutionServiceTestDB(t)
			svc, item := initialAuthorizationFixture(t, db)
			var resolves, writes, scans int
			result := &authorization.InitialProductionResult{SchemaVersion: authorization.InitialProductionResultSchema, TenantID: int64(item.TenantID), ProducerOwner: "transfer", ExecutionID: uuid.MustParse(item.ExecutionID), Outcome: "denied", DenialCode: "source_access_denied", RecordedAt: time.Now().UTC()}
			if scenario == "issued_history" {
				result.Outcome = "issued"
				result.DenialCode = ""
				result.Grant = &authorization.InitialProductionGrant{RequestID: uuid.New(), GrantedAt: time.Now().UTC(), ExpiryMode: authorization.SharingExpiryUntilRevoked}
			}
			continuation := &ProductionAuthorizationContinuation{executions: svc, runtime: productionRuntimeFixture{
				lookup: func(_ context.Context, tenant uint, id string) (*authorization.InitialProductionLookup, error) {
					resolves++
					if tenant != uint(item.TenantID) || id != item.ExecutionID {
						t.Fatal("untrusted continuation identity")
					}
					if scenario == "transient" {
						return nil, errors.New("network unknown")
					}
					if scenario == "issued_history" {
						return &authorization.InitialProductionLookup{Found: true, Result: result}, nil
					}
					return &authorization.InitialProductionLookup{}, nil
				},
				write: func(context.Context, uint, string) (*authorization.InitialProductionResult, error) {
					writes++
					if scenario == "scan" {
						return nil, &client.TenantAPIError{StatusCode: 503, ErrorCode: "initial_production_pending"}
					}
					if scenario == "unclassified503" {
						return nil, &client.TenantAPIError{StatusCode: 503}
					}
					return result, nil
				},
			}, meta: basicScannerFixture(func(_ context.Context, tenant uint, id string) (*client.MetaProductionBasicScan, error) {
				scans++
				if tenant != uint(item.TenantID) || id != item.ExecutionID {
					t.Fatal("scan scope changed")
				}
				return &client.MetaProductionBasicScan{ProductionExecutionID: id, ScanExecutionID: uuid.NewString(), Status: "pending"}, nil
			})}
			now := time.Now().UTC()
			if err := continuation.runOne(t.Context(), now); err != nil {
				t.Fatal(err)
			}
			var stored execution.TaskExecution
			if err := db.First(&stored, item.ID).Error; err != nil {
				t.Fatal(err)
			}
			state, err := models.DecodeProductionAuthorization(stored.Metadata[models.ProductionAuthorizationMetadataKey])
			if err != nil {
				t.Fatal(err)
			}
			want := "pending"
			if scenario == "denied" {
				want = "denied"
			}
			if scenario == "issued_history" {
				want = "issued"
			}
			if state.Status != want || state.Attempts != 1 || stored.Status != "success" || stored.Metadata[models.TargetProductionMetadataKey] != item.Metadata[models.TargetProductionMetadataKey] {
				t.Fatalf("state=%+v physical status=%s", state, stored.Status)
			}
			if (scans == 1) != (scenario == "scan") || scenario == "issued_history" && writes != 0 || scenario == "transient" && writes != 0 {
				t.Fatalf("unexpected resolve/write/scan=%d/%d/%d", resolves, writes, scans)
			}
			if err := continuation.runOne(t.Context(), now.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if resolves != 1 {
				t.Fatal("backoff or terminal result was ignored")
			}
			if err := svc.UpdateExecution(t.Context(), uint(item.ID), map[string]interface{}{"metadata": map[string]interface{}{models.ProductionAuthorizationMetadataKey: map[string]interface{}{"status": "issued"}}}); err == nil {
				t.Fatal("generic writer forged continuation outcome")
			}
			if err := svc.FinishExecution(t.Context(), uint(item.ID), models.ExecutionStatusSuccess, nil, commonmodels.JSONMap{models.ProductionAuthorizationMetadataKey: state}); err == nil {
				t.Fatal("generic completion forged continuation outcome")
			}
		})
	}
}

func TestInitialAuthorizationDoesNotBackfillHistoricalSuccess(t *testing.T) {
	db := newExecutionServiceTestDB(t)
	svc, item := initialAuthorizationFixture(t, db)
	var stored execution.TaskExecution
	if err := db.First(&stored, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	delete(stored.Metadata, models.ProductionAuthorizationMetadataKey)
	if err := db.Model(&stored).Update("metadata", stored.Metadata).Error; err != nil {
		t.Fatal(err)
	}
	rpc := func(context.Context, uint, string) (*authorization.InitialProductionLookup, error) {
		t.Fatal("historical success was backfilled")
		return nil, nil
	}
	c := &ProductionAuthorizationContinuation{executions: svc, runtime: productionRuntimeFixture{lookup: rpc}, meta: basicScannerFixture(func(context.Context, uint, string) (*client.MetaProductionBasicScan, error) {
		t.Fatal("historical scan was created")
		return nil, nil
	})}
	if err := c.runOne(t.Context(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationPostgresInitialAuthorizationContinuation(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run make test-transfer-postgres")
	}
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(testpg.ConnInfoFromEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	svc, item := initialAuthorizationFixture(t, db)
	var reads atomic.Int32
	c := &ProductionAuthorizationContinuation{executions: svc, runtime: productionRuntimeFixture{lookup: func(context.Context, uint, string) (*authorization.InitialProductionLookup, error) {
		reads.Add(1)
		return nil, errors.New("temporary outage")
	}}, meta: basicScannerFixture(func(context.Context, uint, string) (*client.MetaProductionBasicScan, error) {
		t.Error("outage unexpectedly submitted scan")
		return nil, nil
	})}
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- c.runOne(t.Context(), time.Now().UTC()) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if reads.Load() != 1 {
		t.Fatalf("duplicate simultaneous claims=%d", reads.Load())
	}
	var stored execution.TaskExecution
	if err := db.First(&stored, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	state, err := models.DecodeProductionAuthorization(stored.Metadata[models.ProductionAuthorizationMetadataKey])
	if err != nil || state.Attempts != 1 || state.Status != "pending" || stored.Status != "success" {
		t.Fatal("outage changed physical execution", state, err)
	}
}
