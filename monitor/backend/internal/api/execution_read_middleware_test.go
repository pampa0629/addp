package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonauth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type requestScopeResolver struct{ revoked, unavailable bool }

func (r *requestScopeResolver) Resolve(_ context.Context, _ []string, tenant int, principal int64, bearer string) ([]execution.ReadScope, error) {
	if r.unavailable {
		return nil, service.ErrExecutionOwnerUnavailable
	}
	if r.revoked {
		return []execution.ReadScope{}, nil
	}
	return []execution.ReadScope{{TenantID: tenant, PrincipalID: principal, Module: "orchestrator", Grants: []execution.ReadGrant{{TaskType: "orchestration", TaskHistory: true}}}}, nil
}

type executionReadModuleRegistry struct{ modules []*commonclient.ModuleInfo }

func (r executionReadModuleRegistry) ListActiveModules(context.Context) ([]*commonclient.ModuleInfo, error) {
	return r.modules, nil
}

func TestExecutionReadAPIReauthenticatesThroughOwnerForEveryRequest(t *testing.T) {
	var revoked, unavailable atomic.Bool
	var identityReads, ownerReads atomic.Int64
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer execution-reader" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		identityReads.Add(1)
		permissions := []string{"monitor.execution.read", "monitor.statistics.read"}
		if !revoked.Load() {
			permissions = append(permissions, "orchestrator.workflow.read")
		}
		_ = json.NewEncoder(w).Encode(authtest.NewTenantUserAuthContext("7", "9", permissions))
	}))
	defer system.Close()
	ownerRouter := gin.New()
	ownerRouter.Use(commonauth.MustNewMiddleware(commonauth.MiddlewareConfig{SystemURL: system.URL}), commonauth.MustNewContextGuard("tenant"))
	ownerRouter.GET("/api/v1/:module/execution-read-scope", func(c *gin.Context) {
		ownerReads.Add(1)
		if unavailable.Load() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "private-owner-details"})
			return
		}
		module := c.Param("module")
		permissions := map[string]string{"orchestration": "orchestrator.workflow.read"}
		if module == "transfer" {
			permissions = map[string]string{"sync": "transfer.task.read"}
		}
		execution.OwnerReadScopeHandler(module, permissions)(c)
	})
	owner := httptest.NewServer(ownerRouter)
	defer owner.Close()
	registry := executionReadModuleRegistry{}
	for _, module := range []string{"orchestrator", "transfer"} {
		registry.modules = append(registry.modules, &commonclient.ModuleInfo{ModuleName: module, Enabled: true,
			Instances: []commonclient.ModuleRuntimeInstanceInfo{{Role: "backend", Status: "up", ModuleURL: owner.URL, LeaseExpiresAt: time.Now().Add(time.Hour)}}})
	}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	repo := execution.NewTaskExecutionRepository(db)
	task, root, actor, peer := "deleted-definition", "root", 9, 10
	for _, row := range []execution.TaskExecution{
		{ExecutionID: root, TenantID: 7, Module: "orchestrator", TaskType: "orchestration", SourceTaskID: &task},
		{ExecutionID: "mine", TenantID: 7, Module: "orchestrator", TaskType: "orchestration", TriggeredBy: &actor},
		{ExecutionID: "peer-private", TenantID: 7, Module: "orchestrator", TaskType: "orchestration", TriggeredBy: &peer},
		{ExecutionID: "missing-actor-private", TenantID: 7, Module: "orchestrator", TaskType: "orchestration"},
		{ExecutionID: "child-private", TenantID: 7, Module: "transfer", TaskType: "sync", SourceTaskID: &task, ParentExecutionID: &root},
		{ExecutionID: "tenant-private", TenantID: 8, Module: "orchestrator", TaskType: "orchestration", SourceTaskID: &task},
	} {
		row.Status, row.TriggerType = "failed", "manual"
		row.ExecutionConfig = models.JSONMap{"password": "payload-private"}
		row.Metadata = models.JSONMap{"result": "payload-private", "unknown": "payload-private"}
		if err := repo.Create(t.Context(), &row); err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&execution.Event{ExecutionID: row.ExecutionID, TenantID: row.TenantID, Module: row.Module, TaskType: row.TaskType,
			Attempt: 1, OccurredAt: time.Now().UTC(), Kind: "failed", Counters: models.JSONMap{}}).Error; err != nil {
			t.Fatal(err)
		}
	}
	query := service.NewExecutionQueryService(repo)
	query.SetReadResolver(service.NewExecutionReadAuthorizer(registry))
	router := SetupRouter(query, service.NewStatisticsService(repo), nil, nil, nil, nil, nil, nil, nil, system.URL, nil, nil, modulelifecycle.NewStandalone("monitor"), nil, nil, nil, nil, nil, nil, nil)
	get := func(path string, status int) []byte {
		t.Helper()
		beforeIdentity, beforeOwner := identityReads.Load(), ownerReads.Load()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/monitor/executions"+path, nil)
		req.Header.Set("Authorization", "Bearer execution-reader")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != status {
			t.Fatalf("%s: status=%d want=%d body=%s", path, response.Code, status, response.Body.String())
		}
		if identityReads.Load()-beforeIdentity < 2 || ownerReads.Load() <= beforeOwner {
			t.Fatalf("%s: Monitor did not obtain a freshly authenticated Owner decision", path)
		}
		for _, private := range []string{"peer-private", "missing-actor-private", "child-private", "tenant-private", "payload-private", "private-owner-details", "execution_config"} {
			if strings.Contains(response.Body.String(), private) {
				t.Fatalf("%s exposed %s", path, private)
			}
		}
		return response.Body.Bytes()
	}
	for _, denied := range []string{"peer-private", "missing-actor-private", "child-private", "tenant-private"} {
		get("/by-execution-id/"+denied, http.StatusNotFound)
		get("/by-execution-id/"+denied+"/events", http.StatusNotFound)
	}
	for _, isRevoked := range []bool{false, true, false} {
		revoked.Store(isRevoked)
		wantStatus, wantTotal := http.StatusOK, int64(2)
		if isRevoked {
			wantStatus, wantTotal = http.StatusNotFound, 0
		}
		for _, path := range []string{"/by-execution-id/root", "/by-execution-id/root/tree", "/by-execution-id/root/events", "/by-execution-id/mine"} {
			get(path, wantStatus)
		}
		for _, path := range []string{"", "/stats"} {
			var response struct {
				Total int64 `json:"total"`
			}
			if err := json.Unmarshal(get(path, http.StatusOK), &response); err != nil || response.Total != wantTotal {
				t.Fatalf("revoked=%t path=%s total=%d want=%d err=%v", isRevoked, path, response.Total, wantTotal, err)
			}
		}
		if !isRevoked {
			var page execution.EventPage
			if err := json.Unmarshal(get("/by-execution-id/root/events", http.StatusOK), &page); err != nil || len(page.Items) != 1 || page.Items[0].Kind != "failed" {
				t.Fatalf("authorized events=%+v err=%v", page, err)
			}
		}
	}
	unavailable.Store(true)
	for _, path := range []string{"", "/stats", "/by-execution-id/root", "/by-execution-id/root/tree", "/by-execution-id/root/events"} {
		get(path, http.StatusServiceUnavailable)
	}
	unavailable.Store(false)
	get("/by-execution-id/root", http.StatusOK)
}

func TestExecutionReadAPIRejectsHiddenChildrenRevocationAndUnavailableOwner(t *testing.T) {
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(monitorTenantAuthContext())
	}))
	defer system.Close()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	repo := execution.NewTaskExecutionRepository(db)
	task, root := "deleted-task", "root"
	for _, row := range []execution.TaskExecution{
		{ExecutionID: root, TenantID: 7, Module: "orchestrator", TaskType: "orchestration", SourceTaskID: &task, Status: "failed", TriggerType: "manual", ExecutionConfig: models.JSONMap{"secret": "payload-sentinel"}, ErrorDetails: models.JSONMap{"message": "password=payload-sentinel", "stack": "payload-sentinel"}},
		{ExecutionID: "hidden", TenantID: 7, Module: "transfer", TaskType: "sync", SourceTaskID: &task, ParentExecutionID: &root, Status: "success", TriggerType: "manual"},
		{ExecutionID: "other-tenant", TenantID: 8, Module: "orchestrator", TaskType: "orchestration", SourceTaskID: &task, Status: "success", TriggerType: "manual"},
	} {
		if err := repo.Create(context.Background(), &row); err != nil {
			t.Fatal(err)
		}
	}
	resolver := &requestScopeResolver{}
	query := service.NewExecutionQueryService(repo)
	query.SetReadResolver(resolver)
	router := SetupRouter(query, nil, nil, nil, nil, nil, nil, nil, nil, system.URL, nil, nil, modulelifecycle.NewStandalone("monitor"), nil, nil, nil, nil, nil, nil, nil)
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer addp_at_monitor")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	response := get("/api/v1/monitor/executions/by-execution-id/root/tree")
	if response.Code != 200 || strings.Contains(response.Body.String(), "hidden") || strings.Contains(response.Body.String(), "payload-sentinel") || strings.Contains(response.Body.String(), "execution_config") {
		t.Fatalf("unsafe tree status=%d body=%s", response.Code, response.Body.String())
	}
	if response = get("/api/v1/monitor/executions/by-execution-id/hidden"); response.Code != 404 {
		t.Fatalf("hidden status=%d", response.Code)
	}
	resolver.revoked = true
	if response = get("/api/v1/monitor/executions/by-execution-id/root"); response.Code != 404 {
		t.Fatalf("revoked status=%d", response.Code)
	}
	resolver.unavailable = true
	if response = get("/api/v1/monitor/executions"); response.Code != 503 {
		t.Fatalf("unavailable status=%d", response.Code)
	}
}
