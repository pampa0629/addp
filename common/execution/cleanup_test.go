package execution

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/events"
	"github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func newCleanupProtocolDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newTaskExecutionRepositoryTestDB(t)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec("CREATE UNIQUE INDEX common.uq_cleanup_parent_owner ON task_executions(parent_execution_id,module) WHERE task_type='cleanup_executor' AND parent_execution_id IS NOT NULL").Error; err != nil {
		t.Fatal(err)
	}
	return db
}
func cleanupProtocolFixture(t *testing.T, db *gorm.DB, budgets ...time.Duration) (TaskExecution, TaskExecution, events.CleanupRequest) {
	t.Helper()
	now, err := DatabaseNow(db)
	if err != nil {
		t.Fatal(err)
	}
	tenant := int(940000000 + time.Now().UnixNano()%10000000)
	actor, member, version := int64(7), int64(17), int64(1)
	by := 7
	parent := TaskExecution{ExecutionID: uuid.NewString(), TenantID: tenant, Module: ModuleSystem, TaskType: TaskTypeCleanup, Source: ModuleSystem, Status: ExecutionStatusPending, ExecutionBoundary: ExecutionBoundaryBounded, TriggerType: TriggerTypeManual, TriggeredBy: &by, ActorPrincipalID: &actor, ActorTenantMembershipID: &member, IssuedAuthorizationVersion: &version, MaxAttempts: 3, ExecutionConfig: models.JSONMap{DisplayLanguageKey: "en"}}
	budget := time.Minute
	if len(budgets) > 0 {
		budget = budgets[0]
	}
	request := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parent.ExecutionID, TenantID: uint(tenant), RequestedBy: 7, Action: events.CleanupActionScan, TriggerType: TriggerTypeManual, ExpectedModules: []string{ModuleGraph}, RequestedAt: now, Deadline: now.Add(budget)}
	if err = request.Seal(); err != nil {
		t.Fatal(err)
	}
	children, err := CreateCleanupExecutions(t.Context(), db, &parent, request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Where("tenant_id = ?", tenant).Delete(&TaskExecution{}).Error; err != nil {
			t.Error(err)
		}
	})
	return parent, children[0], request
}
func cleanupIdentity(e TaskExecution) PendingIdentity {
	return PendingIdentity{ExecutionID: e.ExecutionID, TenantID: e.TenantID, Module: e.Module, TaskType: e.TaskType, Source: e.Source}
}
func cleanupOptions(e TaskExecution) ClaimOptions {
	return ClaimOptions{Module: e.Module, TaskType: e.TaskType, Source: e.Source, WorkerID: "cleanup-test", LeaseDuration: time.Minute}
}
func readCleanupExecution(t *testing.T, db *gorm.DB, e TaskExecution) TaskExecution {
	t.Helper()
	var out TaskExecution
	if err := db.Where("execution_id = ?", e.ExecutionID).First(&out).Error; err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCleanupProtocolLifecycle(t *testing.T) {
	testCleanupProtocolLifecycle(t, newCleanupProtocolDB(t))
}
func testCleanupProtocolLifecycle(t *testing.T, db *gorm.DB) {
	t.Run("unleased_parent_waits_closed_parent_rejects_without_runtime", func(t *testing.T) {
		p, c, _ := cleanupProtocolFixture(t, db)
		item, lease, err := ClaimCleanupChild(t.Context(), db, cleanupIdentity(c), cleanupOptions(c), nil)
		if err != nil || item != nil || lease != nil {
			t.Fatalf("pending parent did not wait: %v", err)
		}
		_, pl, err := ClaimPending(t.Context(), db, cleanupIdentity(p), cleanupOptions(p), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = CompleteWithLease(t.Context(), db, *pl, ExecutionStatusTimeout, nil); err != nil {
			t.Fatal(err)
		}
		p = readCleanupExecution(t, db, p)
		now, _ := DatabaseNow(db)
		view := ProjectCleanupAdmission(c, &p, now)
		if view == nil || view.State != "closed_unclaimed" {
			t.Fatalf("closed pending view=%+v", view)
		}
		item, lease, err = ClaimCleanupChild(t.Context(), db, cleanupIdentity(c), cleanupOptions(c), nil)
		if err != nil || item == nil || lease != nil || item.Status != ExecutionStatusFailed || item.Attempt != 0 || item.StartedAt != nil || item.ExecutionTimeMs != nil {
			t.Fatalf("rejection=%+v %+v %v", item, lease, err)
		}
		receipt, err := ReadCleanupReceipt(*item)
		if err != nil || receipt != nil {
			t.Fatal("rejection manufactured a receipt")
		}
		view = ProjectCleanupAdmission(*item, &p, now)
		if view == nil || view.State != "closed_rejected" {
			t.Fatalf("rejected view=%+v", view)
		}
		view = ProjectCleanupAdmission(*item, nil, now)
		if view == nil || view.State != "rejected_unstarted" || view.Basis != "" {
			t.Fatalf("hidden parent leaked=%+v", view)
		}
	})
	t.Run("deadline_alone_does_not_prove_unclaimed", func(t *testing.T) {
		p, c, request := cleanupProtocolFixture(t, db)
		if view := ProjectCleanupAdmission(c, &p, request.Deadline.Add(time.Second)); view != nil {
			t.Fatalf("MVCC deadline invented closure: %+v", view)
		}
		c.Attempt = 1
		if view := ProjectCleanupAdmission(c, &p, request.Deadline.Add(time.Second)); view != nil {
			t.Fatal("prior attempt called unstarted")
		}
	})
	t.Run("claimed_child_can_finish_after_parent_with_atomic_receipt", func(t *testing.T) {
		p, c, input := cleanupProtocolFixture(t, db)
		_, pl, err := ClaimPending(t.Context(), db, cleanupIdentity(p), cleanupOptions(p), nil)
		if err != nil {
			t.Fatal(err)
		}
		claimed, cl, err := ClaimCleanupChild(t.Context(), db, cleanupIdentity(c), cleanupOptions(c), nil)
		if err != nil || cl == nil {
			t.Fatalf("child claim: %v", err)
		}
		if err = CompleteWithLease(t.Context(), db, *pl, ExecutionStatusTimeout, nil); err != nil {
			t.Fatal(err)
		}
		impact, err := events.BuildCleanupImpactData(nil, "")
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := NewCleanupReceipt(*claimed, events.CleanupResultData{Module: c.Module, TenantID: uint(c.TenantID), TaskID: input.RequestID, Action: input.Action, Status: events.CleanupResultSuccess, Summary: events.CleanupResultSummary{ScannedItems: 3, RiskLevel: "low"}, Impact: &impact}, "")
		if err != nil {
			t.Fatal(err)
		}
		rollback := errors.New("owner result failed")
		err = db.Transaction(func(tx *gorm.DB) error {
			if err := CompleteCleanupWithReceipt(t.Context(), tx, *cl, receipt); err != nil {
				return err
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal(err)
		}
		unchanged := readCleanupExecution(t, db, c)
		if unchanged.Status != ExecutionStatusRunning || unchanged.Metadata["cleanup_receipt"] != nil {
			t.Fatal("failed Owner transaction partially committed")
		}
		if err = CompleteCleanupWithReceipt(t.Context(), db, *cl, receipt); err != nil {
			t.Fatal(err)
		}
		c = readCleanupExecution(t, db, c)
		read, err := ReadCleanupReceipt(c)
		if err != nil || read == nil || read.Summary.ScannedItems != 3 {
			t.Fatalf("receipt: %+v %v", read, err)
		}
		if err = CompleteCleanupWithReceipt(t.Context(), db, *cl, receipt); err == nil {
			t.Fatal("terminal receipt rewritten")
		}
		p = readCleanupExecution(t, db, p)
		if p.Status != ExecutionStatusTimeout {
			t.Fatal("late child revived parent")
		}
		now, _ := DatabaseNow(db)
		if ProjectCleanupAdmission(c, &p, now) != nil {
			t.Fatal("started child labeled unstarted")
		}
	})
	t.Run("strict_binding_and_unique_owner", func(t *testing.T) {
		p, c, input := cleanupProtocolFixture(t, db)
		duplicate := c
		duplicate.ID = 0
		duplicate.ExecutionID = uuid.NewString()
		if err := db.Create(&duplicate).Error; err == nil {
			t.Fatal("duplicate owner child admitted")
		}
		raw := c.ExecutionConfig[cleanupInputKey].(string)
		c.ExecutionConfig[cleanupInputKey] = strings.Replace(raw, `"schema":`, `"unknown":true,"schema":`, 1)
		if _, err := CleanupRequestFromExecution(c); err == nil {
			t.Fatal("unknown request accepted")
		}
		c = readCleanupExecution(t, db, c)
		p.TenantID++
		if ProjectCleanupAdmission(c, &p, input.Deadline) != nil {
			t.Fatal("cross tenant projection")
		}
	})
}
