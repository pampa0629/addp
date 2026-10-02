package execution_test

import (
	"context"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
)

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
		{Kind: "private SQL"}, {Kind: "progress", StepID: "password=private"},
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
