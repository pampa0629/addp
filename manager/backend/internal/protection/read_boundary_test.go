package protection

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type boundaryTestStore struct {
	refresh func(context.Context, int64) error
}

type boundaryTestIndex struct{ err error }

func (s boundaryTestIndex) ReadyToAcknowledge(context.Context, int64, string) error { return s.err }

func (s *boundaryTestStore) EnsureCurrent(ctx context.Context, tenant int64) error {
	if s.refresh != nil {
		return s.refresh(ctx, tenant)
	}
	return ctx.Err()
}

func TestReadBoundaryRegistersBeforeRefreshAndReleasesOnFailure(t *testing.T) {
	store := &boundaryTestStore{}
	boundary := NewReadBoundary(store)
	store.refresh = func(_ context.Context, tenant int64) error {
		if !boundary.HasActiveExecutionsForTenant(tenant) {
			t.Fatal("refresh preceded registration")
		}
		return errors.New("checkpoint unavailable")
	}
	if end, err := boundary.BeginRead(t.Context(), 7); !errors.Is(err, ErrRequired) || end != nil {
		t.Fatalf("BeginRead returned end=%t, error=%v", end != nil, err)
	}
	if boundary.HasActiveExecutionsForTenant(7) {
		t.Fatal("failed read blocks acknowledgement forever")
	}
	store.refresh = nil
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if end, err := boundary.BeginRead(ctx, 7); !errors.Is(err, context.Canceled) || end != nil {
		t.Fatalf("canceled begin: %v", err)
	}
	if boundary.HasActiveExecutionsForTenant(7) {
		t.Fatal("canceled read retained")
	}
}

func TestManagerAcknowledgementWaitsForLocalAndLiveLeasedWork(t *testing.T) {
	db := newReadBoundaryTestDB(t)
	boundary := NewReadBoundary(&boundaryTestStore{})
	barrier := NewAcknowledgementBarrier(db, boundary, boundaryTestIndex{})
	now := time.Now().UTC()
	past, future := now.Add(-time.Minute), now.Add(time.Minute)
	newExecution := func(id, status string, tenant int, module string, expiry *time.Time) execution.TaskExecution {
		return execution.TaskExecution{ExecutionID: id, TenantID: tenant, Module: module,
			TaskType: execution.TaskTypeDataProfiling, Source: execution.ModuleManager, Status: status,
			ExecutionBoundary: execution.ExecutionBoundaryBounded, LeaseExpiresAt: expiry}
	}
	items := []execution.TaskExecution{
		newExecution("pending", execution.ExecutionStatusPending, 7, execution.ModuleManager, &future),
		newExecution("expired", execution.ExecutionStatusRunning, 7, execution.ModuleManager, &past),
		newExecution("unleased", execution.ExecutionStatusRunning, 7, execution.ModuleManager, nil),
		newExecution("other-tenant", execution.ExecutionStatusRunning, 8, execution.ModuleManager, &future),
		newExecution("other-owner", execution.ExecutionStatusRunning, 7, execution.ModuleDevelop, &future),
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	if err := barrier.ReadyToAcknowledge(t.Context(), 7, "cursor"); err != nil {
		t.Fatal(err)
	}
	end, err := boundary.BeginRead(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := barrier.ReadyToAcknowledge(t.Context(), 7, "cursor"); err == nil {
		t.Fatal("local read did not block acknowledgement")
	}
	end()
	live := newExecution("live", execution.ExecutionStatusRunning, 7, execution.ModuleManager, &future)
	if err := db.Create(&live).Error; err != nil {
		t.Fatal(err)
	}
	if err := barrier.ReadyToAcknowledge(t.Context(), 7, "cursor"); err == nil {
		t.Fatal("live worker did not block acknowledgement")
	}
	if err := db.Model(&live).Update("lease_expires_at", past).Error; err != nil {
		t.Fatal(err)
	}
	if err := barrier.ReadyToAcknowledge(t.Context(), 7, "cursor"); err != nil {
		t.Fatal(err)
	}
	if err := NewAcknowledgementBarrier(db, nil, boundaryTestIndex{}).ReadyToAcknowledge(t.Context(), 7, "cursor"); err == nil {
		t.Fatal("missing boundary was accepted")
	}
	if err := NewAcknowledgementBarrier(db, boundary, nil).ReadyToAcknowledge(t.Context(), 7, "cursor"); err == nil {
		t.Fatal("missing durable index fence was accepted")
	}
	indexFailure := errors.New("index fence unavailable")
	if err := NewAcknowledgementBarrier(db, boundary, boundaryTestIndex{err: indexFailure}).ReadyToAcknowledge(t.Context(), 7, "cursor"); !errors.Is(err, indexFailure) {
		t.Fatal("index fence failure was ignored")
	}
}

func newReadBoundaryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestManagerAcknowledgementKeepsInstalledCursorWhileOldReadFinishes(t *testing.T) {
	db := newReadBoundaryTestDB(t)
	if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
		t.Fatal(err)
	}
	installer, err := projectionstore.Migrate(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := projectionstore.Open(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	boundary := NewReadBoundary(reader)
	barrier := NewAcknowledgementBarrier(db, boundary, boundaryTestIndex{})
	endOld, err := boundary.BeginRead(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	defer endOld()
	now := time.Now().UTC()
	target := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item"}
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV2, ProjectionID: "projection",
		Revision: "00000000000000000001", ConsumerOwner: "manager",
		State: dataprotection.ProjectionStateEnrolling, Target: target, Rules: []dataprotection.Rule{},
		ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := installer.ApplyBatch(t.Context(), 7, "", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: "new-cursor",
		Changes: []dataprotection.ProjectionChange{{ChangeID: "change", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
	}, now); err != nil {
		t.Fatal(err)
	}
	if err := barrier.ReadyToAcknowledge(t.Context(), 7, "new-cursor"); err == nil {
		t.Fatal("old read allowed acknowledgement")
	}
	if cursor, err := installer.CurrentCursor(t.Context(), 7); err != nil || cursor != "new-cursor" {
		t.Fatalf("installed cursor was rolled back: %q, %v", cursor, err)
	}
	endNew, err := boundary.BeginRead(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	defer endNew()
	if gate := reader.Gate(7, target, now); !gate.Managed || gate.State != dataprotection.ProjectionStateEnrolling {
		t.Fatalf("new read did not refresh installed protection: %+v", gate)
	}
	endNew()
	if err := barrier.ReadyToAcknowledge(t.Context(), 7, "new-cursor"); err == nil {
		t.Fatal("new read completion released old read")
	}
	endOld()
	if err := barrier.ReadyToAcknowledge(t.Context(), 7, "new-cursor"); err != nil {
		t.Fatal(err)
	}
}
