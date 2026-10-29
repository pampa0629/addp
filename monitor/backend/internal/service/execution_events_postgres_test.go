package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type executionTestModuleLister struct{ modules []*commonclient.ModuleInfo }

func (l executionTestModuleLister) ListActiveModules(context.Context) ([]*commonclient.ModuleInfo, error) {
	return l.modules, nil
}

// This is a Monitor query/Owner-protocol T2 test, not a real IAM/Gateway T4.
func TestIntegrationPostgresExecutionEventVisibility(t *testing.T) {
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
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	ids := make([]string, 7)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	t.Cleanup(func() {
		if err := tx.Rollback().Error; err != nil {
			t.Error(err)
		}
		for _, table := range []interface{}{&execution.Event{}, &execution.TaskExecution{}} {
			var remaining int64
			if err := db.Model(table).Where("execution_id IN ?", ids).Count(&remaining).Error; err != nil || remaining != 0 {
				t.Errorf("execution event fixture remains: count=%d err=%v", remaining, err)
			}
		}
	})
	tenant := int(time.Now().UnixNano()%100000000) + 600000000
	actor, peer := 9, 10
	task := uuid.NewString() // Historical identity only; no current task definition exists.
	now := time.Now().UTC()
	rows := []execution.TaskExecution{
		{ExecutionID: ids[0], SourceTaskID: &task},
		{ExecutionID: ids[1], TriggeredBy: &actor},
		{ExecutionID: ids[2], TriggeredBy: &peer},
		{ExecutionID: ids[3]}, // No initiating principal: never infer ownership.
		{ExecutionID: ids[4], Module: "transfer", TaskType: "sync", SourceTaskID: &task, ParentExecutionID: &ids[0]},
		{ExecutionID: ids[5], TaskType: "cleanup_executor", SourceTaskID: &task},
		{ExecutionID: ids[6], TenantID: tenant + 1, SourceTaskID: &task},
	}
	for i := range rows {
		row := &rows[i]
		if row.TenantID == 0 {
			row.TenantID = tenant
		}
		if row.Module == "" {
			row.Module = "orchestrator"
		}
		if row.TaskType == "" {
			row.TaskType = "orchestration"
		}
		row.Source, row.Status, row.TriggerType = row.Module, "failed", "manual"
		row.CreatedAt, row.CompletedAt = now.Add(-time.Duration(i)*time.Second), &now
		row.ExecutionConfig = models.JSONMap{"parameters": "private-event-context"}
		row.Metadata = models.JSONMap{"result": "private-event-context", "authorization": "private-event-context"}
		row.ErrorDetails = models.JSONMap{"message": "private-event-context", "error_code": "connection_failed"}
		if err := tx.Create(row).Error; err != nil {
			t.Fatal(err)
		}
		size := 1
		if i == 0 {
			size = 205
		}
		for n := range size {
			if err := tx.Create(&execution.Event{ExecutionID: row.ExecutionID, TenantID: row.TenantID, Module: row.Module, TaskType: row.TaskType,
				Attempt: 1, OccurredAt: now.Add(-time.Minute), Kind: "progress", Counters: models.JSONMap{"items_scanned": n}}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Create(&execution.Event{ExecutionID: ids[0], TenantID: tenant, Module: "orchestrator", TaskType: "orchestration",
		Attempt: 1, OccurredAt: now.Add(-execution.EventRetention - time.Hour), Kind: "started", Counters: models.JSONMap{}}).Error; err != nil {
		t.Fatal(err)
	}

	var revoked, unavailable, wrongPrincipal atomic.Bool
	var ownerReads atomic.Int64
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ownerReads.Add(1)
		if unavailable.Load() {
			http.Error(w, "private-event-context", http.StatusServiceUnavailable)
			return
		}
		principal := int64(actor)
		switch r.Header.Get("Authorization") {
		case "Bearer event-reader":
		case "Bearer peer-reader":
			principal = int64(peer)
		default:
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if wrongPrincipal.Load() {
			principal++
		}
		module := strings.Split(r.URL.Path, "/")[3]
		scope := execution.ReadScope{Module: module, TenantID: tenant, PrincipalID: principal}
		if module == "orchestrator" && !revoked.Load() {
			scope.Grants = []execution.ReadGrant{{TaskType: "orchestration", TaskHistory: true, OwnAdHoc: true}}
		}
		_ = json.NewEncoder(w).Encode(scope)
	}))
	t.Cleanup(owner.Close)
	registry := executionTestModuleLister{}
	for _, module := range []string{"orchestrator", "transfer"} {
		registry.modules = append(registry.modules, &commonclient.ModuleInfo{ModuleName: module, Enabled: true,
			Instances: []commonclient.ModuleRuntimeInstanceInfo{{Role: "backend", Status: "up", ModuleURL: owner.URL, LeaseExpiresAt: now.Add(time.Hour)}}})
	}
	repo := execution.NewTaskExecutionRepository(tx)
	query := NewExecutionQueryService(repo)
	query.SetReadResolver(commonclient.NewExecutionReadAuthorizer(registry))
	statistics := NewStatisticsService(repo)
	authorize := func(t *testing.T, principal int64, bearer string) context.Context {
		t.Helper()
		before := ownerReads.Load()
		ctx, err := query.AuthorizeReadRequest(t.Context(), tenant, principal, bearer, "")
		if err != nil || ctx == nil || ownerReads.Load() <= before {
			t.Fatal("request did not obtain fresh Owner scopes", err)
		}
		return ctx
	}
	assertSafe := func(t *testing.T, value interface{}) {
		t.Helper()
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, private := range []string{"private-event-context", "execution_config", "authorization", "lease_token"} {
			if strings.Contains(string(body), private) {
				t.Fatal("Monitor query exposed private context", private)
			}
		}
	}

	t.Run("owner_scoped_counts_and_historical_ad_hoc_visibility", func(t *testing.T) {
		for _, user := range []struct {
			principal int64
			bearer    string
			own       string
		}{{9, "Bearer event-reader", ids[1]}, {10, "Bearer peer-reader", ids[2]}} {
			ctx := authorize(t, user.principal, user.bearer)
			page, err := query.ListExecutions(ctx, &ListExecutionsRequest{TenantID: tenant, PageSize: 1})
			if err != nil || page.Total != 2 || len(page.Executions) != 1 {
				t.Fatal("hidden executions leaked through pagination or total", err)
			}
			next, err := query.ListExecutions(ctx, &ListExecutionsRequest{TenantID: tenant, Page: 2, PageSize: 1})
			if err != nil || next.Total != 2 || len(next.Executions) != 1 || page.Executions[0].ExecutionID != ids[0] || next.Executions[0].ExecutionID != user.own {
				t.Fatal("visible execution pages contain another initiator or task type", err)
			}
			stats, err := statistics.GetStatistics(ctx, &StatisticsRequest{TenantID: tenant})
			if err != nil || stats.Total != 2 || stats.FailedCount != 2 {
				t.Fatal("hidden executions leaked through statistics", err)
			}
			own, err := query.GetExecutionEvents(ctx, user.own, tenant, 0, 100)
			if err != nil || len(own.Items) != 1 {
				t.Fatal("initiator cannot read own ad-hoc events", err)
			}
			assertSafe(t, page)
			assertSafe(t, next)
			assertSafe(t, own)
		}
	})
	t.Run("event_cursor_preserves_order_retention_and_safe_projection", func(t *testing.T) {
		var cursor int64
		for i, size := range []int{100, 100, 5} {
			ctx := authorize(t, 9, "Bearer event-reader")
			page, err := query.GetExecutionEvents(ctx, ids[0], tenant, cursor, 100)
			if err != nil || len(page.Items) != size || page.HasMore != (i < 2) || page.RetainedAfter.Before(now.Add(-execution.EventRetention)) {
				t.Fatal("event pagination or retention window is incorrect", err)
			}
			for _, event := range page.Items {
				if event.ID <= cursor || event.ExecutionID != ids[0] || event.Kind != "progress" {
					t.Fatal("cursor duplicated, crossed execution or returned an expired event")
				}
				cursor = event.ID
			}
			if page.NextCursor != cursor {
				t.Fatal("event cursor is not the last visible ID")
			}
			assertSafe(t, page)
		}
		ctx := authorize(t, 9, "Bearer event-reader")
		page, err := query.GetExecutionEvents(ctx, ids[0], tenant, cursor, 100)
		if err != nil || len(page.Items) != 0 || page.HasMore {
			t.Fatal("end cursor did not yield an empty page", err)
		}
		for _, hidden := range []string{ids[2], ids[3], ids[4], ids[5], ids[6], uuid.NewString()} {
			if page, err := query.GetExecutionEvents(ctx, hidden, tenant, cursor, 100); page != nil || !errors.Is(err, commonapi.ErrNotFound) {
				t.Fatal("a cursor exposed an inaccessible execution", err)
			}
		}
	})
	t.Run("readable_parent_does_not_grant_child_events", func(t *testing.T) {
		ctx := authorize(t, 9, "Bearer event-reader")
		tree, err := query.GetExecutionTreeByExecutionID(ctx, ids[0], tenant)
		if err != nil || len(tree.Children) != 0 || tree.Truncated {
			t.Fatal("readable parent exposed a hidden child or truncation hint", err)
		}
		assertSafe(t, tree)
	})
	t.Run("revocation_between_pages_returns_no_events_or_counts", func(t *testing.T) {
		page, err := query.GetExecutionEvents(authorize(t, 9, "Bearer event-reader"), ids[0], tenant, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		revoked.Store(true)
		defer revoked.Store(false)
		ctx := authorize(t, 9, "Bearer event-reader")
		if denied, err := query.GetExecutionEvents(ctx, ids[0], tenant, page.NextCursor, 100); denied != nil || !errors.Is(err, commonapi.ErrNotFound) {
			t.Fatal("revoked scope continued a previously authorized cursor", err)
		}
		list, err := query.ListExecutions(ctx, &ListExecutionsRequest{TenantID: tenant})
		if err != nil || list.Total != 0 || len(list.Executions) != 0 {
			t.Fatal("revoked scope retained a visible count", err)
		}
		stats, err := statistics.GetStatistics(ctx, &StatisticsRequest{TenantID: tenant})
		if err != nil || stats.Total != 0 || stats.FailedCount != 0 {
			t.Fatal("revoked scope retained visible statistics", err)
		}
	})
	t.Run("owner_failure_or_wrong_subject_cannot_reuse_prior_scope", func(t *testing.T) {
		for _, mode := range []*atomic.Bool{&unavailable, &wrongPrincipal} {
			mode.Store(true)
			ctx, err := query.AuthorizeReadRequest(t.Context(), tenant, 9, "Bearer event-reader", "")
			mode.Store(false)
			if ctx != nil || !errors.Is(err, commonclient.ErrExecutionOwnerUnavailable) || strings.Contains(err.Error(), "private-event-context") {
				t.Fatal("invalid Owner response did not fail closed", err)
			}
		}
		page, err := query.GetExecutionEvents(authorize(t, 9, "Bearer event-reader"), ids[0], tenant, 0, 100)
		if err != nil || len(page.Items) != 100 {
			t.Fatal("fresh authorization could not recover after Owner failure", err)
		}
	})
}
