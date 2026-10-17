package repository

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/engine/plugins/postgresql"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresScheduledAuthorizationClaim(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Transfer PostgreSQL gate")
	}
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(testpg.ConnInfoFromEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS transfer").Error; err != nil {
		t.Fatal(err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.TransferTask{}, &models.SyncState{}); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"concurrent", "changed_definition", "changed_actor"} {
		t.Run(scenario, func(t *testing.T) {
			due := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
			task := models.TransferTask{TenantID: 999999, Name: "schedule-" + uuid.NewString(), TaskType: "sync", Config: models.JSONMap{"runtime": map[string]interface{}{"boundary": "bounded"}, "load": map[string]interface{}{"mode": "snapshot"}}, Schedule: "0 */5 * * * *", Enabled: true, NextRunAt: &due, Status: models.TaskStatusIdle}
			if err := db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = db.Where("module=? AND source_task_id=?", "transfer", fmt.Sprint(task.ID)).Delete(&commonExecution.TaskExecution{}).Error
				_ = db.Unscoped().Delete(&models.TransferTask{}, task.ID).Error
			})
			hash := authorizeScheduledRepositoryTask(t, db, &task)
			if scenario == "changed_definition" {
				if err := db.Model(&task).Update("auto_scan_metadata", true).Error; err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "changed_actor" {
				if err := db.Model(&task).Update("authorization_principal_id", 42).Error; err != nil {
					t.Fatal(err)
				}
			}
			claim := func() (bool, error) {
				record := claimTestExecution(task, uuid.NewString())
				record.TriggerType = "scheduled"
				next := time.Now().UTC().Add(time.Hour)
				claimed, _, err := NewTaskRepository(db).ClaimDueScheduledExecution(context.Background(), task.ID, task.Schedule, hash, time.Now(), &next, &record, "")
				return claimed != nil, err
			}
			if scenario == "concurrent" {
				start := make(chan struct{})
				var wg sync.WaitGroup
				results := make(chan bool, 2)
				errors := make(chan error, 2)
				for i := 0; i < 2; i++ {
					wg.Add(1)
					go func() { defer wg.Done(); <-start; claimed, err := claim(); results <- claimed; errors <- err }()
				}
				close(start)
				wg.Wait()
				close(results)
				close(errors)
				winners := 0
				for result := range results {
					if result {
						winners++
					}
				}
				for err := range errors {
					if err != nil {
						t.Fatal(err)
					}
				}
				if winners != 1 {
					t.Fatalf("claim winners=%d", winners)
				}
			} else {
				if _, err := claim(); err == nil {
					t.Fatal("stale authorization claimed")
				}
			}
			var stored models.TransferTask
			if err := db.First(&stored, task.ID).Error; err != nil {
				t.Fatal(err)
			}
			var records []commonExecution.TaskExecution
			if err := db.Where("module=? AND source_task_id=?", "transfer", fmt.Sprint(task.ID)).Find(&records).Error; err != nil {
				t.Fatal(err)
			}
			if scenario != "concurrent" {
				if len(records) != 0 || !stored.NextRunAt.Equal(due) {
					t.Fatal("rejected claim left execution or advanced schedule")
				}
				return
			}
			if len(records) != 1 || records[0].ActorPrincipalID == nil || *records[0].ActorPrincipalID != 9 || *records[0].ActorTenantMembershipID != 19 || *records[0].IssuedAuthorizationVersion != 1 || records[0].TriggerType != "scheduled" || !stored.NextRunAt.After(due) {
				t.Fatal("scheduled transaction failed to freeze source and advance exactly once")
			}
		})
	}
}
