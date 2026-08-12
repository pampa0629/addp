package repository

import (
	"errors"

	commonapi "github.com/addp/common/api"
	commonrepo "github.com/addp/common/repository"
	"gorm.io/gorm"
)

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

func wrapDBError(err error) error {
	if errors.Is(err, gorm.ErrForeignKeyViolated) {
		return commonapi.ErrConflict
	}
	return commonrepo.WrapDBError(err)
}
