package repository

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	plugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	execution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresTargetProductionReceipts(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run make test-transfer-postgres")
	}
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(testpg.ConnInfoFromEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	open := func() *gorm.DB {
		t.Helper()
		db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pool.Close() })
		return db
	}
	db := open()
	if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	fixture := func(t *testing.T) (execution.TaskExecution, execution.Lease) {
		t.Helper()
		item, lease := productionExecutionFixture(t, db)
		t.Cleanup(func() {
			if err := db.Where("id=? AND tenant_id=?", item.ID, item.TenantID).Delete(&execution.TaskExecution{}).Error; err != nil {
				t.Error(err)
			}
			var remaining int64
			if err := db.Model(&execution.TaskExecution{}).Where("id=?", item.ID).Count(&remaining).Error; err != nil || remaining != 0 {
				t.Errorf("receipt cleanup not proven: count=%d error=%v", remaining, err)
			}
		})
		return item, lease
	}
	t.Run("one durable intent under concurrent admission", func(t *testing.T) {
		item, lease := fixture(t)
		start := make(chan struct{})
		results := make(chan error, 2)
		var ready sync.WaitGroup
		ready.Add(2)
		for i := 0; i < 2; i++ {
			go func() {
				ready.Done()
				<-start
				results <- NewTaskRepository(db).BeginTargetProduction(t.Context(), lease, productionTestLocator)
			}()
		}
		ready.Wait()
		close(start)
		passed := 0
		for i := 0; i < 2; i++ {
			if <-results == nil {
				passed++
			}
		}
		if passed != 1 || readProductionRecord(t, db, item.ID).Phase != models.TargetProductionPending {
			t.Fatal("concurrent admission reset or duplicated intent")
		}
		// A new repository/connection recovers the persisted receipt, not local state.
		recovered := NewTaskRepository(open())
		if err := recovered.RecordTargetPreparation(t.Context(), lease, productionTestLocator, plugin.TableTargetPreparation{Disposition: plugin.TableTargetRecreated}); err != nil {
			t.Fatal(err)
		}
		if err := recovered.RecordTargetCommit(t.Context(), lease, productionTestLocator, nil); err != nil {
			t.Fatal(err)
		}
		if readProductionRecord(t, db, item.ID).Phase != models.TargetProductionCommitted {
			t.Fatal("receipt not recovered")
		}
		sources, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(3, "schema", "outdoor", "source"))
		if err := recovered.RecordTargetCommit(t.Context(), lease, productionTestLocator, sources); err == nil {
			t.Fatal("unavailable source evidence backfilled after recovery")
		}
	})
	t.Run("committed actual sources remain immutable across repository recovery", func(t *testing.T) {
		item, lease := fixture(t)
		repo := NewTaskRepository(db)
		if err := repo.BeginTargetProduction(t.Context(), lease, productionTestLocator); err != nil {
			t.Fatal(err)
		}
		if err := repo.RecordTargetPreparation(t.Context(), lease, productionTestLocator, plugin.TableTargetPreparation{Disposition: plugin.TableTargetCreated}); err != nil {
			t.Fatal(err)
		}
		const largeEngine = uint(9007199254740993)
		sources, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(largeEngine, "schema", "outdoor", "source"))
		if err := repo.RecordTargetCommit(t.Context(), lease, productionTestLocator, sources); err != nil {
			t.Fatal(err)
		}
		original := readProductionRecord(t, db, item.ID)
		recovered := NewTaskRepository(open())
		if err := recovered.RecordTargetCommit(t.Context(), lease, productionTestLocator, sources); err != nil {
			t.Fatal(err)
		}
		sources.Paths[0].Segments[2].Name = "other"
		if err := recovered.RecordTargetCommit(t.Context(), lease, productionTestLocator, sources); err == nil {
			t.Fatal("recovered worker rewrote committed source proof")
		}
		if after := readProductionRecord(t, db, item.ID); after.SourceReadSet == nil || after.SourceReadSet.Paths[0].Segments[2].Name != "source" || after.SourceReadSet.Paths[0].EngineID != largeEngine || !after.CommittedAt.Equal(*original.CommittedAt) {
			t.Fatal("recovery changed original sources/commit time")
		}
	})
	t.Run("new attempt cannot reset a crashed original", func(t *testing.T) {
		item, lease := fixture(t)
		repo := NewTaskRepository(db)
		if err := repo.BeginTargetProduction(t.Context(), lease, productionTestLocator); err != nil {
			t.Fatal(err)
		}
		newLease := lease
		newLease.Attempt++
		newLease.Token = uuid.NewString()
		newLease.Owner = "recovered-worker"
		if err := db.Model(&item).Updates(map[string]interface{}{"attempt": newLease.Attempt, "lease_token": newLease.Token, "lease_owner": newLease.Owner}).Error; err != nil {
			t.Fatal(err)
		}
		if err := repo.BeginTargetProduction(t.Context(), newLease, productionTestLocator); err == nil {
			t.Fatal("new attempt erased original intent")
		}
		if err := repo.RecordTargetPreparation(t.Context(), newLease, productionTestLocator, plugin.TableTargetPreparation{Disposition: plugin.TableTargetCreated}); err == nil {
			t.Fatal("new attempt rewrote old outcome")
		}
		if err := repo.RecordTargetCommit(t.Context(), lease, productionTestLocator, nil); err == nil {
			t.Fatal("stale worker completed receipt")
		}
		record := readProductionRecord(t, db, item.ID)
		if record.Attempt != 1 || record.Phase != models.TargetProductionPending {
			t.Fatal("crashed receipt changed")
		}
	})
	t.Run("lease expires while waiting for row lock", func(t *testing.T) {
		item, lease := fixture(t)
		if err := NewTaskRepository(db).BeginTargetProduction(t.Context(), lease, productionTestLocator); err != nil {
			t.Fatal(err)
		}
		waitingDB := open()
		pool, _ := waitingDB.DB()
		pool.SetMaxOpenConns(1)
		appName := "production_wait_" + uuid.NewString()
		if err := waitingDB.Exec("SELECT set_config('application_name',?,false)", appName).Error; err != nil {
			t.Fatal(err)
		}
		blocker := db.Begin()
		if blocker.Error != nil {
			t.Fatal(blocker.Error)
		}
		defer blocker.Rollback()
		if err := blocker.Exec("UPDATE common.task_executions SET lease_expires_at=clock_timestamp()+interval '300 milliseconds' WHERE id=?", item.ID).Error; err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		result := make(chan error, 1)
		go func() {
			result <- NewTaskRepository(waitingDB).RecordTargetPreparation(ctx, lease, productionTestLocator, plugin.TableTargetPreparation{Disposition: plugin.TableTargetCreated})
		}()
		waitFor := func(predicate func() bool) {
			t.Helper()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for !predicate() {
				select {
				case <-ctx.Done():
					t.Fatal("timed out observing lock/expiry")
				case <-ticker.C:
				}
			}
		}
		waitFor(func() bool {
			var waiting bool
			if err := db.Raw("SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name=? AND wait_event_type='Lock')", appName).Scan(&waiting).Error; err != nil {
				t.Fatal(err)
			}
			return waiting
		})
		waitFor(func() bool {
			var expired bool
			if err := blocker.Raw("SELECT lease_expires_at<clock_timestamp() FROM common.task_executions WHERE id=?", item.ID).Scan(&expired).Error; err != nil {
				t.Fatal(err)
			}
			return expired
		})
		if err := blocker.Commit().Error; err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-result:
			if err == nil {
				t.Fatal("expired lock waiter updated receipt")
			}
		case <-ctx.Done():
			t.Fatal(fmt.Errorf("receipt waiter did not stop: %w", ctx.Err()))
		}
		if readProductionRecord(t, db, item.ID).Phase != models.TargetProductionPending {
			t.Fatal("late receipt changed pending state")
		}
	})
}
