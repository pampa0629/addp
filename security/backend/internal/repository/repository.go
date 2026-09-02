package repository

import (
	"errors"

	commonapi "github.com/addp/common/api"
	commonrepo "github.com/addp/common/repository"
	"gorm.io/gorm"
)

var ErrVersionConflict = errors.New("resource version conflict")

type Repository[T any] struct{ db *gorm.DB }

func New[T any](db *gorm.DB) *Repository[T] { return &Repository[T]{db: db} }

func (r *Repository[T]) List(tenantID int64) ([]T, error) {
	var rows []T
	err := r.db.Where("tenant_id = ?", tenantID).Order("id ASC").Find(&rows).Error
	return rows, commonrepo.WrapDBError(err)
}

func (r *Repository[T]) Get(id, tenantID int64) (*T, error) {
	var row T
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&row).Error
	return &row, commonrepo.WrapDBError(err)
}

func (r *Repository[T]) Create(row *T) error {
	return commonrepo.WrapDBError(r.db.Create(row).Error)
}

func (r *Repository[T]) CountWhere(tenantID int64, query string, args ...interface{}) (int64, error) {
	var count int64
	err := r.db.Model(new(T)).Where("tenant_id = ?", tenantID).Where(query, args...).Count(&count).Error
	return count, commonrepo.WrapDBError(err)
}

func (r *Repository[T]) Update(id, tenantID, version int64, values map[string]interface{}) error {
	values["version"] = gorm.Expr("version + 1")
	result := r.db.Model(new(T)).Where("id = ? AND tenant_id = ? AND version = ?", id, tenantID, version).Updates(values)
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := r.db.Model(new(T)).Where("id = ? AND tenant_id = ?", id, tenantID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return commonapi.ErrNotFound
		}
		return ErrVersionConflict
	}
	return nil
}

func (r *Repository[T]) Delete(id, tenantID int64) error {
	result := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(new(T))
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return commonapi.ErrNotFound
	}
	return nil
}

func IsConflict(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey) || errors.Is(err, commonapi.ErrConflict)
}
