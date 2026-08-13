package repository

import (
	"errors"

	commonapi "github.com/addp/common/api"
	"gorm.io/gorm"
)

// WrapDBError 将 gorm 错误转换为业务错误，在 repository 层边界调用。
// gorm 错误不应越过 repository 层边界，上层（service/handler）只应看到业务错误。
func WrapDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return commonapi.ErrNotFound
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return commonapi.ErrConflict
	}
	if errors.Is(err, gorm.ErrForeignKeyViolated) {
		return commonapi.ErrConflict
	}
	return err
}
