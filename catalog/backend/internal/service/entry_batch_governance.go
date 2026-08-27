package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	BatchGovernanceMaxEntries                  = 200
	BatchGovernanceAssignPrimaryDomain         = "assign_primary_domain"
	BatchGovernanceAssignAccountableDepartment = "assign_accountable_department"
)

type BatchGovernanceEntryInput struct {
	ID      uuid.UUID `json:"id"`
	Version int64     `json:"version"`
}

type BatchGovernanceInput struct {
	Entries     []BatchGovernanceEntryInput `json:"entries"`
	Operation   string                      `json:"operation"`
	ReferenceID int64                       `json:"reference_id,string" swaggertype:"string"`
}

type BatchGovernanceEntryResult struct {
	ID      uuid.UUID `json:"id"`
	Version int64     `json:"version"`
}

type BatchGovernanceResult struct {
	BatchID uuid.UUID                    `json:"batch_id"`
	Entries []BatchGovernanceEntryResult `json:"entries"`
}

type batchGovernanceTarget struct {
	standard *commonClient.StandardReferenceResolution
	system   *commonClient.SystemCatalogReferenceResolution
}

// BatchGovernance applies one governance relation to an explicit, versioned set of CatalogEntry aggregates.
func (s *EntryService) BatchGovernance(
	ctx context.Context,
	tenantID int64,
	input BatchGovernanceInput,
	actor UpdateEntryActor,
) (*BatchGovernanceResult, error) {
	if s == nil || s.db == nil || tenantID <= 0 || strings.TrimSpace(actor.Type) == "" || strings.TrimSpace(actor.ID) == "" {
		return nil, ErrInvalidBatchGovernance
	}
	versions, ordered, err := validateBatchGovernanceInput(input)
	if err != nil {
		return nil, err
	}
	target, err := s.resolveBatchGovernanceTarget(ctx, tenantID, input.Operation, input.ReferenceID)
	if err != nil {
		return nil, err
	}

	batchID := uuid.New()
	now := time.Now().UTC()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids := make([]uuid.UUID, 0, len(ordered))
		for _, member := range ordered {
			ids = append(ids, member.ID)
		}

		var entries []models.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND id IN ?", tenantID, ids).
			Order("id ASC").Find(&entries).Error; err != nil {
			return fmt.Errorf("lock Catalog batch entries: %w", err)
		}
		if len(entries) != len(ids) {
			return ErrEntryNotFound
		}
		for _, entry := range entries {
			if entry.EntryStatus != models.EntryStatusActive {
				return ErrEntryNotEditable
			}
			if entry.Version != versions[entry.ID] {
				return ErrEntryVersionConflict
			}
		}

		if input.Operation == BatchGovernanceAssignPrimaryDomain {
			if err := rejectOwnerManagedBatchDomains(tx, tenantID, entries); err != nil {
				return err
			}
			if err := replaceBatchPrimaryDomains(tx, tenantID, entries, input.ReferenceID, *target.standard, now); err != nil {
				return err
			}
		} else {
			if err := replaceBatchAccountableDepartments(tx, tenantID, entries, input.ReferenceID, *target.system, now); err != nil {
				return err
			}
			if err := resolveBatchAccountableDepartmentTasks(tx, tenantID, ids, input.ReferenceID, now); err != nil {
				return err
			}
		}

		audits := make([]models.AuditEvent, 0, len(entries))
		projections := make([]models.ProjectionTask, 0, len(entries))
		for _, entry := range entries {
			expectedVersion := versions[entry.ID]
			updated := tx.Model(&models.Entry{}).
				Where("tenant_id = ? AND id = ? AND version = ?", tenantID, entry.ID, expectedVersion).
				Updates(map[string]any{"version": expectedVersion + 1, "updated_at": now})
			if updated.Error != nil {
				return fmt.Errorf("update Catalog batch entry version: %w", updated.Error)
			}
			if updated.RowsAffected != 1 {
				return ErrEntryVersionConflict
			}
			audits = append(audits, models.AuditEvent{
				ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entry.ID,
				EventType: "catalog.entry.batch_governance_applied", ActorType: actor.Type, ActorID: actor.ID,
				Details: commonModels.JSONMap{
					"batch_id": batchID.String(), "operation": input.Operation,
					"reference_id":     fmt.Sprintf("%d", input.ReferenceID),
					"previous_version": expectedVersion, "version": expectedVersion + 1,
				},
				CreatedAt: now,
			})
			projections = append(projections, models.ProjectionTask{
				TenantID: tenantID, CatalogEntryID: entry.ID, Projection: "search", Status: "pending",
				AvailableAt: now, CreatedAt: now, UpdatedAt: now,
			})
		}
		if err := tx.Create(&audits).Error; err != nil {
			return fmt.Errorf("create Catalog batch audit events: %w", err)
		}
		if err := tx.Create(&projections).Error; err != nil {
			return fmt.Errorf("create Catalog batch projection tasks: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	result := &BatchGovernanceResult{BatchID: batchID, Entries: make([]BatchGovernanceEntryResult, 0, len(input.Entries))}
	for _, member := range input.Entries {
		result.Entries = append(result.Entries, BatchGovernanceEntryResult{ID: member.ID, Version: member.Version + 1})
	}
	return result, nil
}

func validateBatchGovernanceInput(input BatchGovernanceInput) (map[uuid.UUID]int64, []BatchGovernanceEntryInput, error) {
	if len(input.Entries) < 1 || len(input.Entries) > BatchGovernanceMaxEntries || input.ReferenceID <= 0 ||
		(input.Operation != BatchGovernanceAssignPrimaryDomain && input.Operation != BatchGovernanceAssignAccountableDepartment) {
		return nil, nil, ErrInvalidBatchGovernance
	}
	versions := make(map[uuid.UUID]int64, len(input.Entries))
	ordered := append([]BatchGovernanceEntryInput(nil), input.Entries...)
	for _, member := range input.Entries {
		if member.ID == uuid.Nil || member.Version <= 0 {
			return nil, nil, ErrInvalidBatchGovernance
		}
		if _, exists := versions[member.ID]; exists {
			return nil, nil, ErrInvalidBatchGovernance
		}
		versions[member.ID] = member.Version
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID.String() < ordered[j].ID.String() })
	return versions, ordered, nil
}

func (s *EntryService) resolveBatchGovernanceTarget(ctx context.Context, tenantID int64, operation string, referenceID int64) (*batchGovernanceTarget, error) {
	if operation == BatchGovernanceAssignPrimaryDomain {
		if s.standard == nil {
			return nil, ErrReferenceValidationUnavailable
		}
		results, err := s.standard.ResolveStandardReferences(ctx, tenantID, []commonClient.StandardReference{{ObjectType: models.SemanticTypeDomain, ID: referenceID}})
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrReferenceValidationUnavailable, err)
		}
		if len(results) != 1 || results[0].ObjectType != models.SemanticTypeDomain || results[0].ID != referenceID {
			return nil, ErrReferenceValidationUnavailable
		}
		if !results[0].Found || !results[0].Referenceable {
			return nil, ErrReferenceNotReferenceable
		}
		return &batchGovernanceTarget{standard: &results[0]}, nil
	}
	if s.system == nil {
		return nil, ErrReferenceValidationUnavailable
	}
	results, err := s.system.ResolveSystemReferences(ctx, tenantID, []commonClient.SystemCatalogReference{{SubjectType: models.ResponsibilitySubjectDepartment, ID: referenceID}})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrReferenceValidationUnavailable, err)
	}
	if len(results) != 1 || results[0].SubjectType != models.ResponsibilitySubjectDepartment || results[0].ID != referenceID {
		return nil, ErrReferenceValidationUnavailable
	}
	if !results[0].Found || !results[0].Referenceable {
		return nil, ErrReferenceNotReferenceable
	}
	return &batchGovernanceTarget{system: &results[0]}, nil
}

func rejectOwnerManagedBatchDomains(tx *gorm.DB, tenantID int64, entries []models.Entry) error {
	ids := make([]uuid.UUID, 0, len(entries))
	entryByID := make(map[uuid.UUID]models.Entry, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
		entryByID[entry.ID] = entry
	}
	var sources []models.SourceBinding
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND catalog_entry_id IN ? AND is_current = ?", tenantID, ids, true).
		Order("catalog_entry_id ASC").Find(&sources).Error; err != nil {
		return fmt.Errorf("lock Catalog batch sources: %w", err)
	}
	if len(sources) != len(entries) {
		return ErrEntryNotEditable
	}
	for _, source := range sources {
		entry := entryByID[source.CatalogEntryID]
		if (source.SourceModule == models.SourceModuleModel &&
			(entry.EntryType == models.EntryTypeBusinessEntity || entry.EntryType == models.EntryTypeLogicalModel)) ||
			(source.SourceModule == models.SourceModuleStandard && entry.EntryType == models.EntryTypeMetric && source.SourceType == models.SourceTypeMetric) {
			return ErrBatchGovernanceUnsupportedEntry
		}
	}
	return nil
}

func replaceBatchPrimaryDomains(tx *gorm.DB, tenantID int64, entries []models.Entry, domainID int64, resolved commonClient.StandardReferenceResolution, now time.Time) error {
	ids := make([]uuid.UUID, 0, len(entries))
	rows := make([]models.SemanticAssociation, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
		rows = append(rows, newSemanticAssociation(tenantID, entry.ID, domainID, models.SemanticTypeDomain, models.SemanticRolePrimary, resolved, now))
	}
	if err := tx.Where("tenant_id = ? AND catalog_entry_id IN ? AND semantic_type = ? AND relation_role = ?", tenantID, ids, models.SemanticTypeDomain, models.SemanticRolePrimary).
		Delete(&models.SemanticAssociation{}).Error; err != nil {
		return fmt.Errorf("replace Catalog batch primary domains: %w", err)
	}
	if err := tx.Create(&rows).Error; err != nil {
		return fmt.Errorf("create Catalog batch primary domains: %w", err)
	}
	return nil
}

func replaceBatchAccountableDepartments(tx *gorm.DB, tenantID int64, entries []models.Entry, departmentID int64, resolved commonClient.SystemCatalogReferenceResolution, now time.Time) error {
	ids := make([]uuid.UUID, 0, len(entries))
	rows := make([]models.Responsibility, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
		rows = append(rows, models.Responsibility{
			ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entry.ID,
			Role: models.ResponsibilityRoleAccountableDepartment, SubjectType: models.ResponsibilitySubjectDepartment,
			SubjectID: departmentID, Status: models.ResponsibilityStatusActive, ObservedSnapshot: systemSnapshot(resolved),
			VerifiedAt: now, CreatedAt: now, UpdatedAt: now,
		})
	}
	if err := tx.Where("tenant_id = ? AND catalog_entry_id IN ? AND role = ?", tenantID, ids, models.ResponsibilityRoleAccountableDepartment).
		Delete(&models.Responsibility{}).Error; err != nil {
		return fmt.Errorf("replace Catalog batch accountable departments: %w", err)
	}
	if err := tx.Create(&rows).Error; err != nil {
		return fmt.Errorf("create Catalog batch accountable departments: %w", err)
	}
	return nil
}

func resolveBatchAccountableDepartmentTasks(tx *gorm.DB, tenantID int64, entryIDs []uuid.UUID, departmentID int64, now time.Time) error {
	var tasks []models.GovernanceTask
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"tenant_id = ? AND catalog_entry_id IN ? AND task_type = ? AND responsibility_role = ? AND status = ?",
		tenantID, entryIDs, models.GovernanceTaskTypeResponsibilityTransfer,
		models.ResponsibilityRoleAccountableDepartment, models.GovernanceTaskStatusOpen,
	).Order("id ASC").Find(&tasks).Error; err != nil {
		return fmt.Errorf("lock Catalog batch governance tasks: %w", err)
	}
	for _, task := range tasks {
		resolution := models.GovernanceTaskResolutionResponsibilityReplaced
		if task.SubjectType == models.ResponsibilitySubjectDepartment && task.SubjectID == departmentID {
			resolution = models.GovernanceTaskResolutionReferenceRestored
		}
		result := tx.Model(&models.GovernanceTask{}).Where("id = ? AND status = ?", task.ID, models.GovernanceTaskStatusOpen).
			Updates(map[string]any{"status": models.GovernanceTaskStatusResolved, "resolved_at": now, "resolution": resolution, "updated_at": now})
		if result.Error != nil {
			return fmt.Errorf("resolve Catalog batch governance task: %w", result.Error)
		}
	}
	return nil
}
