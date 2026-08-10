package repository

import (
	"fmt"

	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

// TenantReferenceRepository validates that referenced Standard resources belong
// to the same tenant before a cross-table relation is persisted.
type TenantReferenceRepository struct {
	db *gorm.DB
}

func NewTenantReferenceRepository(db *gorm.DB) *TenantReferenceRepository {
	return &TenantReferenceRepository{db: db}
}

func (r *TenantReferenceRepository) RequireDomain(tenantID int64, id *int64) error {
	return r.requireOne(&models.Domain{}, tenantID, id, "业务域")
}

func (r *TenantReferenceRepository) RequireUnit(tenantID int64, id *int64) error {
	return r.requireOne(&models.Unit{}, tenantID, id, "计量单位")
}

func (r *TenantReferenceRepository) RequireClassification(tenantID int64, id *int64) error {
	return r.requireOne(&models.Classification{}, tenantID, id, "数据分类")
}

func (r *TenantReferenceRepository) RequireCodeSet(tenantID int64, id *int64) error {
	return r.requireOne(&models.CodeSet{}, tenantID, id, "码值集")
}

func (r *TenantReferenceRepository) RequireMetricCategory(tenantID int64, id *int64) error {
	return r.requireOne(&models.MetricCategory{}, tenantID, id, "指标分类")
}

func (r *TenantReferenceRepository) RequireMetric(tenantID int64, id *int64) error {
	return r.requireOne(&models.Metric{}, tenantID, id, "指标")
}

func (r *TenantReferenceRepository) RequireElement(tenantID, id int64) error {
	return r.requireOne(&models.Element{}, tenantID, &id, "数据元")
}

func (r *TenantReferenceRepository) RequireGlossary(tenantID, id int64) error {
	return r.requireOne(&models.Glossary{}, tenantID, &id, "业务术语")
}

func (r *TenantReferenceRepository) RequireElements(tenantID int64, ids []int64) error {
	return r.requireMany(&models.Element{}, tenantID, ids, "数据元")
}

func (r *TenantReferenceRepository) RequireGlossaries(tenantID int64, ids []int64) error {
	return r.requireMany(&models.Glossary{}, tenantID, ids, "业务术语")
}

func (r *TenantReferenceRepository) RequireMetrics(tenantID int64, ids []int64) error {
	return r.requireMany(&models.Metric{}, tenantID, ids, "指标")
}

func (r *TenantReferenceRepository) requireOne(model interface{}, tenantID int64, id *int64, label string) error {
	if id == nil {
		return nil
	}
	var count int64
	if err := r.db.Model(model).Where("id = ? AND tenant_id = ?", *id, tenantID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("%s不存在或不属于当前租户", label)
	}
	return nil
}

func (r *TenantReferenceRepository) requireMany(model interface{}, tenantID int64, ids []int64, label string) error {
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
		return fmt.Errorf("存在不存在或不属于当前租户的%s", label)
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
