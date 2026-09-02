package repository

import (
	"errors"
	"time"

	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

var ErrInvalidTenantReference = errors.New("invalid tenant resource reference")

// TenantReferenceRepository validates that referenced Standard resources belong
// to the same tenant before a cross-table relation is persisted.
type TenantReferenceRepository struct {
	db *gorm.DB
}

func NewTenantReferenceRepository(db *gorm.DB) *TenantReferenceRepository {
	return &TenantReferenceRepository{db: db}
}

func (r *TenantReferenceRepository) RequireDomain(tenantID int64, id *int64) error {
	return r.requireActiveOne(&models.Domain{}, tenantID, id)
}

func (r *TenantReferenceRepository) RequireUnit(tenantID int64, id *int64) error {
	return r.requireOne(&models.Unit{}, tenantID, id)
}

func (r *TenantReferenceRepository) RequireCodeSet(tenantID int64, id *int64) error {
	return r.requireOne(&models.CodeSet{}, tenantID, id)
}

func (r *TenantReferenceRepository) RequireMetricCategory(tenantID int64, id *int64) error {
	return r.requireOne(&models.MetricCategory{}, tenantID, id)
}

func (r *TenantReferenceRepository) RequireMetric(tenantID int64, id *int64) error {
	return r.requireActiveOne(&models.Metric{}, tenantID, id)
}

func (r *TenantReferenceRepository) RequireElement(tenantID, id int64) error {
	return r.requirePublishedElement(tenantID, id)
}

func (r *TenantReferenceRepository) RequireGlossary(tenantID, id int64) error {
	return r.requireOne(&models.Glossary{}, tenantID, &id)
}

func (r *TenantReferenceRepository) RequireElements(tenantID int64, ids []int64) error {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}
	asOf := time.Now().UTC()
	var count int64
	if err := r.db.Table("standard.elements AS e").
		Joins("JOIN standard.element_revisions er ON er.element_id = e.id AND er.status = ? AND er.effective_from <= ? AND (er.effective_to IS NULL OR er.effective_to > ?)", models.RevisionStatusPublished, asOf, asOf).
		Where("e.tenant_id = ? AND e.lifecycle_state = ? AND e.id IN ?", tenantID, "active", uniqueIDs).
		Distinct("e.id").Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(uniqueIDs)) {
		return ErrInvalidTenantReference
	}
	return nil
}

func (r *TenantReferenceRepository) requirePublishedElement(tenantID, id int64) error {
	asOf := time.Now().UTC()
	var count int64
	if err := r.db.Table("standard.elements AS e").
		Joins("JOIN standard.element_revisions er ON er.element_id = e.id AND er.status = ? AND er.effective_from <= ? AND (er.effective_to IS NULL OR er.effective_to > ?)", models.RevisionStatusPublished, asOf, asOf).
		Where("e.id = ? AND e.tenant_id = ? AND e.lifecycle_state = ?", id, tenantID, "active").
		Distinct("e.id").Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrInvalidTenantReference
	}
	return nil
}

func (r *TenantReferenceRepository) RequireGlossaries(tenantID int64, ids []int64) error {
	return r.requireMany(&models.Glossary{}, tenantID, ids)
}

func (r *TenantReferenceRepository) RequireMetrics(tenantID int64, ids []int64) error {
	return r.requireActiveMany(&models.Metric{}, tenantID, ids)
}

func (r *TenantReferenceRepository) requireActiveOne(model interface{}, tenantID int64, id *int64) error {
	if id == nil {
		return nil
	}
	var count int64
	if err := r.db.Model(model).Where("id = ? AND tenant_id = ? AND lifecycle_state = ?", *id, tenantID, "active").Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrInvalidTenantReference
	}
	return nil
}

func (r *TenantReferenceRepository) requireActiveMany(model interface{}, tenantID int64, ids []int64) error {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}
	var count int64
	if err := r.db.Model(model).
		Where("tenant_id = ? AND lifecycle_state = ? AND id IN ?", tenantID, "active", uniqueIDs).
		Distinct("id").Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(uniqueIDs)) {
		return ErrInvalidTenantReference
	}
	return nil
}

func (r *TenantReferenceRepository) requireOne(model interface{}, tenantID int64, id *int64) error {
	if id == nil {
		return nil
	}
	var count int64
	if err := r.db.Model(model).Where("id = ? AND tenant_id = ?", *id, tenantID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrInvalidTenantReference
	}
	return nil
}

func (r *TenantReferenceRepository) requireMany(model interface{}, tenantID int64, ids []int64) error {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}
	var count int64
	if err := r.db.Model(model).
		Where("tenant_id = ? AND id IN ?", tenantID, uniqueIDs).
		Distinct("id").
		Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(uniqueIDs)) {
		return ErrInvalidTenantReference
	}
	return nil
}

func uniqueInt64s(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
