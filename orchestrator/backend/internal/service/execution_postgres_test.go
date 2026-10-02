package service

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	execution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/schema"
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func postgresReliabilityDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("PostgreSQL integration gate is not enabled")
	}
	port, password := os.Getenv("ADDP_TEST_POSTGRES_PORT"), os.Getenv("ADDP_TEST_POSTGRES_PASSWORD")
	if port == "" || password == "" {
		t.Fatal("PostgreSQL gate requires verified port and password")
	}
	host, user, database := os.Getenv("ADDP_TEST_POSTGRES_HOST"), os.Getenv("ADDP_TEST_POSTGRES_USER"), os.Getenv("ADDP_TEST_POSTGRES_DATABASE")
	if host == "" {
		host = "localhost"
	}
	if user == "" {
		user = "addp"
	}
	if database == "" {
		database = "addp_test"
	}
	dsn := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port), User: url.UserPassword(user, password), Path: database, RawQuery: "sslmode=disable"}
	db, err := gorm.Open(postgres.Open(dsn.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open PostgreSQL failed")
	}
	connection, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS orchestrator").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Orchestration{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestIntegrationPostgresExecutionReliability(t *testing.T) {
	db := postgresReliabilityDB(t)
	s := NewExecutionService(db)
	ctx := context.Background()
	actor := ExecutionActor{PrincipalID: 9, TenantMembershipID: 19, AuthorizationVersion: 3}
	definition := models.Orchestration{TenantID: 999999, Name: "execution-gate-" + uuid.NewString(), Steps: models.Steps{{ID: "first", Name: "Scan", Provider: "meta", TaskType: "scan", TaskID: 41}}, EditorLayout: commonModels.JSONMap{}}
	if err := db.Create(&definition).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var ids []string
		_ = db.Model(&execution.TaskExecution{}).Where("module=? AND tenant_id=? AND source_task_id=?", "orchestrator", definition.TenantID, execution.NewSourceTaskIDFromUint(definition.ID)).Pluck("execution_id", &ids).Error
		if len(ids) > 0 {
			if err := db.Where("execution_id IN ?", ids).Delete(&execution.Event{}).Error; err != nil {
				t.Error(err)
			}
			if err := db.Where("execution_id IN ?", ids).Delete(&execution.TaskExecution{}).Error; err != nil {
				t.Error(err)
			}
		}
		if err := db.Unscoped().Delete(&definition).Error; err != nil {
			t.Error(err)
		}
		var remaining int64
		if err := db.Model(&execution.TaskExecution{}).Where("module=? AND tenant_id=? AND source_task_id=?", "orchestrator", definition.TenantID, execution.NewSourceTaskIDFromUint(definition.ID)).Count(&remaining).Error; err != nil || remaining != 0 {
			t.Errorf("cleanup remaining=%d err=%v", remaining, err)
		}
	})

	t.Run("single_claim_and_stale_writer_fencing", func(t *testing.T) {
		item, err := s.CreateExecutionWithContext(ctx, definition.ID, definition.TenantID, "manual", "orchestrator", nil, actor)
		if err != nil {
			t.Fatal(err)
		}
		var claims atomic.Int32
		var group sync.WaitGroup
		var claimed execution.Lease
		var mutex sync.Mutex
		for range 8 {
			group.Add(1)
			go func() {
				defer group.Done()
				row, lease, err := s.ClaimNext(ctx, "claim-"+uuid.NewString(), time.Minute)
				if err != nil {
					t.Error(err)
					return
				}
				if row != nil {
					claims.Add(1)
					mutex.Lock()
					claimed = *lease
					mutex.Unlock()
					if row.ExecutionID != item.ExecutionID {
						t.Error("claimed another fixture")
					}
				}
			}()
		}
		group.Wait()
		if claims.Load() != 1 {
			t.Fatalf("claims=%d", claims.Load())
		}
		stale := claimed
		stale.Token = uuid.NewString()
		if err := s.FinishExecution(ctx, stale, "success", ""); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("stale finish=%v", err)
		}
		if err := s.UpdateStepResults(ctx, claimed, models.StepResults{"first": {Status: "running", Phase: "waiting", Result: map[string]interface{}{"execution_id": "child"}, StartedAt: time.Now()}}, "first"); err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&execution.TaskExecution{}).Where("id=?", item.ID).Update("lease_expires_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		count, err := s.RecoverExpired(ctx, time.Now().UTC(), 100)
		if err != nil || count != 1 {
			t.Fatalf("recovery count=%d err=%v", count, err)
		}
		row, err := s.GetExecution(ctx, uint(item.ID), definition.TenantID)
		if err != nil {
			t.Fatal(err)
		}
		results, _ := readStepResults(row.Metadata)
		if row.Status != "failed" || row.ErrorDetails["code"] != "orchestrator.execution.lease_expired" || extractProviderExecutionID(results["first"].Result) != "child" {
			t.Fatalf("recovery row=%+v", row)
		}
		if err := s.UpdateStepResults(ctx, claimed, models.StepResults{}, "first"); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("late progress=%v", err)
		}
	})

	t.Run("schedule_is_atomic_and_consumed_once", func(t *testing.T) {
		due := time.Now().UTC().Add(-time.Minute)
		next := time.Now().UTC().Add(time.Hour)
		subject := int64(29)
		hash := strings.Repeat("a", 64)
		ref := uuid.New()
		authorized := time.Now().UTC()
		if err := db.Model(&definition).Updates(map[string]interface{}{"enabled": true, "schedule": "0 * * * *", "next_run_at": due, "authorization_subject_id": subject, "authorization_definition_hash": hash, "authorization_ref": ref, "authorization_principal_id": actor.PrincipalID, "authorization_membership_id": actor.TenantMembershipID, "authorization_version": actor.AuthorizationVersion, "authorized_at": authorized}).Error; err != nil {
			t.Fatal(err)
		}
		var expected models.Orchestration
		if err := db.First(&expected, definition.ID).Error; err != nil {
			t.Fatal(err)
		}
		// Force admission failure after the due row is locked; the due time must roll back.
		invalidActor := ExecutionActor{}
		if _, err := s.CreateScheduled(ctx, &expected, time.Now().UTC(), &next, invalidActor); err == nil {
			t.Fatal("invalid actor admitted")
		}
		var unchanged models.Orchestration
		_ = db.First(&unchanged, definition.ID).Error
		if !unchanged.NextRunAt.Equal(*expected.NextRunAt) {
			t.Fatal("failed admission consumed schedule")
		}
		changed := expected
		wrongHash := strings.Repeat("b", 64)
		changed.AuthorizationDefinitionHash = &wrongHash
		if _, err := s.CreateScheduled(ctx, &changed, time.Now().UTC(), &next, actor); err == nil {
			t.Fatal("changed authorization admitted")
		}
		var admitted atomic.Int32
		var group sync.WaitGroup
		for range 8 {
			group.Add(1)
			go func() {
				defer group.Done()
				item, err := s.CreateScheduled(ctx, &expected, time.Now().UTC(), &next, actor)
				if err != nil {
					t.Error(err)
					return
				}
				if item != nil {
					admitted.Add(1)
					if item.ExecutionConfig["schema_version"] != executionPlanVersion {
						t.Error("schedule missing snapshot")
					}
				}
			}()
		}
		group.Wait()
		if admitted.Load() != 1 {
			t.Fatalf("scheduled admissions=%d", admitted.Load())
		}
		var stored models.Orchestration
		if err := db.First(&stored, definition.ID).Error; err != nil {
			t.Fatal(err)
		}
		if stored.NextRunAt == nil || stored.NextRunAt.Sub(next) > time.Millisecond || next.Sub(*stored.NextRunAt) > time.Millisecond {
			t.Fatal("schedule advancement not committed")
		}
	})

	t.Run("startup_migration_is_atomic_and_one_way", func(t *testing.T) {
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		// Test the actual module startup revision and SQL migrations inside a rollback fixture.
		if err := tx.Exec("DROP TABLE IF EXISTS orchestrator.startup_schema_revision").Error; err != nil {
			t.Fatal(err)
		}
		legacyPending := execution.TaskExecution{TenantID: 999998, ExecutionID: uuid.NewString(), Module: "orchestrator", TaskType: "orchestration", Source: "orchestrator", Status: "pending", TriggerType: "manual", ExecutionBoundary: "bounded", ExecutionConfig: commonModels.JSONMap{}}
		legacyRunning := legacyPending
		legacyRunning.ExecutionID = uuid.NewString()
		legacyRunning.Status = "running"
		legacyRunning.Metadata = commonModels.JSONMap{"step_results": map[string]interface{}{"old": map[string]interface{}{"status": "success", "result": map[string]interface{}{"execution_id": "old-child"}}}}
		complete := legacyPending
		complete.ExecutionID = uuid.NewString()
		complete.Status = "success"
		frozen := legacyPending
		frozen.ExecutionID = uuid.NewString()
		frozen.ExecutionConfig, _ = freezeExecutionPlan(testSteps())
		for _, item := range []*execution.TaskExecution{&legacyPending, &legacyRunning, &complete, &frozen} {
			if err := tx.Create(item).Error; err != nil {
				t.Fatal(err)
			}
		}
		apply := func(bound *gorm.DB) error {
			if err := bound.AutoMigrate(&models.Orchestration{}); err != nil {
				return err
			}
			if err := ReconcileLegacyExecutions(bound); err != nil {
				return err
			}
			return repository.ApplySQLMigrations(bound)
		}
		forced := errors.New("forced migration failure")
		if err := schema.Migrate(tx, "orchestrator", repository.StartupSchemaVersion, func(bound *gorm.DB) error {
			if err := apply(bound); err != nil {
				return err
			}
			return forced
		}); !errors.Is(err, forced) {
			t.Fatalf("migration rollback=%v", err)
		}
		var before execution.TaskExecution
		if err := tx.First(&before, legacyPending.ID).Error; err != nil {
			t.Fatal(err)
		}
		if before.Status != "pending" {
			t.Fatal("SQL migration committed through outer transaction")
		}
		if err := schema.Migrate(tx, "orchestrator", repository.StartupSchemaVersion, apply); err != nil {
			t.Fatal(err)
		}
		if err := schema.Migrate(tx, "orchestrator", repository.StartupSchemaVersion, func(*gorm.DB) error { t.Error("one-way migration ran again"); return nil }); err != nil {
			t.Fatal(err)
		}
		for _, check := range []struct {
			item         execution.TaskExecution
			status, code string
		}{{legacyPending, "failed", "orchestrator.execution.plan_missing"}, {legacyRunning, "failed", "orchestrator.execution.lease_missing"}, {complete, "success", ""}, {frozen, "pending", ""}} {
			var stored execution.TaskExecution
			if err := tx.First(&stored, check.item.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Status != check.status || (check.code != "" && stored.ErrorDetails["code"] != check.code) {
				t.Fatalf("migration %s -> %s error=%v", check.item.Status, stored.Status, stored.ErrorDetails)
			}
			if check.item.ID == legacyRunning.ID {
				results, _ := readStepResults(stored.Metadata)
				if extractProviderExecutionID(results["old"].Result) != "old-child" {
					t.Fatal("migration erased child facts")
				}
			}
		}
	})
}
