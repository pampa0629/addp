package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization/authtest"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
	commonauth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Real PostgreSQL plus the Owner HTTP protocol; not a live IAM/Gateway/Tempo T4.
func TestIntegrationPostgresExecutionTraceOwnerRead(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(webhookIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	tenant := int(time.Now().UnixNano()%100000000) + 600000000
	task, parent, child, adhoc, foreign := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	actor, peer := 9, 10
	ids := []string{parent, child, adhoc, foreign}
	t.Cleanup(func() {
		if err := db.Where("execution_id IN ?", ids).Delete(&execution.TaskExecution{}).Error; err != nil {
			t.Error(err)
		}
		var n int64
		if err := db.Model(&execution.TaskExecution{}).Where("execution_id IN ?", ids).Count(&n).Error; err != nil || n != 0 {
			t.Errorf("fixture remains: %d %v", n, err)
		}
	})
	rows := []execution.TaskExecution{
		{ExecutionID: parent, TenantID: tenant, Module: "orchestrator", TaskType: "orchestration", SourceTaskID: &task},
		{ExecutionID: child, TenantID: tenant, Module: "develop", TaskType: "query", SourceTaskID: &task, ParentExecutionID: &parent},
		{ExecutionID: adhoc, TenantID: tenant, Module: "orchestrator", TaskType: "orchestration", TriggeredBy: &peer},
		{ExecutionID: foreign, TenantID: tenant + 1, Module: "orchestrator", TaskType: "orchestration", SourceTaskID: &task},
	}
	for i := range rows {
		row := &rows[i]
		row.Status = "failed"
		row.Source = row.Module
		row.TriggerType = "manual"
		row.ExecutionBoundary = execution.ExecutionBoundaryBounded
		row.ExecutionConfig = models.JSONMap{"secret": "private-config"}
		row.Metadata = models.JSONMap{"result": "private-result"}
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	var revoked, unavailable atomic.Bool
	var ownerCalls, identityCalls atomic.Int64
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer exact-user" {
			w.WriteHeader(401)
			return
		}
		identityCalls.Add(1)
		permissions := []string{"monitor.execution.read"}
		if !revoked.Load() {
			permissions = append(permissions, "orchestrator.workflow.read")
		}
		_ = json.NewEncoder(w).Encode(authtest.NewTenantUserAuthContext(strconv.Itoa(tenant), "9", permissions))
	}))
	defer system.Close()
	router := gin.New()
	router.Use(commonauth.MustNewMiddleware(commonauth.MiddlewareConfig{SystemURL: system.URL}), commonauth.MustNewContextGuard("tenant"))
	router.GET("/api/v1/:module/execution-read-scope", func(c *gin.Context) {
		ownerCalls.Add(1)
		if unavailable.Load() {
			c.Status(503)
			return
		}
		module := c.Param("module")
		permissions := map[string]string{"orchestration": "orchestrator.workflow.read"}
		if module == "develop" {
			permissions = map[string]string{"query": "develop.query.read"}
		}
		execution.OwnerReadScopeHandler(module, permissions)(c)
	})
	owner := httptest.NewServer(router)
	defer owner.Close()
	modules := []*commonclient.ModuleInfo{}
	for _, module := range []string{"orchestrator", "develop", "transfer"} {
		url := owner.URL
		if module == "transfer" {
			url = "http://127.0.0.1:1"
		} // unrelated unavailable Owner must not be contacted
		modules = append(modules, &commonclient.ModuleInfo{ModuleName: module, Enabled: true, Instances: []commonclient.ModuleRuntimeInstanceInfo{{Role: "backend", Status: "up", ModuleURL: url, LeaseExpiresAt: time.Now().Add(time.Hour)}}})
	}
	query := NewExecutionQueryService(execution.NewTaskExecutionRepository(db))
	resolver := commonclient.NewExecutionReadAuthorizer(executionTestModuleLister{modules})
	query.SetReadResolver(resolver)
	read := func(id string, want error, contact bool) {
		t.Helper()
		before := ownerCalls.Load()
		authBefore := identityCalls.Load()
		row, err := query.ReadExecutionForTrace(t.Context(), id, tenant, int64(actor), "Bearer exact-user")
		if !errors.Is(err, want) {
			t.Fatalf("read %s: %v want %v", id, err, want)
		}
		if want == nil && (row == nil || row.ExecutionID != id) {
			t.Fatal("wrong execution", row)
		}
		if want != nil && row != nil {
			t.Fatal("failed read returned private row")
		}
		expected := int64(0)
		if contact {
			expected = 1
		}
		if ownerCalls.Load()-before != expected || identityCalls.Load()-authBefore != expected {
			t.Fatal("did not reauthenticate exactly one Owner", ownerCalls.Load()-before, identityCalls.Load()-authBefore)
		}
	}
	var before execution.TaskExecution
	if err := db.Where("execution_id = ?", parent).Take(&before).Error; err != nil {
		t.Fatal(err)
	}
	read(parent, nil, true)
	read(child, commonapi.ErrNotFound, true) // parent visibility never grants its child
	read(adhoc, commonapi.ErrNotFound, true) // peer ad hoc execution remains private
	read(foreign, commonapi.ErrNotFound, false)
	read(uuid.NewString(), commonapi.ErrNotFound, false)
	revoked.Store(true)
	read(parent, commonapi.ErrNotFound, true)
	revoked.Store(false)
	read(parent, nil, true)
	unavailable.Store(true)
	read(parent, commonclient.ErrExecutionOwnerUnavailable, true)
	unavailable.Store(false)
	var after execution.TaskExecution
	if err := db.Where("execution_id = ?", parent).Take(&after).Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("read mutated execution", err)
	}
	query.SetReadResolver(traceReadHookResolver{resolve: func(ctx context.Context, owners []string, tenant int, principal int64, bearer string) ([]execution.ReadScope, error) {
		scopes, err := resolver.Resolve(ctx, owners, tenant, principal, bearer)
		if err == nil {
			err = db.Model(&execution.TaskExecution{}).Where("execution_id = ?", parent).Update("module", "develop").Error
		}
		return scopes, err
	}})
	read(parent, commonapi.ErrNotFound, true) // authoritative snapshot rejects identity drift after Owner decision
}

type traceReadHookResolver struct {
	resolve func(context.Context, []string, int, int64, string) ([]execution.ReadScope, error)
}

func (r traceReadHookResolver) Resolve(ctx context.Context, o []string, t int, p int64, b string) ([]execution.ReadScope, error) {
	return r.resolve(ctx, o, t, p, b)
}
