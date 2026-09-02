package protection

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataprotection/projectionstore"
	commonexecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type barrierActiveReader bool

func (r barrierActiveReader) HasActiveExecutionsForTenant(int64) bool { return bool(r) }

func TestExecutionBarrierUsesLiveExecutionBoundaries(t *testing.T) {
	db, gate := newBarrierTestRuntime(t)
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)
	leaseOwner := "develop-query-worker"
	leaseToken := "00000000-0000-0000-0000-000000000001"

	items := []commonexecution.TaskExecution{
		barrierExecution("pending", commonexecution.ExecutionStatusPending, nil, nil, nil),
		barrierExecution("stale-local", commonexecution.ExecutionStatusRunning, &past, nil, nil),
		barrierExecution("expired-lease", commonexecution.ExecutionStatusRunning, nil, &leaseOwner, &past),
		barrierExecution("other-tenant", commonexecution.ExecutionStatusRunning, &future, nil, nil),
	}
	items[2].LeaseToken = &leaseToken
	items[3].TenantID = 8
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	barrier := NewExecutionBarrier(db, gate, nil)
	if err := barrier.ReadyToAcknowledge(t.Context(), 7, "cursor-1"); err != nil {
		t.Fatalf("stale and pending executions must not block acknowledgement: %v", err)
	}

	end, err := gate.BeginUnresolvedRead(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := barrier.ReadyToAcknowledge(t.Context(), 7, "cursor-1"); err == nil {
		t.Fatal("in-process read must block acknowledgement")
	}
	end()

	liveLocal := barrierExecution("live-local", commonexecution.ExecutionStatusRunning, &future, nil, nil)
	if err := db.Create(&liveLocal).Error; err != nil {
		t.Fatal(err)
	}
	if err := barrier.ReadyToAcknowledge(t.Context(), 7, "cursor-1"); err == nil {
		t.Fatal("running local execution with a live authorization must block acknowledgement")
	}
	if err := db.Delete(&liveLocal).Error; err != nil {
		t.Fatal(err)
	}

	liveLease := barrierExecution("live-lease", commonexecution.ExecutionStatusRunning, nil, &leaseOwner, &future)
	liveLease.LeaseToken = &leaseToken
	if err := db.Create(&liveLease).Error; err != nil {
		t.Fatal(err)
	}
	if err := barrier.ReadyToAcknowledge(t.Context(), 7, "cursor-1"); err == nil {
		t.Fatal("running worker execution with a live lease must block acknowledgement")
	}
	if err := db.Delete(&liveLease).Error; err != nil {
		t.Fatal(err)
	}

	if err := NewExecutionBarrier(db, gate, barrierActiveReader(true)).ReadyToAcknowledge(t.Context(), 7, "cursor-1"); err == nil {
		t.Fatal("active notebook execution must block acknowledgement")
	}
}

func TestExecutionBarrierRequiresGate(t *testing.T) {
	db, _ := newBarrierTestRuntime(t)
	err := NewExecutionBarrier(db, nil, nil).ReadyToAcknowledge(context.Background(), 7, "cursor-1")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("ReadyToAcknowledge() error = %v, want configuration error", err)
	}
}

func newBarrierTestRuntime(t *testing.T) (*gorm.DB, *Gate) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
		t.Fatal(err)
	}
	store, err := projectionstore.New(db, "develop", "develop", nil)
	if err != nil {
		t.Fatal(err)
	}
	return db, NewGate(store)
}

func barrierExecution(
	executionID, status string,
	authorizationExpiresAt *time.Time,
	leaseOwner *string,
	leaseExpiresAt *time.Time,
) commonexecution.TaskExecution {
	now := time.Now().UTC()
	return commonexecution.TaskExecution{
		TenantID: 7, ExecutionID: executionID, Module: commonexecution.ModuleDevelop,
		TaskType: commonexecution.TaskTypeQuery, Source: commonexecution.ModuleDevelop,
		Status: status, ExecutionBoundary: commonexecution.ExecutionBoundaryBounded,
		LeaseOwner: leaseOwner, LeaseExpiresAt: leaseExpiresAt,
		AuthorizationExpiresAt: authorizationExpiresAt,
		TriggerType:            commonexecution.TriggerTypeManual, CreatedAt: now, UpdatedAt: now,
	}
}
