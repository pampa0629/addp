package execution_test

import (
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestExecutionEventRetention(t *testing.T) {
	eventRetentionCases(t, func(t *testing.T, _ []string) *gorm.DB {
		db := newLeaseTestDB(t)
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		return db
	})
}

func TestExecutionEventRetentionAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_EXECUTION_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_EXECUTION_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
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
	eventRetentionCases(t, func(t *testing.T, ids []string) *gorm.DB {
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		t.Cleanup(func() {
			if err := tx.Rollback().Error; err != nil {
				t.Error(err)
			}
			for _, model := range []interface{}{&execution.Event{}, &execution.TaskExecution{}} {
				var remaining int64
				if err := db.Model(model).Where("execution_id IN ?", ids).Count(&remaining).Error; err != nil || remaining != 0 {
					t.Errorf("retention fixture remains: count=%d err=%v", remaining, err)
				}
			}
		})
		return tx
	})
}

func eventRetentionCases(t *testing.T, open func(*testing.T, []string) *gorm.DB) {
	for _, failDelete := range []bool{false, true} {
		name := "cutoff_and_batch_limits_preserve_complete_execution_facts"
		if failDelete {
			name = "failed_delete_rolls_back_and_returns_no_committed_result"
		}
		t.Run(name, func(t *testing.T) {
			ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
			db := open(t, ids)
			now := time.Now().UTC().Truncate(time.Microsecond)
			cutoff := now.Add(-execution.EventRetention)
			oldCompletion := now.Add(-200 * 24 * time.Hour)
			principal, membership, version := int64(9), int64(90), int64(1)
			authorizationExpiresAt := now.Add(time.Hour)
			for i, id := range ids {
				authorizationID := now.UnixNano() + int64(i)
				row := execution.TaskExecution{ExecutionID: id, TenantID: 7, Module: "meta", TaskType: "scan", Source: "meta",
					Status: execution.ExecutionStatusSuccess, TriggerType: "manual", CompletedAt: &oldCompletion,
					ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version,
					ExecutionAuthorizationID: &authorizationID, AuthorizationExpiresAt: &authorizationExpiresAt,
					ExecutionConfig: models.JSONMap{"parameters": "retained-professional-context"},
					Metadata:        models.JSONMap{"result": "retained-professional-result", "lineage_facts": map[string]interface{}{"pending": true}},
					ErrorDetails:    models.JSONMap{"error_code": "execution_failed"}}
				if i == 1 {
					row.Status, row.CompletedAt, row.ParentExecutionID = execution.ExecutionStatusRunning, nil, &ids[0]
				}
				if i == 2 {
					row.TenantID = 8
				}
				if err := db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			}
			loadExecutions := func() []execution.TaskExecution {
				t.Helper()
				var rows []execution.TaskExecution
				if err := db.Where("execution_id IN ?", ids).Order("execution_id").Find(&rows).Error; err != nil {
					t.Fatal(err)
				}
				return rows
			}
			before := loadExecutions()
			// Limit destructive test queries to this case's UUIDs, even when the
			// standard gate shares addp_test with another owner's fixtures.
			maintenanceDB := db.Where("execution_id IN ?", ids)
			events := []execution.Event{}
			for i, at := range []time.Time{cutoff.Add(-2 * time.Second), cutoff.Add(-time.Second), cutoff.Add(-time.Microsecond), cutoff, now.Add(-time.Hour), now} {
				tenant := 7
				if i%3 == 2 {
					tenant = 8
				}
				event := execution.Event{ExecutionID: ids[i%3], TenantID: tenant, Module: "meta", TaskType: "scan", Attempt: 1,
					OccurredAt: at, Kind: "progress", Counters: models.JSONMap{}}
				if err := db.Create(&event).Error; err != nil {
					t.Fatal(err)
				}
				events = append(events, event)
			}
			if failDelete {
				injected := errors.New("retention delete failed after SQL")
				callback := "retention_failure_" + uuid.NewString()
				var affected int64
				if err := db.Callback().Delete().After("gorm:delete").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Table == (execution.Event{}).TableName() {
						affected = tx.RowsAffected
						tx.AddError(injected)
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Callback().Delete().Remove(callback) })
				result, err := execution.PruneEvents(t.Context(), maintenanceDB, now, 1000)
				if !errors.Is(err, injected) || affected != 3 {
					t.Fatal("delete failure was not injected after the expired rows were affected", err)
				}
				var count int64
				if err := db.Model(&execution.Event{}).Where("execution_id IN ?", ids).Count(&count).Error; err != nil || count != 6 {
					t.Fatal("failed cleanup committed a partial deletion", err)
				}
				if result != nil {
					t.Fatal("rolled-back cleanup returned a committed result", result.Deleted)
				}
			} else {
				for _, want := range []int64{1, 1, 1, 0} {
					result, err := execution.PruneEvents(t.Context(), maintenanceDB, now, 1)
					if err != nil || result.Deleted != want || !result.ExpiredBefore.Equal(cutoff) {
						t.Fatal("cleanup ignored its batch limit or retention cutoff", err)
					}
				}
				var remaining []execution.Event
				if err := db.Where("execution_id IN ?", ids).Order("id").Find(&remaining).Error; err != nil || len(remaining) != 3 {
					t.Fatal("cleanup removed a non-expired event", err)
				}
				for i, event := range remaining {
					if event.ID != events[i+3].ID || !event.OccurredAt.Equal(events[i+3].OccurredAt) || event.Attempt != 1 || event.Kind != "progress" || len(event.Counters) != 0 {
						t.Fatal("cleanup crossed the retention boundary or changed a retained event")
					}
				}
			}
			if !reflect.DeepEqual(before, loadExecutions()) {
				t.Fatal("event maintenance changed execution, tree, professional result or authorization facts")
			}
		})
	}
}
