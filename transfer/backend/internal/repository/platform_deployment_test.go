package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/common/infra/database"
	"github.com/addp/transfer/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestInstalledTransferStorage(t *testing.T) {
	if os.Getenv("ADDP_TEST_TRANSFER_DEPLOYMENT") != "1" {
		t.Skip("requires isolated Transfer deployment")
	}
	db, err := OpenDatabase(database.Config{Host: os.Getenv("POSTGRES_HOST"), Port: os.Getenv("POSTGRES_PORT"), DBName: os.Getenv("POSTGRES_DB"), Schema: "transfer"})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	t.Run("startup_backfill_preserves_newer_schedule", func(t *testing.T) {
		exerciseScheduleStartupBackfill(t, db)
	})
	ctx := context.Background()
	checkStoredDeadLetter(t, db)
	bounded, _, err := NewTaskRepository(db).ClaimNextBoundedExecution(ctx, "fixture-worker", time.Minute)
	if err != nil || bounded != nil {
		t.Fatalf("empty bounded queue: %+v %v", bounded, err)
	}
	claim, err := NewRuntimeLeaseRepository(db, ContinuousRecoveryPolicy{}).ClaimNext(ctx, "fixture-continuous", time.Minute)
	if err != nil || claim != nil {
		t.Fatalf("empty continuous queue: %+v %v", claim, err)
	}
	t.Run("CDC approval and scan origin share stored time", func(t *testing.T) {
		now := time.Now().UTC()
		_, parent, _ := seedPendingSchemaChangeRuntime(t, db, 7, now, now.Add(time.Minute))
		var request models.SchemaChangeRequest
		if err := db.Where("execution_id = ?", parent.ExecutionID).First(&request).Error; err != nil {
			t.Fatal(err)
		}
		assertAppliedScanOriginClaimRoundTrip(t, db, request)
	})
	t.Run("continuous checkpoint rejects expired lease", func(t *testing.T) {
		testInstalledContinuousCheckpoint(t, db)
	})
	t.Log("dead letter repeat and collision verified; bounded and continuous worker polling succeeded")
}

func testInstalledContinuousCheckpoint(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC()
	task, execution, lease := seedPendingSchemaChangeRuntime(t, db, 7, now, now.Add(time.Minute))
	if err := db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
		conn = conn.Session(&gorm.Session{NewDB: true})
		expiredAt := "CURRENT_TIMESTAMP - INTERVAL '1 second'"
		if conn.Dialector.Name() == "mysql" {
			// Hold this connection in the same second as the lease deadline.
			if err := conn.Exec("SET timestamp = 1795659000.750000").Error; err != nil {
				return err
			}
			defer func() {
				if err := conn.Exec("SET timestamp = DEFAULT").Error; err != nil {
					t.Errorf("restore test database session clock: %v", err)
				}
			}()
			expiredAt = "TIMESTAMPADD(MICROSECOND, -250000, CURRENT_TIMESTAMP(6))"
		}
		states := NewSyncStateRepository(conn)
		const source = "orders.events"
		for _, partition := range []string{"2", "1"} {
			state, err := states.ClaimContinuousPartition(ctx, task.ID, source, partition, "kafka_offset", "v1", lease.OwnerInstanceID, lease.FencingToken)
			if err != nil {
				t.Fatalf("claim live partition %s: %v", partition, err)
			}
			if err := states.CommitContinuousPosition(ctx, state.ID, task.ID, state.StateVersion, lease.FencingToken, lease.OwnerInstanceID, models.JSONMap{"next_offset": "5"}, execution.ExecutionID); err != nil {
				t.Fatalf("commit live checkpoint for partition %s: %v", partition, err)
			}
		}
		list, err := states.List(ctx, task.ID, source)
		if err != nil || len(list) != 2 || list[0].Partition != "1" || list[1].Partition != "2" {
			t.Errorf("list checkpoints by partition order: %+v, err=%v", list, err)
		}
		state, err := states.Get(ctx, task.ID, source, "1")
		if err != nil {
			return err
		}
		if err := conn.Model(&lease).UpdateColumn("lease_until", gorm.Expr(expiredAt)).Error; err != nil {
			return err
		}
		if _, err := states.ClaimContinuousPartition(ctx, task.ID, source, "3", "kafka_offset", "v1", lease.OwnerInstanceID, lease.FencingToken); !errors.Is(err, ErrSyncStateFenced) {
			t.Errorf("expired partition claim: %v, want fenced", err)
		}
		if err := states.CommitContinuousPosition(ctx, state.ID, task.ID, state.StateVersion, lease.FencingToken, lease.OwnerInstanceID, models.JSONMap{"next_offset": "6"}, execution.ExecutionID); !errors.Is(err, ErrSyncStateFenced) {
			t.Errorf("expired checkpoint write: %v, want fenced", err)
		}
		if _, err := states.Get(ctx, task.ID, source, "3"); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("expired claim left a checkpoint: %v, want record not found", err)
		}
		stored, err := states.GetByID(ctx, state.ID)
		if err != nil {
			return err
		}
		if stored.StateVersion != 1 || stored.Position["next_offset"] != "5" {
			t.Errorf("checkpoint changed after expiry: version=%d, position=%v", stored.StateVersion, stored.Position)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !t.Failed() {
		t.Log("live lease committed two ordered checkpoints; expired lease could neither claim nor advance")
	}
}

func exerciseScheduleStartupBackfill(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := t.Context()
	task := models.TransferTask{TenantID: 999999, Name: "schedule-backfill-" + uuid.NewString(), TaskType: "sync", Config: models.JSONMap{}, Schedule: "0 0 3 * * *", Enabled: true, Status: models.TaskStatusIdle}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Unscoped().Delete(&task).Error; err != nil {
			t.Error(err)
		}
	})
	due := time.Now().UTC().Truncate(time.Second).Add(time.Hour)
	later := due.Add(time.Hour)
	// Both replicas read this version while its next run is still missing.
	replicaA, replicaB := NewTaskRepository(db).InitializeNextRunAt, NewTaskRepository(db).InitializeNextRunAt
	assertNext := func(want *time.Time) {
		t.Helper()
		var current models.TransferTask
		if err := db.First(&current, task.ID).Error; err != nil {
			t.Fatal(err)
		}
		got := current.NextRunAt
		if (want == nil && got != nil) || (want != nil && (got == nil || !got.Equal(*want))) {
			t.Fatalf("next_run_at = %v, want %v", got, want)
		}
	}
	update := func(values map[string]interface{}) {
		t.Helper()
		if err := db.Model(&models.TransferTask{}).Where("id = ?", task.ID).Updates(values).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := replicaA(ctx, task, due); err != nil {
		t.Fatal(err)
	}
	if err := replicaB(ctx, task, later); err != nil {
		t.Fatal(err)
	}
	assertNext(&due)

	// A late initializer must preserve a position already advanced by a claim.
	update(map[string]interface{}{"next_run_at": later})
	if err := replicaB(ctx, task, due); err != nil {
		t.Fatal(err)
	}
	assertNext(&later)

	update(map[string]interface{}{"enabled": false, "next_run_at": nil})
	if err := replicaB(ctx, task, due); err != nil {
		t.Fatal(err)
	}
	assertNext(nil)
	update(map[string]interface{}{"enabled": true, "schedule": "0 7 * * *", "next_run_at": nil})
	if err := replicaB(ctx, task, due); err != nil {
		t.Fatal(err)
	}
	assertNext(nil)
}
