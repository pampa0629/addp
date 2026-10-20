package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type productionScanReceiptReader func(context.Context, uint, string) (*client.TransferProductionReceipt, error)

func (f productionScanReceiptReader) ReadProductionReceipt(ctx context.Context, tenant uint, id string) (*client.TransferProductionReceipt, error) {
	return f(ctx, tenant, id)
}

func TestProductionBasicScan(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	testProductionBasicScan(t, db, false)
}

func TestProductionBasicScanAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("run make test-meta-postgres")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	testProductionBasicScan(t, db, true)
}

func testProductionBasicScan(t *testing.T, db *gorm.DB, concurrent bool) {
	tenant := uint(710000096)
	id := uuid.NewString()
	capabilities, err := plugin.MarshalEngineCapabilities((&postgresql.PostgreSQLPlugin{}).Capabilities())
	if err != nil {
		t.Fatal(err)
	}
	capJSON := commonmodels.JSONString(capabilities)
	engines := NewEngineService(db, nil)
	engines.cacheEngine(tenant, &commonmodels.Engine{ID: 9, TenantID: &tenant, EngineType: "postgresql", Version: 3, LifecycleState: "active", Capabilities: &capJSON})
	r := client.TransferProductionReceipt{SchemaVersion: client.TransferProductionReceiptSchema, TenantID: int64(tenant), ExecutionID: id, Attempt: 1, CommittedAt: time.Now().UTC(), Preparation: plugin.TableTargetCreated, CreatedAndCommitted: true,
		Target: plugin.TabularItemPath(9, "schema", "public", "result.with.dot"), UserOrigin: &client.TransferProductionUserOrigin{PrincipalID: 31, TenantMembershipID: 51, AuthorizationVersion: 6}}
	svc := NewScanExecutionService(db, nil, engines, nil)
	svc.WithTransferProductionReceiptReader(productionScanReceiptReader(func(_ context.Context, tid uint, eid string) (*client.TransferProductionReceipt, error) {
		if tid != tenant || eid != id {
			t.Error("untrusted receipt identity")
		}
		copy := r
		return &copy, nil
	}))
	t.Cleanup(func() {
		if err := db.Where("tenant_id=? AND module='meta' AND parent_execution_id=?", tenant, id).Delete(&execution.TaskExecution{}).Error; err != nil {
			t.Error(err)
		}
		var count int64
		if err := db.Model(&execution.TaskExecution{}).Where("tenant_id=? AND module='meta' AND parent_execution_id=?", tenant, id).Count(&count).Error; err != nil || count != 0 {
			t.Errorf("scan cleanup=%d error=%v", count, err)
		}
	})
	for _, change := range []func(*client.TransferProductionReceipt){
		func(r *client.TransferProductionReceipt) { r.CreatedAndCommitted = false },
		func(r *client.TransferProductionReceipt) {
			r.Preparation = plugin.TableTargetRecreated
			r.CreatedAndCommitted = false
		},
		func(r *client.TransferProductionReceipt) { r.Attempt = 2 },
		func(r *client.TransferProductionReceipt) { r.UserOrigin = nil },
		func(r *client.TransferProductionReceipt) { r.TenantID++ },
	} {
		copy := r
		change(&copy)
		if validProductionBasicScanReceipt(&copy, tenant, id) {
			t.Fatal("unproven or wrong-Tenant production qualified")
		}
	}
	var first *client.MetaProductionBasicScan
	if concurrent {
		var wg sync.WaitGroup
		results := make(chan *client.MetaProductionBasicScan, 2)
		failures := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				result, err := svc.EnsureTransferProductionBasicScan(t.Context(), tenant, id)
				results <- result
				failures <- err
			}()
		}
		wg.Wait()
		close(results)
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		for result := range results {
			if first != nil && result.ScanExecutionID != first.ScanExecutionID {
				t.Fatal("duplicate concurrent scans")
			}
			first = result
		}
	} else {
		first, err = svc.EnsureTransferProductionBasicScan(t.Context(), tenant, id)
		if err != nil {
			t.Fatal(err)
		}
	}
	var stored execution.TaskExecution
	if err := db.Where("execution_id=?", first.ScanExecutionID).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ParentExecutionID == nil || *stored.ParentExecutionID != id || stored.ActorPrincipalID == nil || *stored.ActorPrincipalID != 31 || stored.ActorTenantMembershipID == nil || *stored.ActorTenantMembershipID != 51 || stored.IssuedAuthorizationVersion == nil || *stored.IssuedAuthorizationVersion != 6 || stored.ExecutionAuthorizationID != nil {
		t.Fatal("scan lost human provenance or copied authorization")
	}
	if stored.ExecutionConfig["scan_depth"] != "basic" || stored.ExecutionConfig["force"] != true {
		t.Fatalf("broadened or deep scan config=%+v", stored.ExecutionConfig)
	}
	targets := jsonMapStringSlice(stored.ExecutionConfig, "targets")
	if len(targets) != 1 || targets[0] != "addp://engine/9/path/public/result.with.dot?type=table" {
		t.Fatalf("not exact target: %+v", targets)
	}
	encodedTarget, err := json.Marshal(stored.ExecutionConfig["initial_production_target"])
	if err != nil {
		t.Fatal(err)
	}
	var frozenTarget plugin.EngineCatalogPath
	if err := json.Unmarshal(encodedTarget, &frozenTarget); err != nil || !reflect.DeepEqual(frozenTarget, r.Target) {
		t.Fatalf("trusted typed target changed: %+v err=%v", frozenTarget, err)
	}
	facts, err := svc.GetScanPlanFacts(t.Context(), tenant, stored.ExecutionID)
	if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisComplete || facts.SourceScope == nil || facts.SourceScope.Mode != "exact" || !reflect.DeepEqual(facts.SourceScope.Anchors, []plugin.EngineCatalogPath{r.Target}) || facts.TaskID != nil {
		t.Fatalf("trusted new output facts=%+v err=%v", facts, err)
	}
	svc.WithTransferProductionReceiptReader(productionScanReceiptReader(func(context.Context, uint, string) (*client.TransferProductionReceipt, error) {
		return nil, errors.New("offline")
	}))
	if err := db.Model(&execution.TaskExecution{}).Where("execution_id=?", stored.ExecutionID).Update("status", "failed").Error; err != nil {
		t.Fatal(err)
	}
	replay, err := svc.EnsureTransferProductionBasicScan(t.Context(), tenant, id)
	if err != nil || replay.ScanExecutionID != first.ScanExecutionID || replay.Status != "failed" {
		t.Fatal("replay revived a failed scan or needed live owner", replay, err)
	}
	var count int64
	if err := db.Model(&execution.TaskExecution{}).Where("tenant_id=? AND parent_execution_id=?", tenant, id).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("scans=%d error=%v", count, err)
	}
}
