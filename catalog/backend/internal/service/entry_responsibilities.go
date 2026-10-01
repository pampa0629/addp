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

// Responsibility transfer is the only responsibility-only write route, and
// applies only while deprecated. Other states retain their curation contract.
type TransferEntryResponsibilitiesInput struct {
	Version          int64                 `json:"version" binding:"required,gt=0" minimum:"1"`
	Reason           string                `json:"reason" binding:"required"`
	Responsibilities []ResponsibilityInput `json:"responsibilities" binding:"required,min=3,max=200,dive"`
}

func (s *EntryService) TransferResponsibilities(ctx context.Context, tenantID int64, id uuid.UUID,
	access EntryAccess, input TransferEntryResponsibilitiesInput, actor UpdateEntryActor,
) (*EntryDetail, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if s == nil || s.db == nil || tenantID <= 0 || id == uuid.Nil || input.Version <= 0 ||
		input.Reason == "" || strings.TrimSpace(actor.Type) == "" || strings.TrimSpace(actor.ID) == "" {
		return nil, ErrInvalidEntryUpdate
	}
	if err := validateResponsibilityInputs(input.Responsibilities); err != nil {
		return nil, err
	}
	counts := make(map[string]int)
	for _, item := range input.Responsibilities {
		counts[item.Role]++
	}
	if counts[models.ResponsibilityRoleAccountableDepartment] != 1 ||
		counts[models.ResponsibilityRoleBusinessOwner] != 1 || counts[models.ResponsibilityRoleDataSteward] < 1 {
		return nil, ErrInvalidEntryUpdate
	}
	if _, err := s.Get(ctx, tenantID, access, id); err != nil {
		return nil, err
	}
	// Validate current System subjects before taking the local transaction lock;
	// frozen Standard facts are deliberately neither resolved nor rewritten.
	refs, err := s.resolveUpdateReferences(ctx, tenantID, UpdateEntryInput{Responsibilities: input.Responsibilities})
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var entry models.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, id).First(&entry).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrEntryNotFound
			}
			return err
		}
		if entry.Version != input.Version {
			return ErrEntryVersionConflict
		}
		if entry.EntryStatus != models.EntryStatusActive || entry.GovernanceStatus != models.GovernanceStatusDeprecated {
			return ErrEntryNotEditable
		}
		var previous []models.Responsibility
		if err := tx.Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, id).Order("role, subject_id").Find(&previous).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := replaceResponsibilities(tx, tenantID, id, input.Responsibilities, refs, now); err != nil {
			return err
		}
		if err := resolveSupersededResponsibilityTasks(tx, tenantID, id, input.Responsibilities, now); err != nil {
			return err
		}
		result := tx.Model(&models.Entry{}).Where("tenant_id = ? AND id = ? AND version = ?", tenantID, id, input.Version).
			Updates(map[string]interface{}{"version": input.Version + 1, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrEntryVersionConflict
		}
		previousSubjects := make([]commonModels.JSONMap, 0, len(previous))
		for _, item := range previous {
			previousSubjects = append(previousSubjects, commonModels.JSONMap{
				"role": item.Role, "subject_type": item.SubjectType, "subject_id": fmt.Sprint(item.SubjectID), "status": item.Status,
			})
		}
		if err := tx.Create(&models.AuditEvent{
			ID: uuid.New(), TenantID: tenantID, CatalogEntryID: id,
			EventType: "catalog.entry.responsibilities_transferred", ActorType: actor.Type, ActorID: actor.ID,
			Details: commonModels.JSONMap{"previous_version": input.Version, "version": input.Version + 1,
				"reason": input.Reason, "previous_responsibilities": previousSubjects, "responsibilities": input.Responsibilities,
				"governance_status": entry.GovernanceStatus, "visibility": entry.Visibility}, CreatedAt: now,
		}).Error; err != nil {
			return err
		}
		return tx.Create(&models.ProjectionTask{TenantID: tenantID, CatalogEntryID: id,
			Projection: "search", Status: "pending", AvailableAt: now, CreatedAt: now, UpdatedAt: now}).Error
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, EntryAccess{Inventory: true}, id)
}
