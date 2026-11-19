package engineaccess

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/system/internal/migration"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestSystemAccessRequestLockAgainstMySQL(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_SYSTEM_REQUEST_LOCK") != "1" {
		t.Skip("run the disposable platform-system-request MySQL gate")
	}
	dialect, err := platformmysql.Dialector(database.Config{
		Host: "127.0.0.1", Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"),
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
	if err := migration.MigrateMySQLAccessRequestLocks(db); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	repo := NewRepository(db)
	id, other := uuid.New(), uuid.New()
	rollback := errors.New("roll back access request")
	waiting := make(chan error, 1)
	contended := false
	err = repo.transaction(ctx, func(first *Repository) error {
		if err := first.lockAccessRequest(ctx, id); err != nil {
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
			return otherTx.lockAccessRequest(ctx, other)
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
	err = db.Table("system.engine_access_request_locks").Where("request_id = ? OR request_id = ?", id[:], other[:]).Count(&count).Error
	if err != nil || count != 2 {
		t.Fatalf("request lock row count=%d: %v", count, err)
	}
}
