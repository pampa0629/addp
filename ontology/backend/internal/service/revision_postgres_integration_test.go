package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/execution"
	"github.com/addp/common/schema"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The fixture owns only a previously absent ontology schema. It never resets
// common, a business database, or somebody else's pre-existing ontology data.
func openTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("ONTOLOGY_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("ONTOLOGY_POSTGRES_TEST_DSN not set (T2 only)")
	}
	if _, err := uuid.Parse(os.Getenv("ONTOLOGY_POSTGRES_TEST_RUN_ID")); err != nil {
		t.Fatal("use the standard ontology-postgres gate to allocate a run ID")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	if (config.Host != "localhost" && config.Host != "127.0.0.1" && config.Host != "::1") ||
		(config.Database != "addp_test" && !(os.Getenv("GITHUB_ACTIONS") == "true" && config.Database == "addp_ontology_test")) {
		t.Fatal("unsafe PostgreSQL test target")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("cannot connect to test PostgreSQL")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(8)
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	var acquired bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(hashtextextended('addp.test.ontology',0))").Scan(&acquired); err != nil || !acquired {
		t.Fatal("another Ontology test gate is running")
	}
	t.Cleanup(func() {
		_, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(hashtextextended('addp.test.ontology',0))")
		if err != nil {
			t.Error(err)
		}
	})
	var database string
	if err := db.Raw("SELECT current_database()").Scan(&database).Error; err != nil || database != config.Database {
		t.Fatal("database identity mismatch")
	}
	return db
}

func fixtureMarker() string {
	return "addp.ontology-test:" + os.Getenv("ONTOLOGY_POSTGRES_TEST_RUN_ID")
}

func postgresFixture(t *testing.T) *gorm.DB {
	t.Helper()
	return postgresFixtureMigration(t, repository.Migrate)
}

func postgresFixtureMigration(t *testing.T, migrate func(*gorm.DB) error) *gorm.DB {
	t.Helper()
	db := openTestDatabase(t)
	var exists bool
	if err := db.Raw("SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname='ontology')").Scan(&exists).Error; err != nil || exists {
		t.Fatal("refusing to reset existing ontology schema")
	}
	// Test setup calls the real shared owner initializer. Production Ontology
	// calls schema.Require only; it never migrates the shared store itself.
	if err := schema.InitializeCommon(db); err != nil {
		t.Fatal(err)
	}
	// Create and tag together so the shell exit trap can prove ownership even
	// if go test is interrupted before migration or its normal Cleanup runs.
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("CREATE SCHEMA ontology").Error; err != nil {
			return err
		}
		return tx.Exec("COMMENT ON SCHEMA ontology IS '" + fixtureMarker() + "'").Error
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupFixture(t, db) })
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func cleanupFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cleanupCancel()
	cleanup := db.WithContext(cleanupCtx)
	var marker string
	result := cleanup.Raw("SELECT COALESCE(obj_description(oid,'pg_namespace'),'') FROM pg_namespace WHERE nspname='ontology'").Scan(&marker)
	if result.Error != nil {
		t.Error(result.Error)
		return
	}
	if result.RowsAffected == 0 {
		return
	}
	if marker != fixtureMarker() {
		t.Error("refusing cleanup of another owner or run's ontology schema")
		return
	}
	var present bool
	if err := cleanup.Raw("SELECT to_regclass('ontology.revisions') IS NOT NULL").Scan(&present).Error; err != nil {
		t.Error(err)
		return
	}
	if present {
		var ids []string
		if err := cleanup.Model(&models.Revision{}).Where("build_execution_id IS NOT NULL").Pluck("build_execution_id::text", &ids).Error; err != nil {
			t.Error(err)
			return
		}
		var hasProjections bool
		if err := cleanup.Raw("SELECT to_regclass('ontology.projections') IS NOT NULL").Scan(&hasProjections).Error; err != nil {
			t.Error(err)
			return
		}
		if hasProjections {
			var projectionIDs []string
			if err := cleanup.Model(&models.Projection{}).Pluck("execution_id::text", &projectionIDs).Error; err != nil {
				t.Error(err)
				return
			}
			ids = append(ids, projectionIDs...)
		}
		if len(ids) > 0 {
			if err := cleanup.Where("module = ? AND execution_id IN ?", models.Module, ids).Delete(&execution.TaskExecution{}).Error; err != nil {
				t.Error(err)
				return
			}
			var remaining int64
			if err := cleanup.Model(&execution.TaskExecution{}).Where("execution_id IN ?", ids).Count(&remaining).Error; err != nil || remaining != 0 {
				t.Error("execution cleanup failed")
				return
			}
		}
	}
	if err := cleanup.Exec("DROP SCHEMA IF EXISTS ontology CASCADE").Error; err != nil {
		t.Error(err)
		return
	}
	if err := cleanup.Raw("SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname='ontology')").Scan(&present).Error; err != nil || present {
		t.Error("schema cleanup failed")
	}
}

func TestPostgresGateCleanup(t *testing.T) {
	cleanupFixture(t, openTestDatabase(t))
}

func TestPostgresRevisionLifecycle(t *testing.T) {
	db := postgresFixture(t)
	s := NewRevisionService(repository.NewRevisionRepository(db))
	ctx := context.Background()
	actor := testActor(101)
	t.Run("management_lists", func(t *testing.T) { testManagementLists(t, s) })
	t.Run("projection_runtime", func(t *testing.T) { testProjectionRuntime(t, db, s, actor) })
	t.Run("projection_rebuild", func(t *testing.T) { testProjectionRebuild(t, db, s, actor) })
	t.Run("projection_admission", func(t *testing.T) { testProjectionAdmission(t, db, s, actor) })
	t.Run("management_reads_are_tenant_scoped", func(t *testing.T) {
		def := testDefinition("management_reads")
		r, err := s.CreateDraft(ctx, actor, def)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.LatestProjection(ctx, actor, def.Scope); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal("draft has projection", err)
		}
		r, err = s.Transition(ctx, actor, def.Scope, r.Version, "submit")
		if err != nil {
			t.Fatal(err)
		}
		r, err = s.Transition(ctx, actor, def.Scope, r.Version, "publish")
		if err != nil {
			t.Fatal(err)
		}
		head, err := s.Head(ctx, actor, def.Scope.OntologyID)
		if err != nil || head.LastRevision != 1 || head.ActivationVersion != 1 || head.ActiveRevision != nil {
			t.Fatalf("head %+v %v", head, err)
		}
		p, err := s.Projection(ctx, actor, def.Scope.OntologyID, *r.Generation)
		if err != nil || p.Status != "pending" || p.ExecutionID != *r.BuildExecutionID {
			t.Fatalf("projection %+v %v", p, err)
		}
		latest, err := s.LatestProjection(ctx, actor, def.Scope)
		if err != nil || latest.Generation != p.Generation {
			t.Fatalf("latest %+v %v", latest, err)
		}
		otherScope := def.Scope
		otherScope.TenantID = 102
		if _, err := s.LatestProjection(ctx, testActor(102), otherScope); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal("cross-tenant latest visible", err)
		}
		if _, err := s.Head(ctx, testActor(102), def.Scope.OntologyID); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal("cross-tenant head visible", err)
		}
		if _, err := s.Projection(ctx, testActor(102), def.Scope.OntologyID, *r.Generation); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal("cross-tenant projection visible", err)
		}
		if _, err := s.Projection(ctx, actor, "other", *r.Generation); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal("cross-ontology projection visible", err)
		}
		if _, err := s.Projection(ctx, actor, def.Scope.OntologyID, "invalid"); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal("invalid generation accepted", err)
		}
	})
	newID := func() string { return "it_" + strings.ReplaceAll(uuid.NewString(), "-", "") }
	transition := func(t *testing.T, r *models.Revision, action string) *models.Revision {
		t.Helper()
		scope := testDefinition(r.OntologyID).Scope
		scope.Revision = r.Revision
		next, err := s.Transition(ctx, actor, scope, r.Version, action)
		if err != nil {
			t.Fatal(err)
		}
		return next
	}
	t.Run("migration_idempotent_and_version_guard", func(t *testing.T) {
		if err := repository.Migrate(db); err != nil {
			t.Fatal(err)
		}
		if err := schema.Require(db, "ontology", repository.SchemaVersion); err != nil {
			t.Fatal(err)
		}
		if err := schema.Migrate(db, "ontology", repository.SchemaVersion+1, func(tx *gorm.DB) error {
			if err := tx.Exec("CREATE TABLE ontology.failed_migration_probe(id BIGINT)").Error; err != nil {
				return err
			}
			return errors.New("injected migration failure")
		}); err == nil {
			t.Fatal("failed migration accepted")
		}
		if err := schema.Require(db, "ontology", repository.SchemaVersion); err != nil {
			t.Fatal("failed migration changed revision", err)
		}
		var present bool
		if err := db.Raw("SELECT to_regclass('ontology.failed_migration_probe') IS NOT NULL").Scan(&present).Error; err != nil || present {
			t.Fatal("failed migration kept DDL", err)
		}
		tx := db.Begin()
		defer tx.Rollback()
		if err := tx.Exec("UPDATE ontology.startup_schema_revision SET version=?", repository.SchemaVersion+1).Error; err != nil {
			t.Fatal(err)
		}
		if err := repository.Migrate(tx); err == nil {
			t.Fatal("schema downgrade accepted")
		}
	})
	t.Run("full_lifecycle_and_isolation", func(t *testing.T) {
		d := testDefinition(newID())
		r, err := s.CreateDraft(ctx, actor, d)
		if err != nil {
			t.Fatal(err)
		}
		d.Classes[0].Name = "北京户外活动观察（已编辑）"
		r, err = s.SaveDraft(ctx, actor, r.Version, d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.SaveDraft(ctx, actor, 1, d); !errors.Is(err, repository.ErrConflict) {
			t.Fatal(err)
		}
		r = transition(t, r, "submit")
		if _, err = s.SaveDraft(ctx, actor, r.Version, d); !errors.Is(err, repository.ErrConflict) {
			t.Fatal(err)
		}
		r = transition(t, r, "return")
		r = transition(t, r, "submit")
		r = transition(t, r, "publish")
		if r.Status != models.Published || r.BuildExecutionID == nil || r.Generation == nil {
			t.Fatalf("publication: %+v", r)
		}
		var job execution.TaskExecution
		if err := db.Where("execution_id = ?", r.BuildExecutionID).First(&job).Error; err != nil {
			t.Fatal(err)
		}
		if job.Status != "pending" || job.ExecutionAuthorizationID != nil || job.ExecutionConfig["revision"] != "1" || job.ExecutionConfig["digest"] != r.Digest || job.ActorPrincipalID == nil || *job.ActorPrincipalID != actor.PrincipalID {
			t.Fatalf("invalid build intent: %+v", job)
		}
		var claimed *execution.TaskExecution
		if err := db.Transaction(func(tx *gorm.DB) error {
			var err error
			claimed, _, err = execution.ClaimNext(ctx, tx, execution.ClaimOptions{Module: models.Module, TaskType: models.ProjectionTaskType, WorkerID: "fixture", LeaseDuration: time.Minute, RequireAuthorization: true})
			return err
		}); err != nil || claimed != nil {
			t.Fatal("unadmitted intent was claimed", err)
		}
		if err := db.Exec("UPDATE ontology.revisions SET payload='{}', version=version+1 WHERE tenant_id=? AND ontology_id=? AND revision=1", actor.TenantID, d.Scope.OntologyID).Error; err == nil {
			t.Fatal("published payload mutable")
		}
		if err := db.Exec("UPDATE ontology.revision_events SET digest=? WHERE ontology_id=?", strings.Repeat("0", 64), d.Scope.OntologyID).Error; err == nil {
			t.Fatal("audit mutable")
		}
		other := d.Scope
		other.TenantID = 102
		if _, err := s.Get(ctx, testActor(102), other); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal("tenant read escaped", err)
		}
		if _, err := s.Transition(ctx, testActor(102), other, r.Version, "withdraw"); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal("tenant write escaped", err)
		}
		d.Scope.Revision = 2
		if _, err := s.CreateDraft(ctx, actor, d); err != nil {
			t.Fatal(err)
		}
		r = transition(t, r, "withdraw")
		if err := db.Where("execution_id = ?", r.BuildExecutionID).First(&job).Error; err != nil || job.Status != "cancelled" {
			t.Fatal("pending intent not canceled", err)
		}
		old := d.Scope
		old.Revision = 1
		if saved, err := s.Get(ctx, actor, old); err != nil || saved.Payload != r.Payload || saved.Status != models.Withdrawn {
			t.Fatal("history unavailable", err)
		}
		if _, err := s.Transition(ctx, actor, old, r.Version, "publish"); !errors.Is(err, repository.ErrConflict) {
			t.Fatal("withdrawn reactivated", err)
		}
		var events int64
		if err := db.Model(&models.RevisionEvent{}).Where("ontology_id=? AND revision=1", d.Scope.OntologyID).Count(&events).Error; err != nil || events != int64(r.Version) {
			t.Fatal("incomplete audit", events, err)
		}
	})
	t.Run("parallel_draft_edits_and_publication", func(t *testing.T) {
		d := testDefinition(newID())
		r, err := s.CreateDraft(ctx, actor, d)
		if err != nil {
			t.Fatal(err)
		}
		parallel := func(operation func() error) {
			t.Helper()
			start := make(chan struct{})
			out := make(chan error, 2)
			var wg sync.WaitGroup
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); <-start; out <- operation() }()
			}
			close(start)
			wg.Wait()
			close(out)
			success, conflicts := 0, 0
			for err := range out {
				if err == nil {
					success++
				} else if errors.Is(err, repository.ErrConflict) {
					conflicts++
				} else {
					t.Fatal(err)
				}
			}
			if success != 1 || conflicts != 1 {
				t.Fatalf("success=%d conflicts=%d", success, conflicts)
			}
		}
		parallel(func() error { _, err := s.SaveDraft(ctx, actor, r.Version, d); return err })
		r, err = s.Get(ctx, actor, d.Scope)
		if err != nil {
			t.Fatal(err)
		}
		r = transition(t, r, "submit")
		parallel(func() error { _, err := s.Transition(ctx, actor, d.Scope, r.Version, "publish"); return err })
		var jobs int64
		if err := db.Model(&execution.TaskExecution{}).Where("module=? AND execution_config->>'ontology_id'=?", models.Module, d.Scope.OntologyID).Count(&jobs).Error; err != nil || jobs != 1 {
			t.Fatal("duplicate jobs", jobs, err)
		}
	})
	t.Run("publication_failure_rolls_back_all_facts", func(t *testing.T) {
		d := testDefinition(newID())
		r, err := s.CreateDraft(ctx, actor, d)
		if err != nil {
			t.Fatal(err)
		}
		r = transition(t, r, "submit")
		injected := errors.New("injected audit failure")
		if err := db.Callback().Create().Before("gorm:create").Register("ontology_test_audit_failure", func(tx *gorm.DB) {
			if event, ok := tx.Statement.Dest.(*models.RevisionEvent); ok && event.Action == "publish" {
				tx.AddError(injected)
			}
		}); err != nil {
			t.Fatal(err)
		}
		_, err = s.Transition(ctx, actor, d.Scope, r.Version, "publish")
		removeErr := db.Callback().Create().Remove("ontology_test_audit_failure")
		if removeErr != nil {
			t.Fatal(removeErr)
		}
		if !errors.Is(err, injected) {
			t.Fatal("fault did not reach transaction", err)
		}
		after, err := s.Get(ctx, actor, d.Scope)
		if err != nil || after.Status != models.InReview || after.Version != r.Version || after.BuildExecutionID != nil {
			t.Fatal("partial publication", err)
		}
		var count int64
		if err := db.Model(&execution.TaskExecution{}).Where("module=? AND execution_config->>'ontology_id'=?", models.Module, d.Scope.OntologyID).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("orphan build intent", count, err)
		}
		transition(t, r, "publish")
	})
	t.Run("sequence_and_database_constraints", func(t *testing.T) {
		d := testDefinition(newID())
		r, err := s.CreateDraft(ctx, actor, d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateDraft(ctx, actor, d); !errors.Is(err, repository.ErrConflict) {
			t.Fatal(err)
		}
		d.Scope.Revision = 2
		if _, err := s.CreateDraft(ctx, actor, d); !errors.Is(err, repository.ErrConflict) {
			t.Fatal("two working revisions", err)
		}
		if err := db.Exec("UPDATE ontology.revisions SET status='published', version=version+1 WHERE ontology_id=?", d.Scope.OntologyID).Error; err == nil {
			t.Fatal("bypassed review and build intent")
		}
		if err := db.Exec("DELETE FROM ontology.revisions WHERE ontology_id=?", d.Scope.OntologyID).Error; err == nil {
			t.Fatal("history deleted")
		}
		d.Scope.Revision = 1
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := s.SaveDraft(cancelCtx, actor, r.Version, d); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if got, err := s.Get(ctx, actor, d.Scope); err != nil || got.Version != r.Version {
			t.Fatal("canceled command wrote", err)
		}
	})
	t.Run("withdrawal_failure_preserves_published_intent", func(t *testing.T) {
		d := testDefinition(newID())
		r, err := s.CreateDraft(ctx, actor, d)
		if err != nil {
			t.Fatal(err)
		}
		r = transition(t, r, "submit")
		r = transition(t, r, "publish")
		injected := errors.New("withdrawal audit failure")
		if err := db.Callback().Create().Before("gorm:create").Register("ontology_test_withdraw_failure", func(tx *gorm.DB) {
			if event, ok := tx.Statement.Dest.(*models.RevisionEvent); ok && event.Action == "withdraw" {
				tx.AddError(injected)
			}
		}); err != nil {
			t.Fatal(err)
		}
		_, err = s.Transition(ctx, actor, d.Scope, r.Version, "withdraw")
		if e := db.Callback().Create().Remove("ontology_test_withdraw_failure"); e != nil {
			t.Fatal(e)
		}
		if !errors.Is(err, injected) {
			t.Fatal(err)
		}
		got, err := s.Get(ctx, actor, d.Scope)
		if err != nil || got.Status != models.Published || got.Version != r.Version {
			t.Fatal("partial withdrawal", err)
		}
		var job execution.TaskExecution
		if err := db.Where("execution_id=?", r.BuildExecutionID).First(&job).Error; err != nil || job.Status != execution.ExecutionStatusPending {
			t.Fatal("cancellation escaped rollback", err)
		}
	})
}
