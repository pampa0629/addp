package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const scanReadTenant = 710000090

func TestMetaScanExecutionReadIsolation(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	testMetaScanReadContract(t, db)
}

func TestMetaScanExecutionReadAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("META_POSTGRES_TEST_DSN is not set")
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
	// Record relation identities, not shared data counts: the fixture's rollback
	// must restore the pre-existing store even when a contract assertion fails.
	type storeRelation struct {
		Name string
		OID  int64 `gorm:"column:oid"`
	}
	storeRelations := func() []storeRelation {
		t.Helper()
		var relations []storeRelation
		if err := db.Raw(`SELECT relname AS name, oid::bigint AS oid FROM pg_class
			WHERE relnamespace = to_regnamespace('common') AND relname IN
			('task_executions', 'execution_events', 'execution_schema_migrations') ORDER BY relname`).Scan(&relations).Error; err != nil {
			t.Fatal(err)
		}
		return relations
	}
	before := storeRelations()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() {
		if err := tx.Rollback().Error; err != nil {
			t.Error(err)
			return
		}
		if after := storeRelations(); !reflect.DeepEqual(before, after) {
			t.Errorf("execution store relations not restored: before=%+v after=%+v", before, after)
		}
	})
	if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS common").Error; err != nil {
		t.Fatal(err)
	}
	// Always prove the production initializer on empty relations. AutoMigrate
	// alone omits SQL constraints, and a developer database may hide that gap.
	// The surrounding rollback restores the shared test store and dependencies.
	if err := tx.Exec(`DROP TABLE IF EXISTS common.execution_events,
		common.task_executions, common.execution_schema_migrations CASCADE`).Error; err != nil {
		t.Fatal(err)
	}
	if err := execution.EnsureStore(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS meta").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&models.MetaNode{}); err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&models.MetaItem{}); err != nil {
		t.Fatal(err)
	}
	testMetaScanReadContract(t, tx)
}

func testMetaScanReadContract(t *testing.T, db *gorm.DB) {
	t.Helper()
	tenant := strconv.Itoa(scanReadTenant)
	contexts := map[string]authorization.AuthContext{}
	for _, token := range []string{"owner", "peer", "oauth", "denied", "foreign", "machine", "wrong-machine", "machine-denied", "develop-machine", "develop-machine-denied", "manager-machine", "manager-machine-denied", "manager-machine-foreign", "transfer-machine"} {
		principal, targetTenant := "31", tenant
		if token == "peer" {
			principal = "32"
		}
		if token == "foreign" || token == "manager-machine-foreign" {
			targetTenant = strconv.Itoa(scanReadTenant + 1)
		}
		permissions := []string{"meta.scan_task.read", "meta.scan_task.execute"}
		if token == "denied" {
			permissions = []string{}
		}
		a := authtest.NewTenantUserAuthContext(targetTenant, principal, permissions)
		if token == "oauth" {
			client := "test-meta-reader"
			a.Client.ClientID = &client
			a.Client.ScopeMode = "restricted"
			a.Client.Scopes = []string{"meta.scan_task.read"}
			a.Token.Type = "oauth_access_token"
		}
		if strings.Contains(token, "machine") {
			a.Principal.Type = "service_principal"
			a.Token.Type = "service_access_token"
			a.Authentication.Methods = []string{"service_secret"}
			a.Authentication.AssuranceLevel = "not_applicable"
			client := "addp-orchestrator"
			if token == "transfer-machine" {
				client = "addp-transfer"
			}
			if strings.HasPrefix(token, "develop-machine") {
				client = "addp-develop"
			}
			if strings.HasPrefix(token, "manager-machine") {
				client = "addp-manager"
			}
			if token == "wrong-machine" {
				client = "addp-other"
			}
			a.Client.ClientID = &client
			a.Authorization.RoleAssignments[0].Permissions = []string{"meta.scan_task.execute", "meta.scan_task.read", "meta.task_provider.read"}
			if strings.HasSuffix(token, "-denied") {
				a.Authorization.RoleAssignments[0].Permissions = []string{"meta.scan_task.read"}
			}
		}
		if token == "denied" {
			a.Authorization.RoleAssignments = []authorization.RoleAssignment{}
		}
		if err := authorization.ValidateAuthContext(a); err != nil {
			t.Fatalf("invalid %s fixture: %v", token, err)
		}
		contexts["Bearer "+token] = a
	}
	var ownerRevoked atomic.Bool
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/oauth/token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "addp_at_meta_fixture", "token_type": "bearer", "expires_in": 300, "scope": "addp.api"})
			return
		}
		if r.URL.Path == "/api/v1/system/engines/9" && r.Header.Get("Authorization") == "Bearer addp_at_meta_fixture" {
			id := uint(scanReadTenant)
			json.NewEncoder(w).Encode(commonmodels.Engine{ID: 9, TenantID: &id, EngineType: "s3", LifecycleState: "active"})
			return
		}
		a, ok := contexts[r.Header.Get("Authorization")]
		if !ok {
			w.WriteHeader(401)
			return
		}
		if r.Header.Get("Authorization") == "Bearer owner" && ownerRevoked.Load() {
			a = authorization.CloneAuthContext(a)
			a.Authorization.RoleAssignments = []authorization.RoleAssignment{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(a)
	}))
	t.Cleanup(system.Close)
	tokens, err := commonclient.NewOAuthServiceTokenSource(system.URL, "addp-meta", "meta-scan-read-test-32-byte-secret-value", system.Client())
	if err != nil {
		t.Fatal(err)
	}
	engine := service.NewEngineService(db, commonclient.NewSystemServiceClient(system.URL, tokens, system.Client()))
	cfg := &config.Config{}
	cfg.SystemServiceURL = system.URL
	router := SetupRouter(cfg, db, engine, service.NewScanService(db, engine), nil, service.NewScanExecutionService(db, nil, engine, nil), nil, nil, modulelifecycle.NewStandalone("meta"))

	mine, peer := 31, 32
	definition, empty := "99", ""
	private := "never-expose-secret"
	fixtures := []execution.TaskExecution{
		{Module: "meta", TaskType: "scan", SourceTaskID: &definition, TriggeredBy: &peer},
		{Module: "meta", TaskType: "scan", TriggeredBy: &mine},
		{Module: "meta", TaskType: "scan", TriggeredBy: &peer},
		{Module: "meta", TaskType: "scan"},
		{Module: "develop", TaskType: "scan", TriggeredBy: &mine},
		{Module: "meta", TaskType: "cleanup_executor", TriggeredBy: &mine},
		{Module: "meta", TaskType: "scan", TenantID: scanReadTenant + 1, TriggeredBy: &mine},
		{Module: "meta", TaskType: "scan", SourceTaskID: &empty, TriggeredBy: &mine},
	}
	for i := range fixtures {
		f := &fixtures[i]
		if f.TenantID == 0 {
			f.TenantID = scanReadTenant
		}
		f.ExecutionID = fmt.Sprintf("71000009-0000-4000-8000-%012d", i+1)
		f.Source = "meta"
		f.Status = "failed"
		f.TriggerType = "manual"
		f.ExecutionConfig = commonmodels.JSONMap{"engine_id": 9, "scan_depth": "deep", "password": private}
		f.Metadata = commonmodels.JSONMap{"private": private, "items_scanned": 3, "step_results": map[string]interface{}{"scan": map[string]interface{}{"status": "failed", "result": private}}}
		f.Metadata["scan_progress"] = map[string]interface{}{"phase": "processing", "operation": "hashing", "files_discovered": 10, "items_planned": 2, "items_total_known": true, "items_completed": 1, "items_succeeded": 1, "current_path": "3d/model.obj", "current_bytes_read": 1024, "current_bytes_total": 4096, "private": private}
		if i == 1 {
			progress := f.Metadata["scan_progress"].(map[string]interface{})
			progress["operation"], progress["current_bytes_read"], progress["current_bytes_total"] = "enriching", 0, 0
			progress["current_range_bytes_read"], progress["current_ranges_read"] = 229376, 4096
		}
		f.ErrorDetails = commonmodels.JSONMap{"code": "meta.scan.failed", "message": private, "category": "source"}
		authorizationID, principalID, membershipID, version := int64(710000090+i), int64(31), int64(1), int64(1)
		expires := time.Now().UTC().Add(time.Hour)
		f.ExecutionAuthorizationID = &authorizationID
		f.ActorPrincipalID = &principalID
		f.ActorTenantMembershipID = &membershipID
		f.IssuedAuthorizationVersion = &version
		f.AuthorizationExpiresAt = &expires
		if err := db.Create(f).Error; err != nil {
			t.Fatal(err)
		}
	}
	request := func(token, path string, want int) map[string]interface{} {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v1/meta"+path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		router.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s", token, path, w.Code, want, w.Body)
		}
		for _, forbidden := range []string{"execution_config", "authorization_ref", "actor_principal_id", "execution_authorization_id", "lease_owner", private, "step_results"} {
			if strings.Contains(w.Body.String(), forbidden) {
				t.Fatalf("%s leaked %s", path, forbidden)
			}
		}
		var body map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if want >= 400 {
			for key := range body {
				if key != "error" && key != "error_code" {
					t.Fatalf("denial leaked %s", key)
				}
			}
		}
		return body
	}
	body := request("owner", "/executions/"+fixtures[1].ExecutionID, 200)
	progress, ok := body["scan_progress"].(map[string]interface{})
	if !ok || progress["files_discovered"] != float64(10) || progress["items_completed"] != float64(1) || progress["current_bytes_read"] != float64(0) || progress["current_range_bytes_read"] != float64(229376) || progress["current_ranges_read"] != float64(4096) {
		t.Fatalf("safe scan progress missing from detail: %v", body)
	}
	for _, token := range []string{"owner", "peer", "oauth"} {
		wantID := fixtures[1].ExecutionID
		if token == "peer" {
			wantID = fixtures[2].ExecutionID
		}
		seen := map[string]bool{}
		for _, path := range []string{"/scan/runs?page_size=1", "/scan/runs?page_size=1&page=2"} {
			body := request(token, path, 200)
			if body["total"] != float64(2) || body["total_pages"] != float64(2) {
				t.Fatalf("count leaks: %v", body)
			}
			for _, item := range body["items"].([]interface{}) {
				seen[item.(map[string]interface{})["execution_id"].(string)] = true
			}
		}
		if !seen[wantID] || !seen[fixtures[0].ExecutionID] || len(seen) != 2 {
			t.Fatalf("pagination returned wrong actors: %v", seen)
		}
		detail := request(token, "/executions/"+fixtures[0].ExecutionID, 200)
		if hash := detail["scan_progress"].(map[string]interface{}); hash["current_bytes_read"] != float64(1024) || hash["current_ranges_read"] != float64(0) {
			t.Fatalf("fingerprint and attribute observations mixed: %v", hash)
		}
		ctx := detail["scan_context"].(map[string]interface{})
		if ctx["engine_id"] != float64(9) || ctx["scan_depth"] != "deep" {
			t.Fatalf("scan context lost: %v", ctx)
		}
		request(token, "/executions/"+wantID, 200)
		hiddenID := fixtures[2].ExecutionID
		if token == "peer" {
			hiddenID = fixtures[1].ExecutionID
		}
		request(token, "/executions/"+hiddenID, 404)
		for _, index := range []int{3, 4, 5, 6, 7} {
			request(token, "/executions/"+fixtures[index].ExecutionID, 404)
		}
		request(token, "/executions/00000000-0000-4000-8000-000000000000", 404)
		history := request(token, "/scan/runs?task_id=99", 200)
		if history["total"] != float64(1) {
			t.Fatal("deleted task history missing from task filter")
		}
		filtered := request(token, "/scan/runs?task_id=100", 200)
		if filtered["total"] != float64(0) {
			t.Fatal("task filter leaked count")
		}
	}
	request("foreign", "/executions/"+fixtures[0].ExecutionID, 404)
	request("denied", "/scan/runs", 403)
	request("denied", "/executions/"+fixtures[0].ExecutionID, 403)
	// Reusing a formerly authorized token must consume current permissions;
	// neither another User nor the dedicated machine caller inherits revocation.
	ownerRevoked.Store(true)
	for _, path := range []string{"/scan/runs", "/scan/runs?task_id=99", "/executions/" + fixtures[0].ExecutionID, "/executions/" + fixtures[1].ExecutionID} {
		request("owner", path, http.StatusForbidden)
	}
	request("peer", "/executions/"+fixtures[0].ExecutionID, http.StatusOK)
	request("peer", "/executions/"+fixtures[1].ExecutionID, http.StatusNotFound)
	request("machine", "/task-provider/executions/"+fixtures[1].ExecutionID, http.StatusOK)
	ownerRevoked.Store(false)
	request("owner", "/executions/"+fixtures[1].ExecutionID, http.StatusOK)
	if restored := request("owner", "/scan/runs", http.StatusOK); restored["total"] != float64(2) {
		t.Fatal("restored permissions did not restore the original isolated list")
	}
	request("", "/scan/runs", 401)
	request("machine", "/scan/runs", 403)
	// Machine access intentionally reads another User's ad-hoc execution.
	request("machine", "/task-provider/executions/"+fixtures[2].ExecutionID, 200)
	for _, index := range []int{4, 5, 6} {
		request("machine", "/task-provider/executions/"+fixtures[index].ExecutionID, 404)
	}
	request("machine", "/task-provider/executions/00000000-0000-4000-8000-000000000000", 404)
	request("owner", "/task-provider/executions/"+fixtures[0].ExecutionID, 403)
	request("wrong-machine", "/task-provider/executions/"+fixtures[0].ExecutionID, 403)
	request("machine-denied", "/task-provider/executions/"+fixtures[0].ExecutionID, 403)
	testMetaDevelopProducedScan(t, db, router, request)
	testMetaManagerMosaicProducedScan(t, db, router, request)
	testTransferAndVectorProducedScans(t, db, router, request)

	// A synchronous Manager BFF submission must keep the actual User, rather than
	// introducing a service-created execution that the browser cannot read.
	bffServer := httptest.NewServer(router)
	defer bffServer.Close()
	bff := commonclient.NewMetaScanUserClient(bffServer.URL)
	run, err := bff.CreateManualScanRun(context.Background(), "owner", commonclient.MetaScanOptions{
		EngineID: 9, RefGroups: []commonclient.MetaScanRefGroup{{Primary: "results/upload-bff.parquet"}},
		ScanDepth: "deep", Force: true, Source: "manager",
	})
	if err != nil {
		t.Fatal(err)
	}
	var uploaded execution.TaskExecution
	if err := db.Where("execution_id = ?", run.ExecutionID).First(&uploaded).Error; err != nil {
		t.Fatal(err)
	}
	if uploaded.TriggeredBy == nil || *uploaded.TriggeredBy != mine || uploaded.TenantID != scanReadTenant || uploaded.Source != "manager" || uploaded.ParentExecutionID != nil || uploaded.SourceTaskID != nil {
		t.Fatal("upload scan lost its verified initiating User or tenant")
	}
	encoded, _ := json.Marshal(uploaded.ExecutionConfig)
	if strings.Contains(string(encoded), "Bearer") || strings.Contains(string(encoded), "owner") {
		t.Fatal("User credential entered persisted execution config")
	}
	request("owner", "/executions/"+run.ExecutionID, 200)
	request("peer", "/executions/"+run.ExecutionID, 404)
	request("foreign", "/executions/"+run.ExecutionID, 404)
	request("denied", "/executions/"+run.ExecutionID, 403)
}
