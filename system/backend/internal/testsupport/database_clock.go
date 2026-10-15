package testsupport

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// FixtureDatabaseClock anchors disposable membership/role fixture timestamps
// to IAM's authority clock. It is not a production authorization clock.
func FixtureDatabaseClock(ctx context.Context, db *gorm.DB) (func() time.Time, error) {
	var now time.Time
	if err := db.WithContext(ctx).Raw("SELECT statement_timestamp()").Scan(&now).Error; err != nil {
		return nil, err
	}
	started := time.Now()
	return func() time.Time { return now.UTC().Add(time.Since(started)) }, nil
}
