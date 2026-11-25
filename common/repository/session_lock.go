package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"time"

	"gorm.io/gorm"
)

// WithSessionLock runs fn on the physical session that owns key. A busy lock
// returns immediately without running fn. The connection is scoped to fn.
func WithSessionLock(ctx context.Context, db *gorm.DB, key string, fn func(*sql.Conn) error) error {
	pool, err := db.DB()
	if err != nil {
		return err
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	acquire, release := "SELECT pg_try_advisory_lock(hashtextextended($1, 0))", "SELECT pg_advisory_unlock(hashtextextended($1, 0))"
	if db.Dialector.Name() == "mysql" {
		// MySQL named locks are server-wide and names are limited to 64 bytes.
		const lockName = "SHA2(CONCAT(DATABASE(), '/', ?), 256)"
		acquire, release = "SELECT GET_LOCK("+lockName+", 0)", "SELECT RELEASE_LOCK("+lockName+")"
	}
	var locked bool
	if err := conn.QueryRowContext(ctx, acquire, key).Scan(&locked); err != nil {
		// The server may have acquired the lock before the reply was lost.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return err
	}
	if !locked {
		return nil
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var released bool
		if err := conn.QueryRowContext(releaseCtx, release, key).Scan(&released); err != nil || !released {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	return fn(conn)
}
