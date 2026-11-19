package execution

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

func executionClockSQL(db *gorm.DB) string {
	switch db.Dialector.Name() {
	case "postgres":
		return "clock_timestamp()"
	case "mysql":
		return "CURRENT_TIMESTAMP(6)"
	default:
		// SQLite fixtures have no database clock with the required precision.
		return ""
	}
}

// leaseClockValue is evaluated in a statement issued after locking the row.
func leaseClockValue(db *gorm.DB, sampled time.Time) any {
	if clock := executionClockSQL(db); clock != "" {
		return gorm.Expr(clock)
	}
	return sampled.UTC()
}

// DatabaseNow samples the database clock after ownership locks are acquired.
func DatabaseNow(db *gorm.DB) (time.Time, error) {
	if db == nil {
		return time.Time{}, fmt.Errorf("execution database is required")
	}
	clock := executionClockSQL(db)
	if clock == "" {
		return time.Now().UTC(), nil
	}
	var now time.Time
	err := db.Session(&gorm.Session{NewDB: true}).Raw("SELECT " + clock).Scan(&now).Error
	return now.UTC(), err
}

func ownedActiveExecution(db *gorm.DB, lease Lease, now time.Time) *gorm.DB {
	query := ownedExecution(db, lease)
	return query.Where("lease_expires_at > ?", leaseClockValue(db, now))
}

func ownedExpiredExecution(db *gorm.DB, lease Lease, now time.Time) *gorm.DB {
	query := ownedExecution(db, lease)
	return query.Where("lease_expires_at <= ?", leaseClockValue(db, now))
}
