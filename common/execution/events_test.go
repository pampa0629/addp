package execution_test

import (
	"context"
	"errors"
	"fmt"
	commonModels "github.com/addp/common/models"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"gorm.io/gorm"
)

func TestExpiredFailureEventFencesIdentityAndRollsBack(t *testing.T) {
	db := newLeaseTestDB(t)
	now := time.Now().UTC()
	item := execution.TaskExecution{TenantID: 7, ExecutionID: "expired-event", Module: "meta", TaskType: "scan", Source: "meta", Status: "pending", TriggerType: "manual", ExecutionBoundary: "bounded", Progress: 50}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	_, lease, err := execution.ClaimNext(t.Context(), db, execution.ClaimOptions{Module: "meta", TaskType: "scan", WorkerID: "owner", Now: now, LeaseDuration: time.Minute})
	if err != nil || lease == nil {
		t.Fatalf("claim=%v err=%v", lease, err)
	}
	if err := execution.FailExpiredWithEvent(t.Context(), db, *lease, now, nil); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("active recovery=%v", err)
	}
	if err := db.Model(&item).Update("lease_expires_at", now.Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*execution.Lease){
		func(l *execution.Lease) { l.Token = "wrong" }, func(l *execution.Lease) { l.Attempt++ },
		func(l *execution.Lease) { l.Owner = "wrong" }, func(l *execution.Lease) { l.TenantID++ },
	} {
		wrong := *lease
		mutate(&wrong)
		if err := execution.FailExpiredWithEvent(t.Context(), db, wrong, now, nil); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("wrong recovery identity=%v", err)
		}
	}
	if err := db.Callback().Create().Before("gorm:create").Register("test:reject-recovery-event", func(tx *gorm.DB) {
		if _, isEvent := tx.Statement.Dest.(*execution.Event); isEvent {
			tx.AddError(errors.New("event storage unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Create().Remove("test:reject-recovery-event") })
	if err := execution.FailExpiredWithEvent(t.Context(), db, *lease, now, nil); err == nil {
		t.Fatal("recovery ignored failed event storage")
	}
	var stored execution.TaskExecution
	var count int64
	if err := db.First(&stored, item.ID).Error; err != nil || stored.Status != "running" || stored.Progress != 50 {
		t.Fatalf("state did not roll back: status=%s progress=%d err=%v", stored.Status, stored.Progress, err)
	}
	if err := db.Model(&execution.Event{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("event did not roll back: count=%d err=%v", count, err)
	}
	if err := db.Callback().Create().Remove("test:reject-recovery-event"); err != nil {
		t.Fatal(err)
	}
	if err := execution.FailExpiredWithEvent(t.Context(), db, *lease, now, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&stored, item.ID).Error; err != nil || stored.Status != "failed" || stored.Progress != 50 {
		t.Fatalf("failed state=%s progress=%d err=%v", stored.Status, stored.Progress, err)
	}
	if err := execution.FailExpiredWithEvent(t.Context(), db, *lease, now, nil); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("duplicate recovery=%v", err)
	}
	if err := execution.AppendBoundedEvent(t.Context(), db, *lease, execution.EventInput{Kind: "failed"}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("expired worker append=%v", err)
	}
	var event execution.Event
	if err := db.First(&event).Error; err != nil || event.Kind != "failed" || event.Attempt != lease.Attempt || event.TenantID != 7 {
		t.Fatalf("recovery event=%+v err=%v", event, err)
	}
	if err := db.Model(&execution.Event{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("terminal count=%d err=%v", count, err)
	}
}

func TestExpiredFailureEventRespectsSaturatedBudget(t *testing.T) {
	for _, initial := range []int{execution.MaxDailyAttemptEvents - 1, execution.MaxDailyAttemptEvents} {
		t.Run(fmt.Sprint(initial), func(t *testing.T) {
			db := newLeaseTestDB(t)
			now := time.Now().UTC()
			item := execution.TaskExecution{TenantID: 7, ExecutionID: "expired-budget", Module: "meta", TaskType: "scan", Source: "meta", Status: "pending", TriggerType: "manual", ExecutionBoundary: "bounded"}
			if err := db.Create(&item).Error; err != nil {
				t.Fatal(err)
			}
			_, lease, err := execution.ClaimNext(t.Context(), db, execution.ClaimOptions{Module: "meta", TaskType: "scan", WorkerID: "owner", Now: now, LeaseDuration: time.Minute})
			if err != nil || lease == nil {
				t.Fatalf("claim=%v err=%v", lease, err)
			}
			seed := make([]execution.Event, initial)
			for i := range seed {
				seed[i] = execution.Event{ExecutionID: item.ExecutionID, TenantID: 7, Module: "meta", TaskType: "scan", Attempt: lease.Attempt, OccurredAt: now, Kind: "progress", Counters: commonModels.JSONMap{}}
			}
			if initial == execution.MaxDailyAttemptEvents {
				seed[len(seed)-1].Kind = "truncated"
			}
			if err := db.CreateInBatches(seed, 100).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&item).Update("lease_expires_at", now.Add(-time.Second)).Error; err != nil {
				t.Fatal(err)
			}
			if err := execution.FailExpiredWithEvent(t.Context(), db, *lease, now, nil); err != nil {
				t.Fatal(err)
			}
			var count int64
			var last execution.Event
			if err := db.Model(&execution.Event{}).Count(&count).Error; err != nil || count != execution.MaxDailyAttemptEvents {
				t.Fatalf("recovery exceeded cap: count=%d err=%v", count, err)
			}
			if err := db.Order("id DESC").First(&last).Error; err != nil || last.Kind != "truncated" {
				t.Fatalf("missing budget truncation: kind=%s err=%v", last.Kind, err)
			}
			if err := db.First(&item, item.ID).Error; err != nil || item.Status != "failed" {
				t.Fatalf("saturation blocked recovery: status=%s err=%v", item.Status, err)
			}
		})
	}
}

func TestEventsRequireCurrentLeaseAndNeverCarryPrivatePayload(t *testing.T) {
	db := newLeaseTestDB(t)
	now := time.Now().UTC()
	item := execution.TaskExecution{TenantID: 7, ExecutionID: "event-lease", Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, Source: execution.ModuleMeta, Status: execution.ExecutionStatusPending, TriggerType: execution.TriggerTypeManual, ExecutionBoundary: execution.ExecutionBoundaryBounded}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	_, lease, err := execution.ClaimNext(t.Context(), db, execution.ClaimOptions{Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, WorkerID: "owner", Now: now, LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	input := execution.EventInput{Kind: "progress", Counters: map[string]int64{"items_scanned": 12}}
	for _, mutate := range []func(*execution.Lease){
		func(l *execution.Lease) { l.Token = "old-token" },
		func(l *execution.Lease) { l.Attempt++ },
		func(l *execution.Lease) { l.TenantID++ },
		func(l *execution.Lease) { l.Owner = "wrong-owner" },
	} {
		wrong := *lease
		mutate(&wrong)
		if err := execution.AppendBoundedEvent(t.Context(), db, wrong, input); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("wrong lease error=%v", err)
		}
	}
	for _, invalid := range []execution.EventInput{
		{Kind: "private SQL"}, {Kind: "progress", StepID: "invented-step"}, {Kind: "progress", StepID: "password=private"},
		{Kind: "progress", Counters: map[string]int64{"source_offset": 2}},
		{Kind: "progress", Counters: map[string]int64{"items_scanned": -1}},
	} {
		if err := execution.AppendBoundedEvent(t.Context(), db, *lease, invalid); err == nil {
			t.Fatal("accepted invalid event")
		}
	}
	if err := execution.UpdateWithEvent(t.Context(), db, *lease, map[string]interface{}{"status": "failed"}, input); err == nil {
		t.Fatal("accepted protected update")
	}
	var count int64
	if err := db.Model(&execution.Event{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rolled back event count=%d err=%v", count, err)
	}
	if err := execution.AppendBoundedEvent(t.Context(), db, *lease, input); err != nil {
		t.Fatal(err)
	}
	page, err := execution.NewTaskExecutionRepository(db).ListEvents(t.Context(), item.ExecutionID, 7, 0, 100)
	if err != nil || len(page.Items) != 1 || page.Items[0].TenantID != 7 || page.Items[0].Attempt != 1 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	if err := execution.CompleteWithEvent(t.Context(), db, *lease, execution.ExecutionStatusSuccess, now, nil); err != nil {
		t.Fatal(err)
	}
	if err := execution.AppendBoundedEvent(t.Context(), db, *lease, input); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("terminal write err=%v", err)
	}
}

func TestEventReadsApplyOwnerScopeBeforePagination(t *testing.T) {
	db := newLeaseTestDB(t)
	item := execution.TaskExecution{TenantID: 7, ExecutionID: "event-read", Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, Source: execution.ModuleMeta, Status: "success", TriggerType: "manual", SourceTaskID: execution.NewSourceTaskIDFromUint(42)}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := db.Create(&execution.Event{ExecutionID: item.ExecutionID, TenantID: 7, Module: "meta", TaskType: "scan", Attempt: 1, OccurredAt: time.Now().UTC(), Kind: "progress", Counters: map[string]interface{}{}}).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := execution.NewTaskExecutionRepository(db)
	deny := execution.WithReadScopes(context.Background(), []execution.ReadScope{})
	if _, err := repo.ListEvents(deny, item.ExecutionID, 7, 0, 2); err == nil {
		t.Fatal("unscoped event leak")
	}
	if _, err := repo.ListEvents(t.Context(), item.ExecutionID, 8, 0, 2); err == nil {
		t.Fatal("cross-tenant event leak")
	}
	ctx := execution.WithReadScopes(t.Context(), []execution.ReadScope{{Module: "meta", TenantID: 7, PrincipalID: 9, Grants: []execution.ReadGrant{{TaskType: "scan", TaskHistory: true}}}})
	page, err := repo.ListEvents(ctx, item.ExecutionID, 7, 0, 2)
	if err != nil || len(page.Items) != 2 || !page.HasMore {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	next, err := repo.ListEvents(ctx, item.ExecutionID, 7, page.NextCursor, 2)
	if err != nil || len(next.Items) != 1 || next.HasMore || next.Items[0].ID <= page.NextCursor {
		t.Fatalf("next=%#v err=%v", next, err)
	}
}

func TestEventBudgetAndRetentionKeepExecutionHistory(t *testing.T) {
	db := newLeaseTestDB(t)
	now := time.Now().UTC()
	item := execution.TaskExecution{TenantID: 7, ExecutionID: "event-budget", Module: "meta", TaskType: "scan", Source: "meta", Status: "pending", TriggerType: "manual", ExecutionBoundary: "bounded"}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	_, lease, err := execution.ClaimNext(t.Context(), db, execution.ClaimOptions{Module: "meta", TaskType: "scan", WorkerID: "owner", Now: now, LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < execution.MaxDailyAttemptEvents+3; i++ {
		if err := execution.AppendBoundedEvent(t.Context(), db, *lease, execution.EventInput{Kind: "progress"}); err != nil {
			t.Fatal(err)
		}
	}
	var last execution.Event
	if err := db.Order("id DESC").First(&last).Error; err != nil || last.Kind != "truncated" {
		t.Fatalf("last=%#v err=%v", last, err)
	}
	var count int64
	db.Model(&execution.Event{}).Count(&count)
	if count != execution.MaxDailyAttemptEvents {
		t.Fatalf("count=%d", count)
	}
	if err := db.Model(&execution.Event{}).Where("id = ?", last.ID).Update("occurred_at", now.Add(-execution.EventRetention-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	result, err := execution.PruneEvents(t.Context(), db, now, 1000)
	if err != nil || result.Deleted != 1 {
		t.Fatalf("prune=%#v err=%v", result, err)
	}
	if err := db.First(&item, item.ID).Error; err != nil {
		t.Fatal("pruned execution history", err)
	}
}

func TestEventStepIdentityIsIndependentOfDisplayBudget(t *testing.T) {
	db := newLeaseTestDB(t)
	steps := map[string]interface{}{}
	for i := 0; i < 250; i++ {
		steps[fmt.Sprintf("s%03d", i)] = map[string]interface{}{"status": "success"}
	}
	steps["扫描步骤"] = map[string]interface{}{"status": "running"}
	item := execution.TaskExecution{TenantID: 7, ExecutionID: "many-steps", Module: execution.ModuleOrchestrator, TaskType: execution.TaskTypeOrchestration, Source: execution.ModuleOrchestrator, Status: "pending", TriggerType: "manual", ExecutionBoundary: "bounded", Metadata: commonModels.JSONMap{"step_results": steps}}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	_, lease, err := execution.ClaimNext(t.Context(), db, execution.ClaimOptions{Module: execution.ModuleOrchestrator, TaskType: execution.TaskTypeOrchestration, WorkerID: "owner", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"s249", "扫描步骤"} {
		if err := execution.AppendBoundedEvent(t.Context(), db, *lease, execution.EventInput{Kind: "progress", StepID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if execution.SafeEventStepID("password=private") != "" || execution.SafeEventStepID(strings.Repeat("x", 129)) != "" {
		t.Fatal("unsafe event identity was not omitted")
	}
}
