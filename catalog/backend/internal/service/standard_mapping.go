package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type StandardMappingInput struct {
	CatalogEntryID    uuid.UUID            `json:"catalog_entry_id"`
	ComponentID       uuid.UUID            `json:"component_id"`
	ElementID         int64                `json:"element_id,string" swaggertype:"string"`
	ElementRevisionID int64                `json:"element_revision_id,string" swaggertype:"string"`
	Confidence        *float64             `json:"confidence,omitempty"`
	Evidence          commonModels.JSONMap `json:"evidence"`
	Version           int64                `json:"version,omitempty"`
}

type StandardMappingDecision struct {
	Version int64  `json:"version" minimum:"1"`
	Opinion string `json:"opinion"`
}

type StandardMappingList struct {
	Data       []models.StandardMapping `json:"data"`
	Total      int64                    `json:"total"`
	Page       int                      `json:"page"`
	PageSize   int                      `json:"page_size"`
	TotalPages int                      `json:"total_pages"`
}

type StandardMappingRevisionOption struct {
	ID         string `json:"id"`
	RevisionNo int64  `json:"revision_no"`
	Name       string `json:"name"`
}

func (s *EntryService) resolveStandardMappingDetails(ctx context.Context, tenantID int64, mappings []models.StandardMapping) []StandardMappingDetail {
	details := make([]StandardMappingDetail, len(mappings))
	revisionIDs := make([]int64, 0, len(mappings))
	seenRevisions := make(map[int64]struct{}, len(mappings))
	for index, mapping := range mappings {
		details[index] = StandardMappingDetail{StandardMapping: mapping}
		if mapping.ElementRevisionID == nil {
			details[index].ElementReference.Status = "unfixed"
			continue
		}
		details[index].ElementReference.Status = "unavailable"
		if _, seen := seenRevisions[*mapping.ElementRevisionID]; !seen {
			revisionIDs = append(revisionIDs, *mapping.ElementRevisionID)
			seenRevisions[*mapping.ElementRevisionID] = struct{}{}
		}
	}
	if len(revisionIDs) == 0 || s.elementRevisions == nil {
		return details
	}
	resolved, err := s.elementRevisions.ResolveExactElementRevisions(ctx, tenantID, revisionIDs)
	if err != nil {
		return details
	}
	for index := range details {
		mapping := &details[index]
		if mapping.ElementRevisionID == nil {
			continue
		}
		snapshot := resolved[*mapping.ElementRevisionID]
		if snapshot == nil || snapshot.ElementID != mapping.ElementID || snapshot.RevisionID != *mapping.ElementRevisionID || snapshot.RevisionNo <= 0 || strings.TrimSpace(snapshot.Name) == "" {
			mapping.ElementReference.Status = "missing"
			continue
		}
		mapping.ElementReference = StandardMappingElementReference{
			Status: "resolved", Name: snapshot.Name, Code: snapshot.Code, RevisionNo: snapshot.RevisionNo,
		}
	}
	return details
}

func (s *EntryService) ListStandardMappingRevisionOptions(ctx context.Context, tenantID, elementID int64) ([]StandardMappingRevisionOption, error) {
	if s == nil || tenantID <= 0 || elementID <= 0 {
		return nil, ErrInvalidStandardMapping
	}
	if s.elementRevisions == nil {
		return nil, ErrReferenceValidationUnavailable
	}
	rows, err := s.elementRevisions.ListPublishedElementRevisions(ctx, tenantID, elementID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrReferenceValidationUnavailable, err)
	}
	options := make([]StandardMappingRevisionOption, 0, len(rows))
	for _, row := range rows {
		options = append(options, StandardMappingRevisionOption{ID: strconv.FormatInt(row.ID, 10), RevisionNo: row.RevisionNo, Name: row.Name})
	}
	return options, nil
}

func validateStandardMappingInput(input StandardMappingInput) error {
	if input.CatalogEntryID == uuid.Nil || input.ComponentID == uuid.Nil || input.ElementID <= 0 || input.ElementRevisionID <= 0 ||
		(input.Confidence != nil && (*input.Confidence < 0 || *input.Confidence > 1)) {
		return ErrInvalidStandardMapping
	}
	return nil
}

func validMappingActor(actor UpdateEntryActor) bool {
	return actor.Type == "user" && strings.TrimSpace(actor.ID) != ""
}

func (s *EntryService) resolvePublishedMappingRevision(ctx context.Context, tenantID int64, input StandardMappingInput) error {
	if s.elementRevisions == nil {
		return ErrReferenceValidationUnavailable
	}
	if err := s.elementRevisions.ValidateElementReference(ctx, tenantID, input.ElementID); err != nil {
		if errors.Is(err, commonClient.ErrStandardReferenceDeleting) || errors.Is(err, commonClient.ErrTenantReferenceNotFound) {
			return ErrReferenceNotReferenceable
		}
		return fmt.Errorf("%w: %v", ErrReferenceValidationUnavailable, err)
	}
	resolved, err := s.elementRevisions.ResolveExactElementRevisions(ctx, tenantID, []int64{input.ElementRevisionID})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrReferenceValidationUnavailable, err)
	}
	snapshot := resolved[input.ElementRevisionID]
	if snapshot == nil || snapshot.ElementID != input.ElementID || snapshot.RevisionID != input.ElementRevisionID || snapshot.Status != "published" {
		return ErrReferenceNotReferenceable
	}
	return nil
}

func lockMappingEntry(tx *gorm.DB, tenantID int64, entryID uuid.UUID) error {
	var entry models.Entry
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND id = ?", tenantID, entryID).First(&entry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrEntryNotFound
		}
		return fmt.Errorf("lock StandardMapping entry: %w", err)
	}
	if entry.EntryStatus != models.EntryStatusActive || entry.EntryType != models.EntryTypeDataItem ||
		entry.GovernanceStatus == models.GovernanceStatusCertified || entry.GovernanceStatus == models.GovernanceStatusDeprecated {
		return ErrEntryNotEditable
	}
	return nil
}

func validateMappingComponent(tx *gorm.DB, tenantID int64, entryID, componentID uuid.UUID) error {
	var count int64
	if err := tx.Model(&models.Component{}).
		Where("tenant_id = ? AND catalog_entry_id = ? AND id = ? AND component_status = ?", tenantID, entryID, componentID, models.SourceStatusActive).
		Count(&count).Error; err != nil {
		return fmt.Errorf("validate StandardMapping component: %w", err)
	}
	if count != 1 {
		return ErrReferenceNotReferenceable
	}
	return nil
}

func auditStandardMapping(tx *gorm.DB, tenantID int64, row models.StandardMapping, event string, actor UpdateEntryActor, details commonModels.JSONMap, now time.Time) error {
	if details == nil {
		details = commonModels.JSONMap{}
	}
	details["mapping_id"] = row.ID.String()
	details["component_id"] = row.ComponentID.String()
	details["element_id"] = row.ElementID
	details["element_revision_id"] = row.ElementRevisionID
	details["review_status"] = row.ReviewStatus
	details["version"] = row.Version
	if err := tx.Create(&models.AuditEvent{
		ID: uuid.New(), TenantID: tenantID, CatalogEntryID: row.CatalogEntryID,
		EventType: event, ActorType: actor.Type, ActorID: actor.ID, Details: details, CreatedAt: now,
	}).Error; err != nil {
		return fmt.Errorf("audit StandardMapping: %w", err)
	}
	return nil
}

func (s *EntryService) CreateStandardMapping(ctx context.Context, tenantID int64, input StandardMappingInput, actor UpdateEntryActor) (*models.StandardMapping, error) {
	if s == nil || s.db == nil || tenantID <= 0 || !validMappingActor(actor) || validateStandardMappingInput(input) != nil || input.Version != 0 {
		return nil, ErrInvalidStandardMapping
	}
	if err := s.resolvePublishedMappingRevision(ctx, tenantID, input); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	row := models.StandardMapping{
		ID: uuid.New(), TenantID: tenantID, CatalogEntryID: input.CatalogEntryID, ComponentID: input.ComponentID,
		ElementID: input.ElementID, ElementRevisionID: &input.ElementRevisionID,
		Source: models.StandardMappingSourceManual, Confidence: input.Confidence, Evidence: input.Evidence,
		ReviewStatus: models.StandardMappingProposed, Version: 1,
		ProposedByType: actor.Type, ProposedByID: actor.ID, CreatedAt: now, UpdatedAt: now,
	}
	if row.Evidence == nil {
		row.Evidence = commonModels.JSONMap{}
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockMappingEntry(tx, tenantID, input.CatalogEntryID); err != nil {
			return err
		}
		if err := validateMappingComponent(tx, tenantID, input.CatalogEntryID, input.ComponentID); err != nil {
			return err
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("create StandardMapping: %w", err)
		}
		return auditStandardMapping(tx, tenantID, row, "catalog.standard_mapping.proposed", actor, nil, now)
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *EntryService) GetStandardMapping(ctx context.Context, tenantID int64, access EntryAccess, id uuid.UUID) (*models.StandardMapping, error) {
	if s == nil || s.db == nil || tenantID <= 0 || id == uuid.Nil {
		return nil, ErrInvalidStandardMapping
	}
	var row models.StandardMapping
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrStandardMappingNotFound
		}
		return nil, err
	}
	if _, err := s.Get(ctx, tenantID, access, row.CatalogEntryID); err != nil {
		if errors.Is(err, ErrEntryNotFound) {
			return nil, ErrStandardMappingNotFound
		}
		return nil, err
	}
	return &row, nil
}

func (s *EntryService) ListStandardMappings(ctx context.Context, tenantID int64, access EntryAccess, entryID uuid.UUID, page, pageSize int) (*StandardMappingList, error) {
	if s == nil || s.db == nil || tenantID <= 0 || entryID == uuid.Nil || page < 1 || pageSize < 1 || pageSize > 200 {
		return nil, ErrInvalidPage
	}
	if _, err := s.Get(ctx, tenantID, access, entryID); err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Model(&models.StandardMapping{}).Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, entryID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	rows := make([]models.StandardMapping, 0)
	if err := query.Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	return &StandardMappingList{Data: rows, Total: total, Page: page, PageSize: pageSize, TotalPages: int((total + int64(pageSize) - 1) / int64(pageSize))}, nil
}

func (s *EntryService) UpdateStandardMapping(ctx context.Context, tenantID int64, id uuid.UUID, input StandardMappingInput, actor UpdateEntryActor) (*models.StandardMapping, error) {
	if s == nil || s.db == nil || tenantID <= 0 || id == uuid.Nil || !validMappingActor(actor) || validateStandardMappingInput(input) != nil || input.Version <= 0 {
		return nil, ErrInvalidStandardMapping
	}
	if err := s.resolvePublishedMappingRevision(ctx, tenantID, input); err != nil {
		return nil, err
	}
	var row models.StandardMapping
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockMappingEntry(tx, tenantID, input.CatalogEntryID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, id).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrStandardMappingNotFound
			}
			return err
		}
		if row.CatalogEntryID != input.CatalogEntryID || row.ReviewStatus != models.StandardMappingProposed {
			return ErrStandardMappingStateConflict
		}
		if row.Version != input.Version {
			return ErrStandardMappingVersionConflict
		}
		if err := validateMappingComponent(tx, tenantID, input.CatalogEntryID, input.ComponentID); err != nil {
			return err
		}
		now := time.Now().UTC()
		if input.Evidence == nil {
			input.Evidence = commonModels.JSONMap{}
		}
		nextVersion := row.Version + 1
		if err := tx.Model(&row).Updates(map[string]interface{}{
			"component_id": input.ComponentID, "element_id": input.ElementID, "element_revision_id": input.ElementRevisionID,
			"confidence": input.Confidence, "evidence": input.Evidence, "version": nextVersion, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		row.ComponentID, row.ElementID, row.ElementRevisionID = input.ComponentID, input.ElementID, &input.ElementRevisionID
		row.Confidence, row.Evidence, row.Version, row.UpdatedAt = input.Confidence, input.Evidence, nextVersion, now
		return auditStandardMapping(tx, tenantID, row, "catalog.standard_mapping.updated", actor, nil, now)
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *EntryService) DeleteStandardMapping(ctx context.Context, tenantID int64, id uuid.UUID, version int64, actor UpdateEntryActor) error {
	if s == nil || s.db == nil || tenantID <= 0 || id == uuid.Nil || version <= 0 || !validMappingActor(actor) {
		return ErrInvalidStandardMapping
	}
	var existing models.StandardMapping
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrStandardMappingNotFound
		}
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockMappingEntry(tx, tenantID, existing.CatalogEntryID); err != nil {
			return err
		}
		var row models.StandardMapping
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, id).First(&row).Error; err != nil {
			return err
		}
		if row.Version != version {
			return ErrStandardMappingVersionConflict
		}
		if row.ReviewStatus != models.StandardMappingProposed {
			return ErrStandardMappingStateConflict
		}
		if err := tx.Delete(&row).Error; err != nil {
			return err
		}
		return auditStandardMapping(tx, tenantID, row, "catalog.standard_mapping.deleted", actor, nil, time.Now().UTC())
	})
}

func (s *EntryService) ReviewStandardMapping(ctx context.Context, tenantID int64, id uuid.UUID, action string, input StandardMappingDecision, actor UpdateEntryActor) (*models.StandardMapping, error) {
	if s == nil || s.db == nil || tenantID <= 0 || id == uuid.Nil || input.Version <= 0 || !validMappingActor(actor) ||
		!oneOf(action, "approve", "reject", "withdraw") {
		return nil, ErrInvalidStandardMapping
	}
	input.Opinion = strings.TrimSpace(input.Opinion)
	if action != "approve" && input.Opinion == "" {
		return nil, ErrInvalidStandardMapping
	}
	var existing models.StandardMapping
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrStandardMappingNotFound
		}
		return nil, err
	}
	if action == "approve" {
		if existing.ElementRevisionID == nil {
			return nil, ErrReferenceNotReferenceable
		}
		if err := s.resolvePublishedMappingRevision(ctx, tenantID, StandardMappingInput{CatalogEntryID: existing.CatalogEntryID, ComponentID: existing.ComponentID, ElementID: existing.ElementID, ElementRevisionID: *existing.ElementRevisionID}); err != nil {
			return nil, err
		}
	}
	var row models.StandardMapping
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockMappingEntry(tx, tenantID, existing.CatalogEntryID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, id).First(&row).Error; err != nil {
			return err
		}
		if row.Version != input.Version {
			return ErrStandardMappingVersionConflict
		}
		if (action == "withdraw" && row.ReviewStatus != models.StandardMappingApproved) ||
			(action != "withdraw" && row.ReviewStatus != models.StandardMappingProposed) {
			return ErrStandardMappingStateConflict
		}
		if action == "approve" && (row.ElementRevisionID == nil || existing.ElementRevisionID == nil || *row.ElementRevisionID != *existing.ElementRevisionID || row.ElementID != existing.ElementID) {
			return ErrStandardMappingVersionConflict
		}
		if action == "approve" {
			if err := validateMappingComponent(tx, tenantID, row.CatalogEntryID, row.ComponentID); err != nil {
				return err
			}
			var prior []models.StandardMapping
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND component_id = ? AND review_status = ?", tenantID, row.ComponentID, models.StandardMappingApproved).Find(&prior).Error; err != nil {
				return err
			}
			for _, old := range prior {
				old.ReviewStatus = models.StandardMappingWithdrawn
				old.Version++
				now := time.Now().UTC()
				if err := tx.Model(&old).Updates(map[string]interface{}{"review_status": old.ReviewStatus, "version": old.Version, "reviewed_by_type": actor.Type, "reviewed_by_id": actor.ID, "review_opinion": "superseded by " + row.ID.String(), "reviewed_at": now, "updated_at": now}).Error; err != nil {
					return err
				}
				if err := auditStandardMapping(tx, tenantID, old, "catalog.standard_mapping.withdrawn", actor, commonModels.JSONMap{"superseded_by": row.ID.String()}, now); err != nil {
					return err
				}
			}
		}
		now := time.Now().UTC()
		status := map[string]string{"approve": models.StandardMappingApproved, "reject": models.StandardMappingRejected, "withdraw": models.StandardMappingWithdrawn}[action]
		row.ReviewStatus, row.Version = status, row.Version+1
		row.ReviewedByType, row.ReviewedByID, row.ReviewOpinion, row.ReviewedAt = &actor.Type, &actor.ID, &input.Opinion, &now
		row.UpdatedAt = now
		if err := tx.Model(&row).Updates(map[string]interface{}{"review_status": status, "version": row.Version, "reviewed_by_type": actor.Type, "reviewed_by_id": actor.ID, "review_opinion": input.Opinion, "reviewed_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := auditStandardMapping(tx, tenantID, row, "catalog.standard_mapping."+map[string]string{"approve": "approved", "reject": "rejected", "withdraw": "withdrawn"}[action], actor, commonModels.JSONMap{"opinion": input.Opinion}, now); err != nil {
			return err
		}
		if action != "reject" {
			if err := tx.Create(&models.ProjectionTask{TenantID: tenantID, CatalogEntryID: row.CatalogEntryID, Projection: "search", Status: "pending", AvailableAt: now, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}
