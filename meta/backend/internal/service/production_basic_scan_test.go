package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/nfs"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/production"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanresource"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func metaProductionRef(id string) production.Reference {
	return production.Reference{Owner: "transfer", ExecutionID: id, ResultID: id}
}

func TestRuntimeProductionRegistryUsesAuthenticatedOwnerClients(t *testing.T) {
	id := uuid.NewString()
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer meta-test-token" {
			t.Error("production receipt read must use the authenticated Meta Service client")
		}
		calls[r.URL.Path]++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	tokens := client.ServiceTokenProviderFunc(func(_ context.Context, tenant uint) (string, error) {
		if tenant != 7 {
			t.Errorf("receipt requested outside the exact Tenant: %d", tenant)
		}
		return "meta-test-token", nil
	})
	registry, err := NewProductionRegistry(client.NewTransferClient(server.URL, tokens), client.NewDevelopClient(server.URL, tokens, server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []struct{ module, permission, path string }{
		{"transfer", "transfer.task.execute", "/api/v1/transfer/runtime/executions/" + id + "/production-receipt"},
		{"develop", "develop.task.execute", "/api/v1/develop/runtime/productions/" + id + "/" + id + "/receipt"},
	} {
		registered, ok := registry.Resolve(owner.module, "addp-"+owner.module)
		if !ok || registered.ExecutionPermission != owner.permission || registered.ReceiptPermission != owner.module+".production_receipt.read" {
			t.Fatalf("missing runtime producer %s", owner.module)
		}
		if _, ok := registry.Resolve(owner.module, "addp-unknown"); ok {
			t.Fatal("unregistered Service acquired producer identity")
		}
		_, err := registry.Read(t.Context(), 7, production.Reference{Owner: owner.module, ExecutionID: id, ResultID: id})
		if err == nil || calls[owner.path] != 1 {
			t.Fatalf("producer %s must use only its exact receipt route once, calls=%v err=%v", owner.module, calls, err)
		}
	}
}
func (s *ScanExecutionService) withTestProductionReader(reader func(context.Context, uint, string) (*client.TransferProductionReceipt, error)) *ScanExecutionService {
	registry, err := production.NewRegistry(production.Owner{Module: "transfer", ClientID: "addp-transfer", ReceiptPermission: "transfer.production_receipt.read", ExecutionPermission: "transfer.task.execute", ReadReceipt: func(ctx context.Context, tenant uint, ref production.Reference) (*production.Receipt, error) {
		native, err := reader(ctx, tenant, ref.ExecutionID)
		if err != nil {
			return nil, err
		}
		if native == nil || native.Validate() != nil {
			return nil, production.ErrEvidence
		}
		r := native.ProductionEvidence()
		return &r, nil
	}})
	if err != nil {
		panic(err)
	}
	return s.WithProductionRegistry(registry)
}
func TestProductionBasicScan(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	exerciseProductionDeepScan(t, db, false)
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
	if err := db.Exec("DROP SCHEMA IF EXISTS meta CASCADE;CREATE SCHEMA meta").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS meta CASCADE").Error; err != nil {
			t.Error(err)
		}
	})
	if err := db.AutoMigrate(&models.MetaNode{}, &models.MetaItem{}); err != nil {
		t.Fatal(err)
	}
	exerciseProductionDeepScan(t, db, true)
	testProductionBasicScan(t, db, true)
	tx := db.Begin()
	defer tx.Rollback()
	exerciseProductionIdentification(t, tx)

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
	r := client.TransferProductionReceipt{TargetEngineVersion: 3, SchemaVersion: client.TransferProductionReceiptSchema, TenantID: int64(tenant), ExecutionID: id, Attempt: 1, CommittedAt: time.Now().UTC(), Preparation: plugin.TableTargetCreated, CreatedAndCommitted: true,
		Target: plugin.TabularItemPath(9, "schema", "public", "result.with.dot"), UserOrigin: &client.TransferProductionUserOrigin{PrincipalID: 31, TenantMembershipID: 51, AuthorizationVersion: 6}}
	svc := NewScanExecutionService(db, nil, engines, nil)
	svc.withTestProductionReader(func(_ context.Context, tid uint, eid string) (*client.TransferProductionReceipt, error) {
		if tid != tenant || eid != id {
			t.Error("untrusted receipt identity")
		}
		copy := r
		return &copy, nil
	})
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
		func(r *client.TransferProductionReceipt) { r.TenantID++ },
	} {
		copy := r
		change(&copy)
		if func() bool {
			p := copy.ProductionEvidence()
			return p.Validate() == nil && p.TenantID == int64(tenant) && p.Reference == metaProductionRef(id) && p.Attempt == 1 && p.RetryOfExecutionID == nil && p.Objects[0].Created
		}() {
			t.Fatal("unproven or wrong-Tenant production qualified")
		}
	}
	var first *client.MetaProductionIdentification
	if concurrent {
		var wg sync.WaitGroup
		results := make(chan *client.MetaProductionIdentification, 2)
		failures := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				result, err := svc.EnsureProductionBasicScan(t.Context(), tenant, metaProductionRef(id))
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
		first, err = svc.EnsureProductionBasicScan(t.Context(), tenant, metaProductionRef(id))
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
	encodedTarget, err := json.Marshal(stored.ExecutionConfig["initial_production_receipt"])
	if err != nil {
		t.Fatal(err)
	}
	var frozenTarget production.Receipt
	if err := json.Unmarshal(encodedTarget, &frozenTarget); err != nil || !reflect.DeepEqual(frozenTarget.Primary, r.Target) {
		t.Fatalf("trusted typed target changed: %+v err=%v", frozenTarget, err)
	}
	facts, err := svc.GetScanPlanFacts(t.Context(), tenant, stored.ExecutionID)
	if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisComplete || facts.SourceScope == nil || facts.SourceScope.Mode != "exact" || !reflect.DeepEqual(facts.SourceScope.Anchors, []plugin.EngineCatalogPath{r.Target}) || facts.TaskID != nil {
		t.Fatalf("trusted new output facts=%+v err=%v", facts, err)
	}
	svc.withTestProductionReader(func(context.Context, uint, string) (*client.TransferProductionReceipt, error) {
		return nil, errors.New("offline")
	})
	if err := db.Model(&execution.TaskExecution{}).Where("execution_id=?", stored.ExecutionID).Update("status", "failed").Error; err != nil {
		t.Fatal(err)
	}
	replay, err := svc.EnsureProductionBasicScan(t.Context(), tenant, metaProductionRef(id))
	if err != nil || replay.ScanExecutionID != first.ScanExecutionID || replay.Status != "failed" {
		t.Fatal("replay revived a failed scan or needed live owner", replay, err)
	}
	var count int64
	if err := db.Model(&execution.TaskExecution{}).Where("tenant_id=? AND parent_execution_id=?", tenant, id).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("scans=%d error=%v", count, err)
	}
	// A registered file producer follows the same path, with no invented human
	// origin. The exact file scope also drives the actual Worker read guard.
	caps, err := plugin.MarshalEngineCapabilities((&nfs.NFSPlugin{}).Capabilities())
	if err != nil {
		t.Fatal(err)
	}
	nfsCaps := commonmodels.JSONString(caps)
	nfsEngine := &commonmodels.Engine{ID: 19, TenantID: &tenant, EngineType: "nfs", Version: 3, LifecycleState: "active", Capabilities: &nfsCaps}
	engines.cacheEngine(tenant, nfsEngine)
	fileRef := production.Reference{Owner: "develop", ExecutionID: id, ResultID: uuid.NewString()}
	fileTarget := plugin.FileItemPath(19, "out/new.gpkg")
	fileReceipt := production.Receipt{Reference: fileRef, TenantID: int64(tenant), Attempt: 1, TargetEngineVersion: 3, OperationID: uuid.NewString(), CommittedAt: time.Now(), Primary: fileTarget, Objects: []production.CommittedObject{{Target: fileTarget, Created: true}}, Sources: production.SourceEvidence{Kind: production.SourcesNone}}
	registry, err := production.NewRegistry(production.Owner{Module: "develop", ClientID: "addp-develop", ReceiptPermission: "develop.production_receipt.read", ExecutionPermission: "develop.task.execute", ReadReceipt: func(context.Context, uint, production.Reference) (*production.Receipt, error) {
		return &fileReceipt, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	svc.WithProductionRegistry(registry)
	fileScan, err := svc.EnsureProductionBasicScan(t.Context(), tenant, fileRef)
	if err != nil {
		t.Fatal(err)
	}
	var fileRun execution.TaskExecution
	if err := db.Where("execution_id=?", fileScan.ScanExecutionID).Take(&fileRun).Error; err != nil {
		t.Fatal(err)
	}
	if fileRun.ActorPrincipalID != nil || fileRun.ActorTenantMembershipID != nil || fileRun.IssuedAuthorizationVersion != nil || fileRun.TriggeredBy != nil {
		t.Fatal("service-only scan acquired a human origin")
	}
	if fileRun.ExecutionConfig["initial_production_new_candidate"] != true {
		t.Fatal("fresh file result lost logical candidate")
	}
	fileFacts, err := svc.GetScanPlanFacts(t.Context(), tenant, fileRun.ExecutionID)
	if err != nil || fileFacts.SourceScope == nil || !reflect.DeepEqual(fileFacts.SourceScope.Anchors, []plugin.EngineCatalogPath{fileTarget}) {
		t.Fatal("file scope broadened", fileFacts, err)
	}
	if concurrent {
		token, owner := uuid.NewString(), "production-scan-fixture"
		until := time.Now().Add(time.Minute)
		if err := db.Model(&fileRun).Updates(map[string]any{"status": "running", "attempt": 1, "lease_token": token, "lease_owner": owner, "lease_expires_at": until}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("id=?", fileRun.ID).Take(&fileRun).Error; err != nil {
			t.Fatal(err)
		}
		lease := execution.Lease{TenantID: fileRun.TenantID, ExecutionID: fileRun.ExecutionID, Attempt: 1, Token: token, Owner: owner}
		bounded, err := svc.productionReadContext(t.Context(), &fileRun, lease)
		if err != nil {
			t.Fatal(err)
		}
		if err := scanresource.CheckRefreshContent(bounded, fileTarget); err != nil {
			t.Fatal(err)
		}
		if scanresource.CheckRefreshContent(bounded, plugin.FileItemPath(19, "out/sibling.gpkg")) == nil || scanresource.CheckRefreshEnumeration(bounded, plugin.FileDirectoryPath(19, "out"), false) == nil {
			t.Fatal("produced file scope expanded")
		}
		if err := scanresource.CheckCollectionRead(bounded, nfsEngine); err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&fileRun).Update("lease_token", uuid.NewString()).Error; err != nil {
			t.Fatal(err)
		}
		if scanresource.CheckCollectionRead(bounded, nfsEngine) == nil {
			t.Fatal("superseded scan lease retained source access")
		}
	}

}
