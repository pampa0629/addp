package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/addp/catalog/internal/models"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UpdateEntryGovernanceInput struct {
	Version                     int64      `json:"version" binding:"required,gt=0" minimum:"1"`
	GovernanceStatus            string     `json:"governance_status" binding:"required" enums:"curated,certified,deprecated"`
	Reason                      *string    `json:"reason"`
	RecommendedSuccessorEntryID *uuid.UUID `json:"recommended_successor_entry_id"`
}

type UpdateEntryGovernanceAuthorization struct {
	CanCertify   bool
	CanDeprecate bool
}

func (s *EntryService) UpdateGovernance(
	ctx context.Context,
	tenantID int64,
	id uuid.UUID,
	input UpdateEntryGovernanceInput,
	authorization UpdateEntryGovernanceAuthorization,
	actor UpdateEntryActor,
) (*EntryDetail, error) {
	if s == nil || s.db == nil || tenantID <= 0 || id == uuid.Nil || input.Version <= 0 ||
		strings.TrimSpace(actor.Type) == "" || strings.TrimSpace(actor.ID) == "" {
		return nil, ErrInvalidGovernanceUpdate
	}
	normalizeEditableText(&input.Reason)
	if !oneOf(input.GovernanceStatus, models.GovernanceStatusCurated, models.GovernanceStatusCertified, models.GovernanceStatusDeprecated) {
		return nil, ErrInvalidGovernanceUpdate
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var entry models.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND id = ?", tenantID, id).First(&entry).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrEntryNotFound
			}
			return fmt.Errorf("lock Catalog entry governance: %w", err)
		}
		if entry.EntryStatus != models.EntryStatusActive {
			return ErrEntryNotEditable
		}
		if entry.Version != input.Version {
			return ErrEntryVersionConflict
		}

		eventType, err := validateGovernanceUpdate(tx, tenantID, &entry, input, authorization)
		if err != nil {
			return err
		}
		if input.RecommendedSuccessorEntryID != nil {
			if err := validateRecommendedSuccessor(tx, tenantID, id, *input.RecommendedSuccessorEntryID); err != nil {
				return err
			}
		}

		now := time.Now().UTC()
		result := tx.Model(&models.Entry{}).
			Where("tenant_id = ? AND id = ? AND version = ?", tenantID, id, input.Version).
			Updates(map[string]interface{}{
				"governance_status":              input.GovernanceStatus,
				"recommended_successor_entry_id": input.RecommendedSuccessorEntryID,
				"version":                        input.Version + 1,
				"updated_at":                     now,
			})
		if result.Error != nil {
			return fmt.Errorf("update Catalog entry governance: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrEntryVersionConflict
		}

		details := commonModels.JSONMap{
			"previous_version":           input.Version,
			"version":                    input.Version + 1,
			"previous_governance_status": entry.GovernanceStatus,
			"governance_status":          input.GovernanceStatus,
		}
		if input.Reason != nil {
			details["reason"] = *input.Reason
		}
		if entry.RecommendedSuccessorEntryID != nil {
			details["previous_recommended_successor_entry_id"] = entry.RecommendedSuccessorEntryID.String()
		}
		if input.RecommendedSuccessorEntryID != nil {
			details["recommended_successor_entry_id"] = input.RecommendedSuccessorEntryID.String()
		}
		if err := tx.Create(&models.AuditEvent{
			ID: uuid.New(), TenantID: tenantID, CatalogEntryID: id, EventType: eventType,
			ActorType: actor.Type, ActorID: actor.ID, Details: details, CreatedAt: now,
		}).Error; err != nil {
			return fmt.Errorf("create Catalog governance audit event: %w", err)
		}
		if err := tx.Create(&models.ProjectionTask{
			TenantID: tenantID, CatalogEntryID: id, Projection: "search", Status: "pending",
			AvailableAt: now, CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			return fmt.Errorf("create Catalog governance projection task: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, EntryAccess{Inventory: true}, id)
}

func validateGovernanceUpdate(
	tx *gorm.DB,
	tenantID int64,
	entry *models.Entry,
	input UpdateEntryGovernanceInput,
	authorization UpdateEntryGovernanceAuthorization,
) (string, error) {
	current := entry.GovernanceStatus
	next := input.GovernanceStatus
	switch {
	case current == models.GovernanceStatusCurated && next == models.GovernanceStatusCertified:
		if !authorization.CanCertify {
			return "", ErrCertificationPermissionRequired
		}
		if input.Reason != nil || input.RecommendedSuccessorEntryID != nil {
			return "", ErrInvalidGovernanceUpdate
		}
		if err := validateCertificationRequirements(tx, tenantID, *entry); err != nil {
			return "", err
		}
		return "catalog.entry.certified", nil
	case current == models.GovernanceStatusCertified && next == models.GovernanceStatusCurated:
		if !authorization.CanCertify {
			return "", ErrCertificationPermissionRequired
		}
		if input.Reason == nil {
			return "", ErrCertificationWithdrawalReasonRequired
		}
		if input.RecommendedSuccessorEntryID != nil {
			return "", ErrInvalidGovernanceUpdate
		}
		return "catalog.entry.certification_withdrawn", nil
	case (current == models.GovernanceStatusCurated || current == models.GovernanceStatusCertified) && next == models.GovernanceStatusDeprecated:
		if !authorization.CanDeprecate {
			return "", ErrDeprecationPermissionRequired
		}
		if input.Reason == nil {
			return "", ErrDeprecationReasonRequired
		}
		return "catalog.entry.deprecated", nil
	case current == models.GovernanceStatusDeprecated && next == models.GovernanceStatusDeprecated:
		if !authorization.CanDeprecate {
			return "", ErrDeprecationPermissionRequired
		}
		if input.Reason == nil {
			return "", ErrDeprecationReasonRequired
		}
		if equalOptionalUUID(entry.RecommendedSuccessorEntryID, input.RecommendedSuccessorEntryID) {
			return "", ErrInvalidGovernanceUpdate
		}
		return "catalog.entry.deprecation_updated", nil
	default:
		return "", ErrInvalidGovernanceTransition
	}
}

func validateCertificationRequirements(tx *gorm.DB, tenantID int64, entry models.Entry) error {
	if entry.BusinessName == nil || strings.TrimSpace(*entry.BusinessName) == "" ||
		entry.BusinessDescription == nil || strings.TrimSpace(*entry.BusinessDescription) == "" ||
		!oneOf(entry.Visibility, models.VisibilityDepartment, models.VisibilityTenant) {
		return ErrCertificationRequirementsNotMet
	}

	var source models.SourceBinding
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"tenant_id = ? AND catalog_entry_id = ? AND is_current = ?", tenantID, entry.ID, true,
	).First(&source).Error; err != nil {
		return fmt.Errorf("lock Catalog certification source: %w", err)
	}
	if source.SourceStatus != models.SourceStatusActive {
		return ErrCertificationRequirementsNotMet
	}

	ownerPrimaryDomain, err := hasOwnerPrimaryDomain(entry.EntryType, source)
	if err != nil {
		return err
	}
	if !ownerPrimaryDomain {
		var primaryDomainCount int64
		if err := tx.Model(&models.SemanticAssociation{}).Where(
			"tenant_id = ? AND catalog_entry_id = ? AND semantic_type = ? AND relation_role = ?",
			tenantID, entry.ID, models.SemanticTypeDomain, models.SemanticRolePrimary,
		).Count(&primaryDomainCount).Error; err != nil {
			return fmt.Errorf("count Catalog certification primary domains: %w", err)
		}
		if primaryDomainCount != 1 {
			return ErrCertificationRequirementsNotMet
		}
	}

	type responsibilityCount struct {
		Role  string
		Count int64
	}
	var counts []responsibilityCount
	if err := tx.Model(&models.Responsibility{}).
		Select("role, COUNT(*) AS count").
		Where("tenant_id = ? AND catalog_entry_id = ? AND status = ?", tenantID, entry.ID, models.ResponsibilityStatusActive).
		Group("role").Scan(&counts).Error; err != nil {
		return fmt.Errorf("count Catalog certification responsibilities: %w", err)
	}
	byRole := make(map[string]int64, len(counts))
	for _, count := range counts {
		byRole[count.Role] = count.Count
	}
	if byRole[models.ResponsibilityRoleAccountableDepartment] != 1 ||
		byRole[models.ResponsibilityRoleBusinessOwner] != 1 ||
		byRole[models.ResponsibilityRoleDataSteward] < 1 {
		return ErrCertificationRequirementsNotMet
	}
	return nil
}

func hasOwnerPrimaryDomain(entryType string, source models.SourceBinding) (bool, error) {
	if !ownerManagesPrimaryDomain(entryType, source) {
		return false, nil
	}
	ownerPrimaryDomain, valid := observedOwnerPrimaryDomain(source)
	if !valid {
		return false, ErrCertificationRequirementsNotMet
	}
	return ownerPrimaryDomain, nil
}
