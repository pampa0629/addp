package repository

import (
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/repository"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
)

type StandardReferenceGuardRepository struct{ db *gorm.DB }

func NewStandardReferenceGuardRepository(db *gorm.DB) *StandardReferenceGuardRepository {
	return &StandardReferenceGuardRepository{db: db}
}

func (r *StandardReferenceGuardRepository) SetState(tenantID, resourceID int64, state string) (*commonClient.StandardReferenceGuardResponse, error) {
	response := &commonClient.StandardReferenceGuardResponse{ResourceType: "domain", ResourceID: resourceID, Summary: []commonClient.StandardReferenceImpactSummary{}, Sample: []commonClient.StandardReferenceImpact{}}
	err := r.db.Transaction(func(tx *gorm.DB) error {
		guard, err := repository.LockReferenceGuard(tx, "quality.standard_reference_guards", tenantID, "domain", resourceID)
		if err != nil {
			return err
		}
		if err := repository.SetReferenceGuardState(tx, "quality.standard_reference_guards", guard, state); err != nil {
			return err
		}
		response.State = guard.State
		if state != models.StandardReferenceGuardFrozen {
			return nil
		}
		var count int64
		for _, table := range []string{"quality.rules", "quality.plans"} {
			var n int64
			if err := tx.Table(table).Where("tenant_id = ? AND owner_domain_id = ?", tenantID, resourceID).Count(&n).Error; err != nil {
				return err
			}
			count += n
			if n > 0 {
				kind := "quality_rule"
				if table == "quality.plans" {
					kind = "quality_plan"
				}
				response.Summary = append(response.Summary, commonClient.StandardReferenceImpactSummary{OwnerType: kind, Field: "owner_domain_id", Count: n})
			}
		}
		response.ReferenceCount = count
		response.SampleTruncated = count > int64(len(response.Sample))
		return nil
	})
	return response, err
}
