package repository

import (
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

var ErrReferenceGuardTerminal = errors.New("reference guard is terminal")

// ReferenceGuard is a consumer-local serialization fence, not an owner resource.
// Table is supplied only by trusted module code; callers must hold a transaction.
type ReferenceGuard struct {
	ID           int64 `gorm:"primaryKey"`
	TenantID     int64
	ResourceType string
	ResourceID   int64
	State        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func LockReferenceGuard(tx *gorm.DB, table string, tenantID int64, kind string, id int64) (*ReferenceGuard, error) {
	row := &ReferenceGuard{TenantID: tenantID, ResourceType: kind, ResourceID: id, State: "open"}
	if err := tx.Table(table).Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error; err != nil {
		return nil, WrapDBError(err)
	}
	if err := tx.Table(table).Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND resource_type = ? AND resource_id = ?", tenantID, kind, id).First(row).Error; err != nil {
		return nil, WrapDBError(err)
	}
	return row, nil
}

func SetReferenceGuardState(tx *gorm.DB, table string, row *ReferenceGuard, desired string) error {
	switch desired {
	case "open", "frozen":
		if row.State == "deleted" {
			return ErrReferenceGuardTerminal
		}
	case "deleted":
		if row.State != "frozen" && row.State != "deleted" {
			return ErrReferenceGuardTerminal
		}
	default:
		return gorm.ErrInvalidValue
	}
	if row.State == desired {
		return nil
	}
	if err := tx.Table(table).Where("id = ?", row.ID).Updates(map[string]interface{}{"state": desired, "updated_at": time.Now()}).Error; err != nil {
		return WrapDBError(err)
	}
	row.State = desired
	return nil
}
