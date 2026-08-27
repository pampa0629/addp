package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GovernanceTaskListFilter struct {
	Status   string
	EntryID  uuid.UUID
	Page     int
	PageSize int
}

type GovernanceTaskSummary struct {
	models.GovernanceTask
	EntryDisplayName string `json:"entry_display_name"`
	EntryVersion     int64  `json:"entry_version"`
}

type GovernanceTaskListResult struct {
	Data       []GovernanceTaskSummary `json:"data"`
	Total      int64                   `json:"total"`
	Page       int                     `json:"page"`
	PageSize   int                     `json:"page_size"`
	TotalPages int                     `json:"total_pages"`
}

type GovernanceTaskService struct {
	db       *gorm.DB
	resolver SystemReferenceResolver
	now      func() time.Time
}

func NewGovernanceTaskService(db *gorm.DB, resolver SystemReferenceResolver) *GovernanceTaskService {
	return &GovernanceTaskService{db: db, resolver: resolver, now: time.Now}
}

func (s *GovernanceTaskService) List(ctx context.Context, tenantID int64, filter GovernanceTaskListFilter) (*GovernanceTaskListResult, error) {
	if s == nil || s.db == nil || tenantID <= 0 || filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 200 ||
		(filter.Status != models.GovernanceTaskStatusOpen && filter.Status != models.GovernanceTaskStatusResolved) {
		return nil, ErrInvalidPage
	}
	query := s.db.WithContext(ctx).Table("catalog.governance_tasks AS task").Where("task.tenant_id = ? AND task.status = ?", tenantID, filter.Status)
	if filter.EntryID != uuid.Nil {
		query = query.Where("task.catalog_entry_id = ?", filter.EntryID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count Catalog governance tasks: %w", err)
	}
	type taskRow struct {
		models.GovernanceTask
		BusinessName     *string
		EntryVersion     int64
		ObservedSnapshot commonModels.JSONMap
	}
	var rows []taskRow
	err := query.Select("task.*, entry.business_name, entry.version AS entry_version, source.observed_snapshot").
		Joins("JOIN catalog.entries AS entry ON entry.tenant_id = task.tenant_id AND entry.id = task.catalog_entry_id").
		Joins("LEFT JOIN catalog.source_bindings AS source ON source.tenant_id = task.tenant_id AND source.catalog_entry_id = task.catalog_entry_id AND source.is_current = ?", true).
		Order("task.opened_at DESC, task.id ASC").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list Catalog governance tasks: %w", err)
	}
	data := make([]GovernanceTaskSummary, 0, len(rows))
	for _, row := range rows {
		displayName, _ := row.ObservedSnapshot["name"].(string)
		if row.BusinessName != nil && *row.BusinessName != "" {
			displayName = *row.BusinessName
		}
		data = append(data, GovernanceTaskSummary{
			GovernanceTask: row.GovernanceTask, EntryDisplayName: displayName, EntryVersion: row.EntryVersion,
		})
	}
	return &GovernanceTaskListResult{Data: data, Total: total, Page: filter.Page, PageSize: filter.PageSize, TotalPages: totalPages(total, filter.PageSize)}, nil
}

func (s *GovernanceTaskService) ReconcileTenant(ctx context.Context, tenantID int64) error {
	if s == nil || s.db == nil || s.resolver == nil || tenantID <= 0 {
		return ErrReferenceValidationUnavailable
	}
	var responsibilities []models.Responsibility
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND status IN ?", tenantID, []string{
		models.ResponsibilityStatusActive, models.ResponsibilityStatusNeedsTransfer,
	}).Order("catalog_entry_id ASC, id ASC").Find(&responsibilities).Error; err != nil {
		return fmt.Errorf("list Catalog responsibilities for reconciliation: %w", err)
	}
	for start := 0; start < len(responsibilities); start += 200 {
		end := start + 200
		if end > len(responsibilities) {
			end = len(responsibilities)
		}
		batch := responsibilities[start:end]
		references := make([]commonClient.SystemCatalogReference, 0, len(batch))
		for _, responsibility := range batch {
			references = append(references, commonClient.SystemCatalogReference{SubjectType: responsibility.SubjectType, ID: responsibility.SubjectID})
		}
		resolved, err := s.resolver.ResolveSystemReferences(ctx, tenantID, references)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrReferenceValidationUnavailable, err)
		}
		if err := validateResponsibilityResolutionBatch(references, resolved); err != nil {
			return err
		}
		if err := s.applyResponsibilityResolutionBatch(ctx, tenantID, batch, resolved); err != nil {
			return err
		}
	}
	return nil
}

func validateResponsibilityResolutionBatch(references []commonClient.SystemCatalogReference, resolved []commonClient.SystemCatalogReferenceResolution) error {
	if len(references) != len(resolved) {
		return ErrReferenceValidationUnavailable
	}
	for index, result := range resolved {
		if result.SubjectType != references[index].SubjectType || result.ID != references[index].ID {
			return ErrReferenceValidationUnavailable
		}
	}
	return nil
}

func (s *GovernanceTaskService) applyResponsibilityResolutionBatch(
	ctx context.Context,
	tenantID int64,
	batch []models.Responsibility,
	resolved []commonClient.SystemCatalogReferenceResolution,
) error {
	entryIDs := uniqueSortedEntryIDs(batch)
	responsibilityIDs := make([]uuid.UUID, 0, len(batch))
	for _, responsibility := range batch {
		responsibilityIDs = append(responsibilityIDs, responsibility.ID)
	}
	now := s.now().UTC()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var entries []models.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id IN ?", tenantID, entryIDs).
			Order("id ASC").Find(&entries).Error; err != nil {
			return fmt.Errorf("lock Catalog entries for responsibility reconciliation: %w", err)
		}
		var currentRows []models.Responsibility
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id IN ?", tenantID, responsibilityIDs).
			Order("catalog_entry_id ASC, id ASC").Find(&currentRows).Error; err != nil {
			return fmt.Errorf("lock Catalog responsibilities for reconciliation: %w", err)
		}
		currentByID := make(map[uuid.UUID]models.Responsibility, len(currentRows))
		for _, current := range currentRows {
			currentByID[current.ID] = current
		}
		changedEntries := make(map[uuid.UUID][]commonModels.JSONMap)
		for index, observed := range batch {
			current, exists := currentByID[observed.ID]
			if !exists || current.SubjectType != observed.SubjectType || current.SubjectID != observed.SubjectID || current.Role != observed.Role {
				continue
			}
			result := resolved[index]
			snapshot := mergeResponsibilitySnapshot(current.ObservedSnapshot, result)
			targetStatus := models.ResponsibilityStatusActive
			if !result.Found || !result.Referenceable {
				targetStatus = models.ResponsibilityStatusNeedsTransfer
			}
			if err := tx.Model(&models.Responsibility{}).Where("tenant_id = ? AND id = ?", tenantID, current.ID).
				Updates(map[string]interface{}{"status": targetStatus, "observed_snapshot": snapshot, "verified_at": now, "updated_at": now}).Error; err != nil {
				return fmt.Errorf("update reconciled Catalog responsibility: %w", err)
			}
			if targetStatus == models.ResponsibilityStatusNeedsTransfer {
				reason := models.GovernanceTaskReasonSubjectNotReferenceable
				if !result.Found {
					reason = models.GovernanceTaskReasonSubjectNotFound
				}
				opened, err := openResponsibilityGovernanceTask(tx, current, reason, snapshot, now)
				if err != nil {
					return err
				}
				if current.Status != targetStatus || opened {
					changedEntries[current.CatalogEntryID] = append(changedEntries[current.CatalogEntryID], responsibilityChangeDetail(current, targetStatus, reason))
				}
			} else {
				resolvedTask, err := resolveResponsibilityGovernanceTask(tx, current, models.GovernanceTaskResolutionReferenceRestored, now)
				if err != nil {
					return err
				}
				if current.Status != targetStatus || resolvedTask {
					changedEntries[current.CatalogEntryID] = append(changedEntries[current.CatalogEntryID], responsibilityChangeDetail(current, targetStatus, models.GovernanceTaskResolutionReferenceRestored))
				}
			}
		}
		for _, entryID := range entryIDs {
			changes := changedEntries[entryID]
			if len(changes) == 0 {
				continue
			}
			if err := tx.Model(&models.Entry{}).Where("tenant_id = ? AND id = ?", tenantID, entryID).
				Updates(map[string]interface{}{"version": gorm.Expr("version + 1"), "updated_at": now}).Error; err != nil {
				return fmt.Errorf("advance Catalog entry after responsibility reconciliation: %w", err)
			}
			if err := tx.Create(&models.AuditEvent{
				ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entryID,
				EventType: "catalog.responsibility.reference_state_changed", ActorType: "service", ActorID: "addp-catalog",
				Details: commonModels.JSONMap{"changes": changes}, CreatedAt: now,
			}).Error; err != nil {
				return fmt.Errorf("audit Catalog responsibility reconciliation: %w", err)
			}
			if err := tx.Create(&models.ProjectionTask{
				TenantID: tenantID, CatalogEntryID: entryID, Projection: "search", Status: "pending",
				AvailableAt: now, CreatedAt: now, UpdatedAt: now,
			}).Error; err != nil {
				return fmt.Errorf("project Catalog responsibility reconciliation: %w", err)
			}
		}
		return nil
	})
}

func uniqueSortedEntryIDs(responsibilities []models.Responsibility) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(responsibilities))
	ids := make([]uuid.UUID, 0, len(responsibilities))
	for _, responsibility := range responsibilities {
		if _, exists := seen[responsibility.CatalogEntryID]; exists {
			continue
		}
		seen[responsibility.CatalogEntryID] = struct{}{}
		ids = append(ids, responsibility.CatalogEntryID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	return ids
}

func mergeResponsibilitySnapshot(existing commonModels.JSONMap, result commonClient.SystemCatalogReferenceResolution) commonModels.JSONMap {
	snapshot := make(commonModels.JSONMap, len(existing)+6)
	for key, value := range existing {
		snapshot[key] = value
	}
	if result.Name != "" {
		snapshot["name"] = result.Name
	}
	if result.Code != "" {
		snapshot["code"] = result.Code
	}
	if result.Status != "" {
		snapshot["status"] = result.Status
	}
	if result.PrincipalStatus != "" {
		snapshot["principal_status"] = result.PrincipalStatus
	}
	if result.MembershipStatus != "" {
		snapshot["membership_status"] = result.MembershipStatus
	}
	snapshot["found"] = result.Found
	snapshot["referenceable"] = result.Referenceable
	return snapshot
}

func openResponsibilityGovernanceTask(tx *gorm.DB, responsibility models.Responsibility, reason string, snapshot commonModels.JSONMap, now time.Time) (bool, error) {
	var task models.GovernanceTask
	err := tx.Where(
		"tenant_id = ? AND catalog_entry_id = ? AND task_type = ? AND responsibility_role = ? AND subject_type = ? AND subject_id = ? AND status = ?",
		responsibility.TenantID, responsibility.CatalogEntryID, models.GovernanceTaskTypeResponsibilityTransfer,
		responsibility.Role, responsibility.SubjectType, responsibility.SubjectID, models.GovernanceTaskStatusOpen,
	).First(&task).Error
	if err == nil {
		if err := tx.Model(&models.GovernanceTask{}).Where("id = ?", task.ID).
			Updates(map[string]interface{}{"reason": reason, "observed_snapshot": snapshot, "updated_at": now}).Error; err != nil {
			return false, fmt.Errorf("refresh Catalog governance task: %w", err)
		}
		return false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, fmt.Errorf("read Catalog governance task: %w", err)
	}
	if err := tx.Create(&models.GovernanceTask{
		ID: uuid.New(), TenantID: responsibility.TenantID, CatalogEntryID: responsibility.CatalogEntryID,
		TaskType: models.GovernanceTaskTypeResponsibilityTransfer, ResponsibilityRole: responsibility.Role,
		SubjectType: responsibility.SubjectType, SubjectID: responsibility.SubjectID,
		Status: models.GovernanceTaskStatusOpen, Reason: reason, ObservedSnapshot: snapshot,
		OpenedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		return false, fmt.Errorf("open Catalog governance task: %w", err)
	}
	return true, nil
}

func resolveResponsibilityGovernanceTask(tx *gorm.DB, responsibility models.Responsibility, resolution string, now time.Time) (bool, error) {
	result := tx.Model(&models.GovernanceTask{}).Where(
		"tenant_id = ? AND catalog_entry_id = ? AND task_type = ? AND responsibility_role = ? AND subject_type = ? AND subject_id = ? AND status = ?",
		responsibility.TenantID, responsibility.CatalogEntryID, models.GovernanceTaskTypeResponsibilityTransfer,
		responsibility.Role, responsibility.SubjectType, responsibility.SubjectID, models.GovernanceTaskStatusOpen,
	).Updates(map[string]interface{}{
		"status": models.GovernanceTaskStatusResolved, "resolved_at": now, "resolution": resolution, "updated_at": now,
	})
	if result.Error != nil {
		return false, fmt.Errorf("resolve Catalog governance task: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}

func responsibilityChangeDetail(responsibility models.Responsibility, status, reason string) commonModels.JSONMap {
	return commonModels.JSONMap{
		"role": responsibility.Role, "subject_type": responsibility.SubjectType,
		"subject_id": responsibility.SubjectID, "previous_status": responsibility.Status,
		"status": status, "reason": reason,
	}
}

func resolveSupersededResponsibilityTasks(tx *gorm.DB, tenantID int64, entryID uuid.UUID, inputs []ResponsibilityInput, now time.Time) error {
	currentKeys := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		currentKeys[fmt.Sprintf("%s:%s:%d", input.Role, input.SubjectType, input.SubjectID)] = struct{}{}
	}
	var tasks []models.GovernanceTask
	if err := tx.Where("tenant_id = ? AND catalog_entry_id = ? AND task_type = ? AND status = ?", tenantID, entryID,
		models.GovernanceTaskTypeResponsibilityTransfer, models.GovernanceTaskStatusOpen).Find(&tasks).Error; err != nil {
		return fmt.Errorf("list superseded Catalog governance tasks: %w", err)
	}
	for _, task := range tasks {
		key := fmt.Sprintf("%s:%s:%d", task.ResponsibilityRole, task.SubjectType, task.SubjectID)
		if _, exists := currentKeys[key]; exists {
			continue
		}
		resolution := models.GovernanceTaskResolutionResponsibilityReplaced
		if err := tx.Model(&models.GovernanceTask{}).Where("id = ? AND status = ?", task.ID, models.GovernanceTaskStatusOpen).
			Updates(map[string]interface{}{"status": models.GovernanceTaskStatusResolved, "resolved_at": now, "resolution": resolution, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("resolve superseded Catalog governance task: %w", err)
		}
	}
	return nil
}
