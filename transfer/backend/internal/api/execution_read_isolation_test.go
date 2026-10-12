package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonauth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/models"
	"github.com/addp/transfer/internal/service"
	"github.com/addp/transfer/internal/testpg"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTransferExecutionReadIsolation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	testTransferExecutionReadIsolation(t, db)
}

func TestIntegrationPostgresTransferExecutionReadIsolation(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("Transfer PostgreSQL gate enables integration tests")
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
	t.Cleanup(func() { pool.Close() })
	if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	testTransferExecutionReadIsolation(t, tx)
}

func testTransferExecutionReadIsolation(t *testing.T, db *gorm.DB) {
	t.Helper()
	const tenant = 710000095
	identities := map[string]authorization.AuthContext{}
	for token, id := range map[string]string{"owner": "31", "peer": "32", "oauth": "31", "foreign": "31", "denied": "31"} {
		tenantID := tenant
		if token == "foreign" {
			tenantID++
		}
		identity := authtest.NewTenantUserAuthContext(strconv.Itoa(tenantID), id, []string{"transfer.task.read"})
		if token == "denied" {
			identity.Authorization.RoleAssignments[0].Permissions = []string{"monitor.execution.read"}
		}
		if token == "oauth" {
			identity.Token.Type = "oauth_access_token"
			identity.Client.ScopeMode, identity.Client.Scopes = "restricted", []string{"addp.api"}
		}
		identities[token] = identity
	}
	for token, clientID := range map[string]string{"provider": "addp-orchestrator", "wrong-provider": "addp-meta", "export": "addp-develop", "wrong-export": "addp-manager"} {
		identity := authtest.NewTenantUserAuthContext(strconv.Itoa(tenant), "91", []string{"transfer.task_provider.read", "transfer.execution.read", "transfer.task.read"})
		identity.Principal.Type, identity.Token.Type = "service_principal", "service_access_token"
		identity.Authentication.Methods, identity.Authentication.AssuranceLevel = []string{"service_secret"}, "not_applicable"
		identity.Client.ClientID = &clientID
		identities[token] = identity
	}
	authorizationUnavailable := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authorizationUnavailable {
			w.WriteHeader(503)
			return
		}
		identity, ok := identities[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok || r.URL.Path != "/api/v1/system/auth/context" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(identity)
	}))
	t.Cleanup(server.Close)
	history, empty := "1", ""
	owner, peer, zero := 31, 32, 0
	rows := map[string]execution.TaskExecution{
		"history": {SourceTaskID: &history, TriggeredBy: &peer},
		"own":     {TriggeredBy: &owner}, "peer": {TriggeredBy: &peer},
		"missing-actor": {}, "zero-actor": {TriggeredBy: &zero},
		"empty-source": {SourceTaskID: &empty, TriggeredBy: &owner},
		"other-module": {Module: "meta", TaskType: "scan", SourceTaskID: &history},
		"cleanup":      {TaskType: "cleanup_executor", SourceTaskID: &history},
		"foreign":      {TenantID: tenant + 1, SourceTaskID: &history},
		"export":       {Source: "develop"},
	}
	ids := map[string]string{}
	now := time.Now().UTC().Add(-time.Minute)
	for name, row := range rows {
		row.ExecutionID = uuid.NewString()
		if row.TenantID == 0 {
			row.TenantID = tenant
		}
		if row.Module == "" {
			row.Module = "transfer"
		}
		if row.TaskType == "" {
			row.TaskType = "sync"
		}
		if row.Source == "" {
			row.Source = "transfer"
		}
		row.Status, row.TriggerType = "success", "manual"
		if name == "own" {
			row.Status = "failed"
			row.ErrorDetails = models.JSONMap{"message": "private-sentinel", "code": "query_output_lineage_unresolved"}
		}
		row.StartedAt, row.CompletedAt = &now, &now
		row.CreatedAt, row.UpdatedAt = now, now
		row.Metadata = models.JSONMap{"private": "private-sentinel", "checkpoint_state": map[string]interface{}{"cursor": "private-sentinel"}, "fencing_token": "private-sentinel", "target_refs": []interface{}{map[string]interface{}{"path": "tenant/export/result.csv", "primary": true, "private": "private-sentinel"}}}
		row.Metadata["continuous"] = map[string]interface{}{"partitions": map[string]interface{}{"0": map[string]interface{}{
			"type": "kafka_offset", "version": "v1", "values": map[string]string{"next_offset": "42", "private": "private-sentinel"},
		}}}
		row.ExecutionConfig = models.JSONMap{"sql": "private-sentinel"}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		ids[name] = row.ExecutionID
		event := execution.Event{TenantID: row.TenantID, ExecutionID: row.ExecutionID, Module: row.Module, TaskType: row.TaskType, Attempt: 1, OccurredAt: now, Kind: "completed", Counters: models.JSONMap{}}
		if err := db.Create(&event).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&execution.TaskExecution{}).Where("execution_id = ?", ids["peer"]).Update("parent_execution_id", ids["history"]).Error; err != nil {
		t.Fatal(err)
	}
	h := NewExecutionHandler(service.NewExecutionService(db, execution.NewTaskExecutionRepository(db)), nil)
	router := gin.New()
	router.Use(commonauth.MustNewMiddleware(commonauth.MiddlewareConfig{SystemURL: server.URL}), commonauth.MustNewContextGuard("tenant"))
	user := router.Group("", commonauth.MustNewPermissionGuard("transfer.task.read"))
	user.GET("/executions", h.ListExecutions)
	user.GET("/executions/statistics", h.GetExecutionStatistics)
	user.GET("/executions/:execution_id", h.GetExecution)
	user.GET("/executions/:execution_id/progress", h.GetExecutionProgress)
	user.GET("/executions/:execution_id/events", h.GetExecutionEvents)
	user.GET("/task-definitions/:id/executions", h.GetTaskExecutions)
	router.GET("/executions/:execution_id/result", commonauth.MustNewPermissionGuard("transfer.execution.read"), h.GetOwnedExecutionResult)
	router.GET("/task-provider/executions/:execution_id", commonauth.MustNewServiceClientGuard("addp-orchestrator"), commonauth.MustNewPermissionGuard("transfer.task_provider.read"), h.ProviderGetExecution)
	request := func(token, path string, want int) map[string]interface{} {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("%s %s status=%d want=%d body=%s", token, path, response.Code, want, response.Body.String())
		}
		for _, key := range []string{"private-sentinel", "execution_config", "checkpoint_state", "fencing_token", "execution_authorization_id", "lease_token"} {
			if strings.Contains(response.Body.String(), key) {
				t.Fatalf("%s exposed %s: %s", path, key, response.Body.String())
			}
		}
		var body map[string]interface{}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	for _, token := range []string{"owner", "peer", "oauth"} {
		ownName, peerName := "own", "peer"
		if token == "peer" {
			ownName, peerName = peerName, ownName
		}
		for _, suffix := range []string{"", "/progress", "/events"} {
			for _, name := range []string{"history", ownName} {
				body := request(token, "/executions/"+ids[name]+suffix, 200)
				if suffix == "" {
					partitions := body["metadata"].(map[string]interface{})["continuous"].(map[string]interface{})["partitions"].(map[string]interface{})
					values := partitions["0"].(map[string]interface{})["values"].(map[string]interface{})
					if values["next_offset"] != "42" {
						t.Fatalf("stored committed offset changed: %#v", values)
					}
				}
			}
			for _, name := range []string{peerName, "missing-actor", "zero-actor", "empty-source", "other-module", "cleanup", "foreign", "export"} {
				request(token, "/executions/"+ids[name]+suffix, 404)
			}
			request(token, "/executions/"+uuid.NewString()+suffix, 404)
		}
		body := request(token, "/executions?page_size=1", 200)
		if body["total"] != float64(2) || len(body["data"].([]interface{})) != 1 {
			t.Fatalf("list total or page leaks rows: %#v", body)
		}
		for _, suffix := range []string{"", "/progress"} {
			if token == "peer" {
				continue
			}
			body := request(token, "/executions/"+ids["own"]+suffix, 200)
			details, ok := body["error_details"].(map[string]interface{})
			if !ok || details["category"] != "execution_failed" || details["code"] != "query_output_lineage_unresolved" {
				t.Fatalf("safe lineage diagnostic missing: %#v", body)
			}
		}
		for _, path := range []string{"/executions?task_id=1", "/task-definitions/1/executions"} {
			if body := request(token, path, 200); body["total"] != float64(1) {
				t.Fatalf("deleted task history: %#v", body)
			}
		}
		if body := request(token, "/executions/statistics", 200); body["total_executions"] != float64(2) {
			t.Fatalf("statistics leaked rows: %#v", body)
		}
		if body := request(token, "/executions/statistics?task_id=1", 200); body["total_executions"] != float64(1) {
			t.Fatalf("statistics ignored task: %#v", body)
		}
		if body := request(token, "/executions/statistics?start_time="+time.Now().UTC().Format(time.RFC3339Nano), 200); body["total_executions"] != float64(0) {
			t.Fatalf("statistics ignored time: %#v", body)
		}
	}
	for _, path := range []string{"/executions", "/executions/statistics", "/executions/" + ids["history"], "/executions/" + ids["history"] + "/progress", "/executions/" + ids["history"] + "/events", "/task-definitions/1/executions"} {
		request("denied", path, 403)
		request("export", path, 403)
	}
	request("foreign", "/executions/"+ids["history"], 404)
	request("provider", "/task-provider/executions/"+ids["peer"], 200)
	request("provider", "/task-provider/executions/"+ids["cleanup"], 404)
	request("provider", "/task-provider/executions/"+ids["other-module"], 404)
	request("wrong-provider", "/task-provider/executions/"+ids["history"], 403)
	result := request("export", "/executions/"+ids["export"]+"/result", 200)
	refs := result["metadata"].(map[string]interface{})["target_refs"].([]interface{})
	if len(refs) != 1 || refs[0].(map[string]interface{})["path"] != "tenant/export/result.csv" {
		t.Fatalf("lost export target refs: %#v", result)
	}
	request("wrong-export", "/executions/"+ids["export"]+"/result", 404)
	request("export", "/executions/"+ids["history"]+"/result", 404)
	request("owner", "/executions/"+ids["export"]+"/result", 403)
	for _, path := range []string{"/executions?task_id=oops", "/executions/statistics?task_id=oops", "/executions/statistics?start_time=oops", "/executions/statistics?start_time=2026-10-12T00:00:00Z&end_time=2026-10-11T00:00:00Z"} {
		request("owner", path, 400)
	}
	// Authorization is re-evaluated; readable parent/history grants no peer child.
	identity := identities["owner"]
	identity.Authorization.RoleAssignments[0].Permissions = []string{"monitor.execution.read"}
	identities["owner"] = identity
	request("owner", "/executions/"+ids["history"], 403)
	authorizationUnavailable = true
	request("oauth", "/executions/"+ids["history"], 503)
}
