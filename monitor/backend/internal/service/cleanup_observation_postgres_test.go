package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresCleanupObservationSnapshot(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(webhookIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err = execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	now, err := execution.DatabaseNow(db)
	if err != nil {
		t.Fatal(err)
	}
	tenant := int(750000000 + time.Now().UnixNano()%10000000)
	actor, member, version := int64(7), int64(17), int64(1)
	by := 7
	parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: tenant, Module: "system", TaskType: "cleanup", Source: "system", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", TriggeredBy: &by, ActorPrincipalID: &actor, ActorTenantMembershipID: &member, IssuedAuthorizationVersion: &version, MaxAttempts: 3, ExecutionConfig: models.JSONMap{execution.DisplayLanguageKey: "en"}}
	input := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parent.ExecutionID, TenantID: uint(tenant), RequestedBy: 7, Action: "scan", TriggerType: "manual", ExpectedModules: []string{"graph"}, RequestedAt: now, Deadline: now.Add(time.Minute)}
	if err = input.Seal(); err != nil {
		t.Fatal(err)
	}
	children, err := execution.CreateCleanupExecutions(t.Context(), db, &parent, input)
	if err != nil {
		t.Fatal(err)
	}
	child := children[0]
	t.Cleanup(func() {
		if e := db.Where("tenant_id = ?", tenant).Delete(&execution.TaskExecution{}).Error; e != nil {
			t.Error(e)
		}
		var remaining int64
		if e := db.Model(&execution.TaskExecution{}).Where("tenant_id = ?", tenant).Count(&remaining).Error; e != nil || remaining != 0 {
			t.Errorf("remaining=%d err=%v", remaining, e)
		}
	})
	identity := func(e execution.TaskExecution) execution.PendingIdentity {
		return execution.PendingIdentity{ExecutionID: e.ExecutionID, TenantID: tenant, Module: e.Module, TaskType: e.TaskType, Source: e.Source}
	}
	opts := func(e execution.TaskExecution) execution.ClaimOptions {
		return execution.ClaimOptions{Module: e.Module, TaskType: e.TaskType, Source: e.Source, WorkerID: "snapshot-fixture", LeaseDuration: time.Minute}
	}
	_, lease, err := execution.ClaimPending(t.Context(), db, identity(parent), opts(parent), nil)
	if err != nil {
		t.Fatal(err)
	}
	scope := func(parentAllowed bool) context.Context {
		grants := []execution.ReadGrant{}
		if parentAllowed {
			grants = append(grants, execution.ReadGrant{TaskType: "cleanup", OwnAdHoc: true})
		}
		return execution.WithReadScopes(t.Context(), []execution.ReadScope{{Module: "system", TenantID: tenant, PrincipalID: actor, Grants: grants}, {Module: "graph", TenantID: tenant, PrincipalID: actor, Grants: []execution.ReadGrant{{TaskType: "cleanup_executor", OwnAdHoc: true}}}})
	}
	query := NewExecutionQueryService(execution.NewTaskExecutionRepository(db))
	// Commit P after C was read. A separate connection can write immediately;
	// this response must still observe the pre-commit P from its read snapshot.
	var switched atomic.Bool
	callback := "cleanup_snapshot_" + uuid.NewString()
	if err = db.Callback().Query().After("gorm:query").Register(callback, func(q *gorm.DB) {
		row, ok := q.Statement.Dest.(*execution.TaskExecution)
		if !ok || row.ExecutionID != child.ExecutionID || !switched.CompareAndSwap(false, true) {
			return
		}
		if e := execution.CompleteWithLease(t.Context(), db, *lease, "timeout", nil); e != nil {
			q.AddError(e)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
	view, err := query.GetExecutionByExecutionID(scope(true), child.ExecutionID, tenant)
	if err != nil || !switched.Load() || view.CleanupAdmission != nil {
		t.Fatalf("mixed snapshot view=%+v err=%v", view, err)
	}
	if err = db.Callback().Query().Remove(callback); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(child)
	for _, read := range []func() (*ExecutionObservation, error){
		func() (*ExecutionObservation, error) {
			return query.GetExecutionByExecutionID(scope(true), child.ExecutionID, tenant)
		},
		func() (*ExecutionObservation, error) { return query.GetExecution(scope(true), child.ID, tenant) },
		func() (*ExecutionObservation, error) {
			v, e := query.GetExecutionTreeByExecutionID(scope(true), parent.ExecutionID, tenant)
			if e != nil {
				return nil, e
			}
			if len(v.Children) != 1 {
				t.Fatal("missing authorized child")
			}
			return v.Children[0].Execution, nil
		},
		func() (*ExecutionObservation, error) {
			v, e := query.ListExecutions(scope(true), &ListExecutionsRequest{TenantID: tenant, Module: "graph"})
			if e != nil {
				return nil, e
			}
			if len(v.Executions) != 1 {
				t.Fatal("missing authorized list child")
			}
			return v.Executions[0], nil
		},
	} {
		view, err = read()
		if err != nil || view.Status != "pending" || view.Attempt != 0 || view.StartedAt != nil || view.CleanupAdmission == nil || view.CleanupAdmission.State != "closed_unclaimed" {
			t.Fatalf("invalid closed pending view=%+v err=%v", view, err)
		}
	}
	if err = db.Where("execution_id = ?", child.ExecutionID).First(&child).Error; err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(child)
	if string(before) != string(after) {
		t.Fatal("GET changed child state")
	}
	view, err = query.GetExecutionByExecutionID(scope(false), child.ExecutionID, tenant)
	if err != nil || view.CleanupAdmission != nil {
		t.Fatal("hidden P exposed closure")
	}
	if _, _, err = execution.ClaimCleanupChild(t.Context(), db, identity(child), opts(child), nil); err != nil {
		t.Fatal(err)
	}
	view, err = query.GetExecutionByExecutionID(scope(false), child.ExecutionID, tenant)
	if err != nil || view.Status != "failed" || view.Attempt != 0 || view.StartedAt != nil || view.CleanupAdmission == nil || view.CleanupAdmission.State != "rejected_unstarted" || view.CleanupAdmission.Basis != "" {
		t.Fatalf("unstarted rejection=%+v err=%v", view, err)
	}
	missing := execution.WithReadScopes(t.Context(), []execution.ReadScope{{Module: "graph", TenantID: tenant, PrincipalID: actor, Grants: []execution.ReadGrant{{TaskType: "cleanup_executor", OwnAdHoc: true}}}})
	if _, err = query.GetExecutionTreeByExecutionID(missing, parent.ExecutionID, tenant); !errors.Is(err, commonclient.ErrExecutionOwnerUnavailable) {
		t.Fatalf("missing Owner decision accepted: %v", err)
	}
}
