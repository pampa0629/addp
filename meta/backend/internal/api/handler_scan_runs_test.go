package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/metatest"
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
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS common").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&execution.TaskExecution{}); err != nil {
		t.Fatal(err)
	}
	testMetaScanReadContract(t, tx)
}

func testMetaScanReadContract(t *testing.T, db *gorm.DB) {
	t.Helper()
	tenant := strconv.Itoa(scanReadTenant)
	contexts := map[string]authorization.AuthContext{}
	for _, token := range []string{"owner", "peer", "oauth", "denied", "foreign", "machine", "wrong-machine", "machine-denied"} {
		principal, targetTenant := "31", tenant
		if token == "peer" {
			principal = "32"
		}
		if token == "foreign" {
			targetTenant = strconv.Itoa(scanReadTenant + 1)
		}
		permissions := []string{"meta.scan_task.read"}
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
			if token == "wrong-machine" {
				client = "addp-other"
			}
			a.Client.ClientID = &client
			a.Authorization.RoleAssignments[0].Permissions = []string{"meta.scan_task.read", "meta.task_provider.read"}
			if token == "machine-denied" {
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
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, ok := contexts[r.Header.Get("Authorization")]
		if !ok {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(a)
	}))
	t.Cleanup(system.Close)
	engine := service.NewEngineService(db, nil)
	cfg := &config.Config{}
	cfg.SystemServiceURL = system.URL
	router := SetupRouter(cfg, db, engine, service.NewScanService(db, engine), nil, service.NewScanExecutionService(db, nil, nil, nil), nil, nil, modulelifecycle.NewStandalone("meta"))

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
}
