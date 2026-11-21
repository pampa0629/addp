package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/common/production"
	"github.com/addp/system/internal/migration"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func systemLockTestDB(t *testing.T, user, password string) *gorm.DB {
	t.Helper()
	if os.Getenv("ADDP_TEST_MYSQL_SYSTEM_REQUEST_LOCK") != "1" {
		t.Skip("run the disposable platform-system-request MySQL gate")
	}
	dialect, err := platformmysql.Dialector(database.Config{
		Host: "127.0.0.1", Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
		User: user, Password: password,
		DBName: "system", ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(dialect, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	return db
}

func TestSystemAccessRequestLockAgainstMySQL(t *testing.T) {
	db := systemLockTestDB(t, os.Getenv("ADDP_TEST_MYSQL_USER"), os.Getenv("ADDP_TEST_MYSQL_PASSWORD"))
	if err := migration.MigrateMySQLAccessRequestLocks(db); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	repo := NewRepository(db)
	ref := production.Reference{Owner: "transfer", ExecutionID: uuid.NewString(), ResultID: uuid.NewString()}
	uploadID, fingerprint := uuid.NewString(), testFulfillmentRequest().ItemFingerprint
	id, other := productionGrantCommand(7, ref), uploadGrantCommand(7, uploadID, fingerprint)
	rollback := errors.New("roll back access request")
	waiting := make(chan error, 1)
	contended := false
	err := repo.transaction(ctx, func(first *Repository) error {
		if err := first.lockInitialProduction(ctx, 7, ref); err != nil {
			return err
		}
		go func() {
			waiting <- repo.transaction(ctx, func(second *Repository) error {
				return second.lockAccessRequest(ctx, id)
			})
		}()
		timeout := time.NewTimer(4 * time.Second)
		defer timeout.Stop()
		tick := time.NewTicker(25 * time.Millisecond)
		defer tick.Stop()
		for !contended {
			select {
			case err := <-waiting:
				return errors.Join(errors.New("duplicate request finished before the outer transaction"), err)
			case <-timeout.C:
				return errors.New("MySQL did not observe a row lock wait for the duplicate request")
			case <-tick.C:
				var count int
				err := db.WithContext(ctx).Raw(`SELECT COUNT(*)
					FROM performance_schema.data_lock_waits AS w
					JOIN performance_schema.data_locks AS l ON l.ENGINE_LOCK_ID = w.REQUESTING_ENGINE_LOCK_ID
					WHERE l.OBJECT_SCHEMA = 'system' AND l.OBJECT_NAME = 'engine_access_request_locks'`).Scan(&count).Error
				if err != nil {
					return err
				}
				contended = count == 1
			}
		}
		if err := repo.transaction(ctx, func(otherTx *Repository) error {
			return otherTx.lockInitialUpload(ctx, 7, uploadID, fingerprint)
		}); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	select {
	case err := <-waiting:
		if err != nil {
			t.Fatalf("same request could not continue after rollback: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var count int64
	err = db.Table("system.engine_access_request_locks").Where("request_id IN (UUID_TO_BIN(?), UUID_TO_BIN(?))", id.String(), other.String()).Count(&count).Error
	if err != nil || count != 2 {
		t.Fatalf("request lock row count=%d: %v", count, err)
	}
}

func TestSystemAccessTargetLockAgainstMySQL(t *testing.T) {
	admin := systemLockTestDB(t, os.Getenv("ADDP_TEST_MYSQL_USER"), os.Getenv("ADDP_TEST_MYSQL_PASSWORD"))
	for _, migrate := range []func(*gorm.DB) error{migration.MigrateMySQLAccessRequestLocks, migration.MigrateMySQLAccessTargetLocks} {
		if err := migrate(admin); err != nil {
			t.Fatal(err)
		}
	}
	const runtime = "target_lock_system"
	password := uuid.NewString()
	pool, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.ExecContext(t.Context(), "CREATE USER 'target_lock_system'@'%' IDENTIFIED BY '"+password+"'"); err != nil {
		t.Fatal(err)
	}
	for _, grant := range []string{
		"GRANT INSERT (target_key), UPDATE (lock_marker) ON system.engine_access_target_locks TO 'target_lock_system'@'%'",
		"GRANT INSERT, UPDATE, SELECT (request_id) ON system.engine_access_request_locks TO 'target_lock_system'@'%'",
	} {
		if err := admin.Exec(grant).Error; err != nil {
			t.Fatal(err)
		}
	}
	db := systemLockTestDB(t, runtime, password)
	runtimePool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	runtimePool.SetMaxOpenConns(2)
	path, err := json.Marshal(testFulfillmentRequest().Path)
	if err != nil {
		t.Fatal(err)
	}
	otherPath, err := json.Marshal(engineplugin.TabularItemPath(12, "schema", "业务.域", "another"))
	if err != nil {
		t.Fatal(err)
	}
	for _, finish := range []string{"commit", "rollback"} {
		t.Run(finish, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			first := db.WithContext(ctx).Begin()
			if first.Error != nil {
				t.Fatal(first.Error)
			}
			defer first.Rollback()
			holder := NewRepository(first)
			if err := holder.lockAccessRequest(ctx, uuid.New()); err != nil {
				t.Fatal(err)
			}
			if err := holder.lockFulfillmentTarget(ctx, 7, path); err != nil {
				t.Fatal(err)
			}
			second := db.WithContext(ctx).Begin()
			if second.Error != nil {
				t.Fatal(second.Error)
			}
			defer second.Rollback()
			contender := NewRepository(second)
			if err := contender.lockAccessRequest(ctx, uuid.New()); err != nil {
				t.Fatal(err)
			}
			if err := contender.lockFulfillmentTarget(ctx, 7, otherPath); err != nil {
				t.Fatalf("unrelated target blocked: %v", err)
			}
			var waiterID, holderID int64
			if err := first.Raw("SELECT CONNECTION_ID()").Scan(&holderID).Error; err != nil {
				t.Fatal(err)
			}
			if err := second.Raw("SELECT CONNECTION_ID()").Scan(&waiterID).Error; err != nil {
				t.Fatal(err)
			}
			if waiterID == holderID {
				t.Fatalf("expected separate application connections, got %d", holderID)
			}
			done := make(chan error, 1)
			go func() { done <- contender.lockFulfillmentTarget(ctx, 7, path) }()
			// The observer is a separate administrative connection. The two
			// application connections have no access to performance_schema.
			tick := time.NewTicker(20 * time.Millisecond)
			defer tick.Stop()
			for waiting := false; !waiting; {
				select {
				case err := <-done:
					t.Fatalf("same target returned before %s: %v", finish, err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-tick.C:
					// Only these two transactions touch the target table in this
					// disposable database. Observe the storage-engine wait directly.
					var waits int
					err := admin.WithContext(ctx).Raw(`SELECT COUNT(*)
						FROM performance_schema.data_lock_waits w
						JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID
						WHERE l.OBJECT_SCHEMA='system' AND l.OBJECT_NAME='engine_access_target_locks'
						  AND w.REQUESTING_ENGINE_TRANSACTION_ID <> w.BLOCKING_ENGINE_TRANSACTION_ID`).Scan(&waits).Error
					if err != nil {
						t.Fatal(err)
					}
					waiting = waits > 0
				}
			}
			var err error
			if finish == "commit" {
				err = first.Commit().Error
			} else {
				err = first.Rollback().Error
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatalf("same target after %s: %v", finish, err)
			}
			if err := second.Commit().Error; err != nil {
				t.Fatal(err)
			}
			t.Logf("connections %d and %d: independent target acquired, same target waited until %s", holderID, waiterID, finish)
		})
	}
}
