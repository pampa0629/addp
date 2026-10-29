package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/client"
	plugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/service"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProductionReceiptRouteRequiresFixedSystemServiceAndPermission(t *testing.T) {
	const permission = "transfer.production_receipt.read"
	auth := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer system":       {ClientID: "addp-system", Permissions: []string{permission}},
		"Bearer meta":         {ClientID: "addp-meta", Permissions: []string{permission}},
		"Bearer unprivileged": {ClientID: "addp-system", Permissions: []string{"transfer.task.read"}},
		"Bearer transfer":     {ClientID: "addp-transfer", Permissions: []string{permission}},
		"Bearer catalog":      {ClientID: "addp-catalog", Permissions: []string{permission}},
	})
	defer auth.Close()
	router := SetupRouter(NewCleanupResultHandler(nil), nil, nil, nil, nil, auth.URL, "", nil, nil, nil, modulelifecycle.NewStandalone("transfer"))
	path := "/api/v1/transfer/runtime/executions/" + uuid.NewString() + "/production-receipt"
	for _, tc := range []struct {
		token, query string
		status       int
	}{{"", "", 401}, {"transfer", "", 403}, {"catalog", "", 403}, {"unprivileged", "", 403}, {"system", "?tenant_id=8", 400}, {"system", "", 500}, {"meta", "", 500}} {
		req := httptest.NewRequest(http.MethodGet, path+tc.query, nil)
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != tc.status {
			t.Fatalf("%s%s status=%d want=%d body=%s", tc.token, tc.query, response.Code, tc.status, response.Body.String())
		}
	}
}

func TestProductionReceiptRejectsHumanEvenWithPermission(t *testing.T) {
	auth := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{"Bearer user": {"transfer.production_receipt.read"}})
	defer auth.Close()
	router := SetupRouter(NewCleanupResultHandler(nil), nil, nil, nil, nil, auth.URL, "", nil, nil, nil, modulelifecycle.NewStandalone("transfer"))
	req := httptest.NewRequest("GET", "/api/v1/transfer/runtime/executions/"+uuid.NewString()+"/production-receipt", nil)
	req.Header.Set("Authorization", "Bearer user")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("human receipt access status=%d body=%s", response.Code, response.Body.String())
	}
}

type productionReceiptToken string

func (token productionReceiptToken) Token(context.Context, uint) (string, error) {
	return string(token), nil
}

func TestProductionReceiptReadThroughSharedClient(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	testProductionReceiptRead(t, db)
}

func TestIntegrationPostgresProductionReceiptRead(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run make test-transfer-postgres")
	}
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(testpg.ConnInfoFromEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	testProductionReceiptRead(t, db)
}

func testProductionReceiptRead(t *testing.T, db *gorm.DB) {
	t.Helper()
	ids := []string{uuid.NewString(), uuid.NewString()}
	now := time.Now().UTC()
	for index, id := range ids {
		tenant := 7 + index
		record := models.TargetProduction{SchemaVersion: models.TargetProductionSchema, TenantID: tenant, ExecutionID: id, Attempt: 1, TargetLocator: "addp://engine/2/path/outdoor/persons?type=table", Phase: models.TargetProductionCommitted, Preparation: plugin.TableTargetCreated, StartedAt: now, PreparedAt: &now, CommittedAt: &now}
		record.SourceReadSet, _ = plugin.NewQueryReadSet(plugin.TabularItemPath(uint(9007199254740993), "schema", "outdoor", "source"))
		record.CreatedAndCommitted = true
		encoded, err := models.EncodeTargetProduction(record)
		if err != nil {
			t.Fatal(err)
		}
		item := execution.TaskExecution{TenantID: tenant, ExecutionID: id, Module: "transfer", TaskType: "sync", Source: "transfer", TriggerType: "manual", ExecutionBoundary: "bounded", Status: "success", Attempt: 1, Metadata: commonmodels.JSONMap{models.TargetProductionMetadataKey: encoded, "private": "private-sentinel"}}
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		if err := db.Where("execution_id IN ?", ids).Delete(&execution.TaskExecution{}).Error; err != nil {
			t.Error(err)
		}
		var count int64
		if err := db.Model(&execution.TaskExecution{}).Where("execution_id IN ?", ids).Count(&count).Error; err != nil || count != 0 {
			t.Errorf("receipt fixture cleanup count=%d error=%v", count, err)
		}
	})
	auth := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{"Bearer system": {ClientID: "addp-system", Permissions: []string{"transfer.production_receipt.read"}}})
	defer auth.Close()
	svc := service.NewExecutionService(db, execution.NewTaskExecutionRepository(db))
	router := SetupRouter(NewCleanupResultHandler(nil), nil, svc, nil, nil, auth.URL, "", nil, nil, nil, modulelifecycle.NewStandalone("transfer"))
	server := httptest.NewServer(router)
	defer server.Close()
	consumer := client.NewTransferClient(server.URL, productionReceiptToken("system"))
	receipt, err := consumer.ReadProductionReceipt(t.Context(), 7, ids[0])
	if err != nil || receipt.ExecutionID != ids[0] || receipt.UserOrigin != nil || receipt.Preparation != plugin.TableTargetCreated || !receipt.CreatedAndCommitted {
		t.Fatalf("shared client receipt=%+v error=%v", receipt, err)
	}
	if receipt.SourceReadSet == nil || receipt.SourceReadSet.Paths[0].EngineID != uint(9007199254740993) {
		t.Fatal("stored owner evidence or HTTP client rounded the source Engine ID")
	}
	if _, err := consumer.ReadProductionReceipt(t.Context(), 7, ids[1]); err == nil {
		t.Fatal("cross tenant receipt returned")
	}
	req := httptest.NewRequest("GET", "/api/v1/transfer/runtime/executions/"+ids[0]+"/production-receipt", nil)
	req.Header.Set("Authorization", "Bearer system")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	var wire map[string]interface{}
	if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "private-sentinel") || json.Unmarshal(response.Body.Bytes(), &wire) != nil || wire["metadata"] != nil || wire["outputs"] != nil || wire["user_origin"] != nil || wire["created_and_committed"] != true {
		t.Fatalf("unsafe receipt response: %d %s", response.Code, response.Body.String())
	}
}
