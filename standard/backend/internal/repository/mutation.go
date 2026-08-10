package repository

import (
	commonrepo "github.com/addp/common/repository"
	"gorm.io/gorm"
)

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
	return commonrepo.WrapDBError(err)
}
