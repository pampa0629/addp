package repository

import (
	"errors"

	commonapi "github.com/addp/common/api"
	commonrepo "github.com/addp/common/repository"
	"gorm.io/gorm"
)

var ErrVersionConflict = errors.New("resource version conflict")

func deleteInTransaction(db *gorm.DB, model interface{}, query string, args ...interface{}) error {
	return wrapDBError(db.Transaction(func(tx *gorm.DB) error {
		return requireAffectedRow(tx.Where(query, args...).Delete(model))
	}))
}

func requireAffectedRow(result *gorm.DB) error {
	if result.Error != nil {
		return wrapDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return wrapDBError(gorm.ErrRecordNotFound)
	}
	return nil
}

func updateVersioned(db *gorm.DB, model interface{}, id, tenantID, expectedVersion int64, updates map[string]interface{}) error {
	updates["version"] = gorm.Expr("version + 1")
	result := db.Model(model).
		Where("id = ? AND tenant_id = ? AND version = ?", id, tenantID, expectedVersion).
		Updates(updates)
	if result.Error != nil {
		return wrapDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrVersionConflict
	}
	return nil
}

func wrapDBError(err error) error {
	if errors.Is(err, gorm.ErrForeignKeyViolated) {
		return commonapi.ErrConflict
	}
	return commonrepo.WrapDBError(err)
}
