package repository

import (
	"errors"
	"fmt"
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

func (r *TenantReferenceRepository) RequireMetricDefinition(tenantID, id int64) error {
	return r.requireActiveOne(&models.MetricDefinition{}, tenantID, &id)
}

func (r *TenantReferenceRepository) RequireMetric(tenantID int64, id *int64) error {
	return r.requireActiveOne(&models.MetricDefinition{}, tenantID, id)
}

func (r *TenantReferenceRepository) RequireMetrics(tenantID int64, ids []int64) error {
	return r.requireActiveMany(&models.MetricDefinition{}, tenantID, ids)
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

// ResolveCollectionMembers 校验标准集成员属于当前租户，并返回最小显示摘要。
// 标准集绑定稳定身份，因此数据元和码值集不要求已经发布。
func (r *TenantReferenceRepository) ResolveCollectionMembers(tenantID int64, inputs []models.StandardCollectionMemberInput) ([]models.StandardCollectionMember, error) {
	seen := make(map[string]struct{}, len(inputs))
	idsByType := make(map[string][]int64)
	for _, input := range inputs {
		key := fmt.Sprintf("%s:%d", input.MemberType, input.MemberID)
		if input.MemberID <= 0 {
			return nil, ErrInvalidTenantReference
		}
		if _, exists := seen[key]; exists {
			return nil, ErrInvalidTenantReference
		}
		seen[key] = struct{}{}
		idsByType[input.MemberType] = append(idsByType[input.MemberType], input.MemberID)
	}
	for memberType, ids := range idsByType {
		var err error
		switch memberType {
		case models.CollectionMemberElement:
			err = r.requireActiveMany(&models.Element{}, tenantID, ids)
		case models.CollectionMemberCodeSet:
			err = r.requireActiveMany(&models.CodeSet{}, tenantID, ids)
		case models.CollectionMemberMetric:
			err = r.requireActiveMany(&models.MetricDefinition{}, tenantID, ids)
		case models.CollectionMemberGlossary:
			err = r.requireMany(&models.Glossary{}, tenantID, ids)
		case models.CollectionMemberDocument:
			err = r.requireMany(&models.Document{}, tenantID, ids)
		default:
			return nil, ErrInvalidTenantReference
		}
		if err != nil {
			return nil, err
		}
	}

	type summary struct {
		ID         int64
		Name, Code string
	}
	summaries := make(map[string]summary, len(inputs))
	load := func(memberType string, query *gorm.DB) error {
		var rows []summary
		if err := query.Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			summaries[fmt.Sprintf("%s:%d", memberType, row.ID)] = row
		}
		return nil
	}
	if ids := idsByType[models.CollectionMemberElement]; len(ids) > 0 {
		if err := load(models.CollectionMemberElement, r.db.Raw(`SELECT e.id, e.code,
			COALESCE((SELECT er.name FROM standard.element_revisions er WHERE er.element_id=e.id ORDER BY CASE er.status WHEN 'draft' THEN 0 WHEN 'in_review' THEN 1 WHEN 'published' THEN 2 ELSE 3 END, er.revision_no DESC LIMIT 1), e.code) AS name
			FROM standard.elements e WHERE e.tenant_id=? AND e.id IN ?`, tenantID, ids)); err != nil {
			return nil, err
		}
	}
	if ids := idsByType[models.CollectionMemberCodeSet]; len(ids) > 0 {
		if err := load(models.CollectionMemberCodeSet, r.db.Raw(`SELECT c.id, c.code,
			COALESCE((SELECT cr.name FROM standard.code_set_revisions cr WHERE cr.code_set_id=c.id ORDER BY CASE cr.status WHEN 'draft' THEN 0 WHEN 'in_review' THEN 1 WHEN 'published' THEN 2 ELSE 3 END, cr.revision_no DESC LIMIT 1), c.code) AS name
			FROM standard.code_sets c WHERE c.tenant_id=? AND c.id IN ?`, tenantID, ids)); err != nil {
			return nil, err
		}
	}
	if ids := idsByType[models.CollectionMemberMetric]; len(ids) > 0 {
		if err := load(models.CollectionMemberMetric, r.db.Raw(`SELECT m.id, m.code,
			COALESCE((SELECT mr.name FROM standard.metric_definition_revisions mr WHERE mr.metric_definition_id=m.id ORDER BY CASE mr.status WHEN 'draft' THEN 0 WHEN 'in_review' THEN 1 WHEN 'published' THEN 2 ELSE 3 END, mr.revision_no DESC LIMIT 1), m.code) AS name
			FROM standard.metric_definitions m WHERE m.tenant_id=? AND m.id IN ?`, tenantID, ids)); err != nil {
			return nil, err
		}
	}
	if ids := idsByType[models.CollectionMemberGlossary]; len(ids) > 0 {
		if err := load(models.CollectionMemberGlossary, r.db.Raw(`SELECT g.id, g.code,
			COALESCE((SELECT gr.name FROM standard.glossary_revisions gr WHERE gr.glossary_id=g.id ORDER BY CASE gr.status WHEN 'draft' THEN 0 WHEN 'in_review' THEN 1 WHEN 'published' THEN 2 ELSE 3 END, gr.revision_no DESC LIMIT 1), g.code) AS name
			FROM standard.glossaries g WHERE g.tenant_id=? AND g.id IN ?`, tenantID, ids)); err != nil {
			return nil, err
		}
	}
	if ids := idsByType[models.CollectionMemberDocument]; len(ids) > 0 {
		if err := load(models.CollectionMemberDocument, r.db.Raw(`SELECT d.id, d.code,
			COALESCE((SELECT dr.name FROM standard.document_revisions dr WHERE dr.document_id=d.id ORDER BY CASE dr.status WHEN 'draft' THEN 0 WHEN 'in_review' THEN 1 WHEN 'published' THEN 2 ELSE 3 END, dr.revision_no DESC LIMIT 1), d.code) AS name
			FROM standard.documents d WHERE d.tenant_id=? AND d.id IN ?`, tenantID, ids)); err != nil {
			return nil, err
		}
	}
	result := make([]models.StandardCollectionMember, 0, len(inputs))
	for _, input := range inputs {
		item := summaries[fmt.Sprintf("%s:%d", input.MemberType, input.MemberID)]
		result = append(result, models.StandardCollectionMember{MemberType: input.MemberType, MemberID: input.MemberID, Name: item.Name, Code: item.Code})
	}
	return result, nil
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
