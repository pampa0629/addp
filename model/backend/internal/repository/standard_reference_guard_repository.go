package repository

import (
	"errors"
	"sort"
	"time"

	commonrepo "github.com/addp/common/repository"
	"github.com/addp/model/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrStandardReferenceFrozen        = errors.New("standard reference is frozen")
	ErrStandardReferenceGuardTerminal = errors.New("standard reference guard is terminal")
)

type StandardReferenceGuardRepository struct {
	db *gorm.DB
}

func NewStandardReferenceGuardRepository(db *gorm.DB) *StandardReferenceGuardRepository {
	return &StandardReferenceGuardRepository{db: db}
}

func (r *StandardReferenceGuardRepository) DB() *gorm.DB { return r.db }

func validStandardResourceType(resourceType string) bool {
	switch resourceType {
	case models.StandardResourceDomain, models.StandardResourceElement,
		models.StandardResourceDimensionHierarchy, models.StandardResourceMetric:
		return true
	default:
		return false
	}
}

func LockStandardReferences(db *gorm.DB, tenantID int64, references ...models.StandardReference) error {
	unique := make(map[models.StandardReference]struct{}, len(references))
	ordered := make([]models.StandardReference, 0, len(references))
	for _, reference := range references {
		if reference.ResourceID <= 0 || !validStandardResourceType(reference.ResourceType) {
			continue
		}
		if _, exists := unique[reference]; exists {
			continue
		}
		unique[reference] = struct{}{}
		ordered = append(ordered, reference)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].ResourceType == ordered[j].ResourceType {
			return ordered[i].ResourceID < ordered[j].ResourceID
		}
		return ordered[i].ResourceType < ordered[j].ResourceType
	})
	for _, reference := range ordered {
		guard, err := lockStandardReferenceGuard(db, tenantID, reference.ResourceType, reference.ResourceID)
		if err != nil {
			return err
		}
		if guard.State != models.StandardReferenceGuardOpen {
			return ErrStandardReferenceFrozen
		}
	}
	return nil
}

func lockStandardReferenceGuard(db *gorm.DB, tenantID int64, resourceType string, resourceID int64) (*models.StandardReferenceGuard, error) {
	guard := &models.StandardReferenceGuard{
		TenantID: tenantID, ResourceType: resourceType, ResourceID: resourceID,
		State: models.StandardReferenceGuardOpen,
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(guard).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id = ?", tenantID, resourceType, resourceID).
		First(guard).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return guard, nil
}

func (r *StandardReferenceGuardRepository) SetState(tenantID int64, resourceType string, resourceID int64, desiredState string) (*models.StandardReferenceGuardResponse, error) {
	response := &models.StandardReferenceGuardResponse{
		ResourceType: resourceType, ResourceID: resourceID,
		Summary: []models.StandardReferenceImpactSummary{}, Sample: []models.StandardReferenceImpact{},
	}
	err := r.db.Transaction(func(tx *gorm.DB) error {
		guard, err := lockStandardReferenceGuard(tx, tenantID, resourceType, resourceID)
		if err != nil {
			return err
		}
		switch desiredState {
		case models.StandardReferenceGuardOpen:
			if guard.State == models.StandardReferenceGuardDeleted {
				return ErrStandardReferenceGuardTerminal
			}
		case models.StandardReferenceGuardFrozen:
			if guard.State == models.StandardReferenceGuardDeleted {
				return ErrStandardReferenceGuardTerminal
			}
		case models.StandardReferenceGuardDeleted:
			if guard.State != models.StandardReferenceGuardFrozen && guard.State != models.StandardReferenceGuardDeleted {
				return ErrStandardReferenceGuardTerminal
			}
		default:
			return gorm.ErrInvalidValue
		}
		if guard.State != desiredState {
			if err := tx.Model(&models.StandardReferenceGuard{}).Where("id = ?", guard.ID).
				Updates(map[string]interface{}{"state": desiredState, "updated_at": time.Now()}).Error; err != nil {
				return commonrepo.WrapDBError(err)
			}
			guard.State = desiredState
		}
		response.State = guard.State
		if desiredState == models.StandardReferenceGuardFrozen {
			return scanStandardReferenceImpact(tx, tenantID, resourceType, resourceID, response)
		}
		return nil
	})
	return response, err
}

type standardReferenceScan struct {
	OwnerType string
	Table     string
	Join      string
	Tenant    string
	Field     string
	Column    string
}

func standardReferenceScans(resourceType string) []standardReferenceScan {
	switch resourceType {
	case models.StandardResourceDomain:
		return []standardReferenceScan{
			{OwnerType: "entity", Table: "model.entities AS owner", Tenant: "owner.tenant_id", Field: "domain_id", Column: "owner.domain_id"},
			{OwnerType: "logical_table", Table: "model.logical_tables AS owner", Tenant: "owner.tenant_id", Field: "domain_id", Column: "owner.domain_id"},
		}
	case models.StandardResourceElement:
		return []standardReferenceScan{
			{OwnerType: "entity_attribute", Table: "model.entity_attributes AS owner", Join: "JOIN model.entities AS parent ON parent.id = owner.entity_id", Tenant: "parent.tenant_id", Field: "element_id", Column: "owner.element_id"},
			{OwnerType: "logical_field", Table: "model.logical_fields AS owner", Join: "JOIN model.logical_tables AS parent ON parent.id = owner.table_id", Tenant: "parent.tenant_id", Field: "element_id", Column: "owner.element_id"},
		}
	case models.StandardResourceDimensionHierarchy:
		return []standardReferenceScan{
			{OwnerType: "logical_field", Table: "model.logical_fields AS owner", Join: "JOIN model.logical_tables AS parent ON parent.id = owner.table_id", Tenant: "parent.tenant_id", Field: "hierarchy_id", Column: "owner.hierarchy_id"},
		}
	case models.StandardResourceMetric:
		return []standardReferenceScan{
			{OwnerType: "fact_metric_mapping", Table: "model.fact_metric_mappings AS owner", Tenant: "owner.tenant_id", Field: "metric_id", Column: "owner.metric_id"},
		}
	default:
		return nil
	}
}

func scanStandardReferenceImpact(db *gorm.DB, tenantID int64, resourceType string, resourceID int64, response *models.StandardReferenceGuardResponse) error {
	const sampleLimit = 100
	remaining := sampleLimit
	for _, scan := range standardReferenceScans(resourceType) {
		var count int64
		query := db.Table(scan.Table)
		if scan.Join != "" {
			query = query.Joins(scan.Join)
		}
		if err := query.Where(scan.Tenant+" = ? AND "+scan.Column+" = ?", tenantID, resourceID).Count(&count).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if count == 0 {
			continue
		}
		response.ReferenceCount += count
		response.Summary = append(response.Summary, models.StandardReferenceImpactSummary{OwnerType: scan.OwnerType, Field: scan.Field, Count: count})
		if remaining <= 0 {
			continue
		}
		var ids []int64
		query = db.Table(scan.Table)
		if scan.Join != "" {
			query = query.Joins(scan.Join)
		}
		if err := query.Select("owner.id").Where(scan.Tenant+" = ? AND "+scan.Column+" = ?", tenantID, resourceID).
			Order("owner.id ASC").Limit(remaining).Scan(&ids).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		for _, id := range ids {
			response.Sample = append(response.Sample, models.StandardReferenceImpact{OwnerType: scan.OwnerType, OwnerID: id, Field: scan.Field})
		}
		remaining -= len(ids)
	}
	response.SampleTruncated = response.ReferenceCount > int64(len(response.Sample))
	return nil
}
