package cleanup

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresCleanupProfessionalResultAtomicity(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_EXECUTION_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("standard Common execution-lifecycle PostgreSQL gate required")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil {
		t.Fatal(err)
	}
	if name != "addp_test" && !(os.Getenv("CI") == "true" && (name == "addp_common_test" || strings.Contains(name, "disposable"))) {
		t.Fatal("unsafe database")
	}
	if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS asset").Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateResults(db, "asset"); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"commit", "terminal_update_rollback", "stale_lease"} {
		t.Run(mode, func(t *testing.T) {
			schema := "cleanup_result_gate_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			if err := db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
					t.Error(err)
				}
			})
			if err := db.Exec("CREATE TABLE " + schema + ".domain_effect(id int primary key)").Error; err != nil {
				t.Fatal(err)
			}
			ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "7", []string{"system.cleanup.execute"}))
			p := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Module: "system", TaskType: "cleanup", Source: "system", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", ExecutionConfig: models.JSONMap{execution.DisplayLanguageKey: "en"}}
			if err := execution.ApplyUserOrigin(ctx, &p); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			input := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: p.ExecutionID, TenantID: 7, RequestedBy: 7, Action: "scan", TriggerType: "manual", ExpectedModules: []string{"asset"}, RequestedAt: now, Deadline: now.Add(time.Minute)}
			if err := input.Seal(); err != nil {
				t.Fatal(err)
			}
			children, err := execution.CreateCleanupExecutions(ctx, db, &p, input)
			if err != nil {
				t.Fatal(err)
			}
			c := children[0]
			ids := []string{p.ExecutionID, c.ExecutionID}
			t.Cleanup(func() {
				for _, table := range []string{"asset.cleanup_results", "common.execution_events", "common.task_executions"} {
					if err := db.Table(table).Where("execution_id IN ?", ids).Delete(map[string]interface{}{}).Error; err != nil {
						t.Error(err)
					}
					var count int64
					if err := db.Table(table).Where("execution_id IN ?", ids).Count(&count).Error; err != nil || count != 0 {
						t.Error("fixture residue", table, count, err)
					}
				}
			})
			identity := execution.PendingIdentity{ExecutionID: c.ExecutionID, TenantID: 7, Module: "asset", TaskType: "cleanup_executor", Source: "system"}
			if err := execution.AttachPendingAuthorization(ctx, db, identity, execution.PendingAuthorization{ID: 1, ExpiresAt: now.Add(time.Hour), ActorPrincipalID: *c.ActorPrincipalID, ActorTenantMembershipID: *c.ActorTenantMembershipID, IssuedAuthorizationVersion: *c.IssuedAuthorizationVersion}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := execution.ClaimPending(ctx, db, execution.PendingIdentity{ExecutionID: p.ExecutionID, TenantID: 7, Module: "system", TaskType: "cleanup", Source: "system"}, execution.ClaimOptions{Module: "system", TaskType: "cleanup", Source: "system", WorkerID: "parent", LeaseDuration: time.Minute}, nil); err != nil {
				t.Fatal(err)
			}
			duration := time.Minute
			if mode == "stale_lease" {
				duration = 100 * time.Millisecond
			}
			child, lease, err := execution.ClaimCleanupChild(ctx, db, identity, execution.ClaimOptions{Module: "asset", TaskType: "cleanup_executor", Source: "system", WorkerID: "owner", LeaseDuration: duration, RequireAuthorization: true}, nil)
			if err != nil || lease == nil {
				t.Fatal("claim", err)
			}
			impact, err := events.BuildCleanupImpactData(nil, "")
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := execution.NewCleanupReceipt(*child, events.CleanupResultData{TaskID: input.RequestID, TenantID: 7, Module: "asset", Action: "scan", Status: "success", Summary: events.CleanupResultSummary{ScannedItems: 1, RiskLevel: "low"}, Impact: &impact}, "")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "terminal_update_rollback" {
				// Inject failure at this exact child's real terminal UPDATE; the lease
				// lifecycle does not itself emit execution events.
				sql := "CREATE FUNCTION " + schema + ".reject_terminal() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.execution_id = '" + c.ExecutionID + "' AND NEW.status = 'success' THEN RAISE EXCEPTION 'fixture terminal update failure'; END IF; RETURN NEW; END; $$; CREATE TRIGGER " + schema + " BEFORE UPDATE ON common.task_executions FOR EACH ROW EXECUTE FUNCTION " + schema + ".reject_terminal();"
				if err := db.Exec(sql).Error; err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := db.Exec("DROP TRIGGER IF EXISTS " + schema + " ON common.task_executions").Error; err != nil {
						t.Error(err)
					}
				})
			}
			if mode == "stale_lease" {
				time.Sleep(150 * time.Millisecond)
			}
			err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if _, err := execution.LockActiveLease(ctx, tx, *lease); err != nil {
					return err
				}
				if err := tx.Exec("INSERT INTO " + schema + ".domain_effect VALUES (1)").Error; err != nil {
					return err
				}
				return Complete(ctx, tx, *lease, receipt, map[string]int64{"assets": 1})
			})
			if (mode == "commit") != (err == nil) {
				t.Fatal("unexpected transaction result", mode, err)
			}
			var actual execution.TaskExecution
			if err := db.Where("execution_id = ?", c.ExecutionID).First(&actual).Error; err != nil {
				t.Fatal(err)
			}
			var effectCount, resultCount int64
			if err := db.Table(schema + ".domain_effect").Count(&effectCount).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Table("asset.cleanup_results").Where("execution_id = ?", c.ExecutionID).Count(&resultCount).Error; err != nil {
				t.Fatal(err)
			}
			got, err := execution.ReadCleanupReceipt(actual)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "commit" {
				if actual.Status != "success" || got == nil || effectCount != 1 || resultCount != 1 {
					t.Fatal("atomic completion missing")
				}
				if err := db.Table("asset.cleanup_results").Where("execution_id = ?", c.ExecutionID).Update("attempt", 2).Error; err == nil {
					t.Fatal("professional result mutable after commit")
				}
				if err := Complete(context.Background(), db, *lease, receipt, map[string]int64{"assets": 99}); err == nil {
					t.Fatal("old owner rewrote result")
				}
			} else if actual.Status != "running" || got != nil || effectCount != 0 || resultCount != 0 {
				t.Fatal("failed transaction committed partial effects")
			}
		})
	}
}
