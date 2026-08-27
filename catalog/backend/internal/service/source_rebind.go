package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/addp/catalog/internal/models"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RebindSourceInput struct {
	TargetVersion         int64
	TemporaryEntryID      uuid.UUID
	TemporaryEntryVersion int64
	NewSourceIdentity     string
	Reason                string
	Evidence              string
}

type EntryHistory struct {
	EntryID        uuid.UUID              `json:"entry_id"`
	SourceBindings []models.SourceBinding `json:"source_bindings"`
	AuditEvents    []models.AuditEvent    `json:"audit_events"`
}

func (s *EntryService) RebindSource(
	ctx context.Context,
	tenantID int64,
	targetEntryID uuid.UUID,
	input RebindSourceInput,
	actor UpdateEntryActor,
) (*EntryDetail, error) {
	input.NewSourceIdentity = strings.TrimSpace(input.NewSourceIdentity)
	input.Reason = strings.TrimSpace(input.Reason)
	input.Evidence = strings.TrimSpace(input.Evidence)
	if s == nil || s.db == nil || tenantID <= 0 || targetEntryID == uuid.Nil || input.TargetVersion <= 0 ||
		input.TemporaryEntryID == uuid.Nil || input.TemporaryEntryID == targetEntryID || input.TemporaryEntryVersion <= 0 ||
		input.NewSourceIdentity == "" || input.Reason == "" || input.Evidence == "" ||
		strings.TrimSpace(actor.Type) == "" || strings.TrimSpace(actor.ID) == "" {
		return nil, ErrInvalidSourceRebind
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		entries, err := lockRebindEntries(tx, tenantID, targetEntryID, input.TemporaryEntryID)
		if err != nil {
			return err
		}
		target := entries[targetEntryID]
		temporary := entries[input.TemporaryEntryID]
		if target.Version != input.TargetVersion || temporary.Version != input.TemporaryEntryVersion {
			return ErrEntryVersionConflict
		}
		if target.EntryStatus != models.EntryStatusActive || temporary.EntryStatus != models.EntryStatusActive ||
			temporary.GovernanceStatus != models.GovernanceStatusDiscovered || temporary.Visibility != models.VisibilityInventory ||
			temporary.BusinessName != nil || temporary.BusinessDescription != nil {
			return ErrSourceRebindConflict
		}

		targetBinding, err := lockCurrentBinding(tx, tenantID, targetEntryID)
		if err != nil {
			return err
		}
		temporaryBinding, err := lockCurrentBinding(tx, tenantID, input.TemporaryEntryID)
		if err != nil {
			return err
		}
		if targetBinding.SourceStatus != models.SourceStatusMissing || temporaryBinding.SourceStatus != models.SourceStatusActive ||
			temporaryBinding.SourceModule != models.SourceModuleMeta || temporaryBinding.SourceType != models.SourceTypeDataItem ||
			temporaryBinding.SourceIdentity != input.NewSourceIdentity {
			return ErrSourceRebindConflict
		}
		if err := ensureTemporaryEntryHasNoHumanWork(tx, tenantID, input.TemporaryEntryID); err != nil {
			return err
		}

		now := time.Now().UTC()
		if err := tx.Model(targetBinding).Updates(map[string]interface{}{
			"is_current": false, "updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("close previous Catalog source binding: %w", err)
		}
		if err := transferDiscoveredComponents(tx, tenantID, targetEntryID, input.TemporaryEntryID, now); err != nil {
			return err
		}
		if err := tx.Model(temporaryBinding).Updates(map[string]interface{}{
			"catalog_entry_id": targetEntryID, "replaced_binding_id": targetBinding.ID, "updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("move replacement Catalog source binding: %w", err)
		}
		if err := tx.Model(&models.Entry{}).Where("tenant_id = ? AND id = ?", tenantID, targetEntryID).Updates(map[string]interface{}{
			"version": target.Version + 1, "updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("advance rebound Catalog entry: %w", err)
		}
		if err := tx.Model(&models.Entry{}).Where("tenant_id = ? AND id = ?", tenantID, temporary.ID).Updates(map[string]interface{}{
			"entry_status": models.EntryStatusMerged, "merged_into_entry_id": targetEntryID,
			"version": temporary.Version + 1, "updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("merge temporary Catalog entry: %w", err)
		}

		details := commonModels.JSONMap{
			"previous_source_identity": targetBinding.SourceIdentity,
			"new_source_identity":      temporaryBinding.SourceIdentity,
			"temporary_entry_id":       temporary.ID.String(),
			"previous_binding_id":      targetBinding.ID.String(),
			"replacement_binding_id":   temporaryBinding.ID.String(),
			"reason":                   input.Reason,
			"evidence":                 input.Evidence,
			"previous_version":         target.Version,
			"version":                  target.Version + 1,
		}
		if err := createAuditEvent(tx, tenantID, targetEntryID, "catalog.source.rebound", actor, details, now); err != nil {
			return err
		}
		if err := createAuditEvent(tx, tenantID, temporary.ID, "catalog.entry.merged", actor, commonModels.JSONMap{
			"merged_into_entry_id": targetEntryID.String(), "source_identity": temporaryBinding.SourceIdentity,
			"reason": input.Reason, "evidence": input.Evidence,
		}, now); err != nil {
			return err
		}
		if err := enqueueProjection(tx, tenantID, targetEntryID); err != nil {
			return err
		}
		return enqueueProjection(tx, tenantID, temporary.ID)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, EntryAccess{Inventory: true}, targetEntryID)
}

func (s *EntryService) History(ctx context.Context, tenantID int64, access EntryAccess, entryID uuid.UUID) (*EntryHistory, error) {
	if s == nil || s.db == nil || tenantID <= 0 || entryID == uuid.Nil {
		return nil, ErrInvalidPage
	}
	var entry models.Entry
	if err := s.visibleEntriesQuery(ctx, tenantID, access).Where("entries.id = ?", entryID).First(&entry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrEntryNotFound
		}
		return nil, fmt.Errorf("get Catalog entry for history: %w", err)
	}
	var bindings []models.SourceBinding
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, entryID).
		Order("bound_at DESC, id ASC").Find(&bindings).Error; err != nil {
		return nil, fmt.Errorf("list Catalog source history: %w", err)
	}
	var events []models.AuditEvent
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, entryID).
		Order("created_at DESC, id ASC").Limit(200).Find(&events).Error; err != nil {
		return nil, fmt.Errorf("list Catalog audit history: %w", err)
	}
	return &EntryHistory{EntryID: entry.ID, SourceBindings: bindings, AuditEvents: events}, nil
}

func lockRebindEntries(tx *gorm.DB, tenantID int64, targetID, temporaryID uuid.UUID) (map[uuid.UUID]models.Entry, error) {
	ids := []uuid.UUID{targetID, temporaryID}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	var rows []models.Entry
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id IN ?", tenantID, ids).
		Order("id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("lock Catalog entries for source rebind: %w", err)
	}
	if len(rows) != 2 {
		return nil, ErrEntryNotFound
	}
	entries := make(map[uuid.UUID]models.Entry, 2)
	for _, row := range rows {
		entries[row.ID] = row
	}
	return entries, nil
}

func lockCurrentBinding(tx *gorm.DB, tenantID int64, entryID uuid.UUID) (*models.SourceBinding, error) {
	var binding models.SourceBinding
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"tenant_id = ? AND catalog_entry_id = ? AND is_current = ?", tenantID, entryID, true,
	).First(&binding).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSourceRebindConflict
		}
		return nil, fmt.Errorf("lock current Catalog source binding: %w", err)
	}
	return &binding, nil
}

func ensureTemporaryEntryHasNoHumanWork(tx *gorm.DB, tenantID int64, entryID uuid.UUID) error {
	for _, model := range []interface{}{&models.SemanticAssociation{}, &models.Responsibility{}, &models.ComponentElementAssociation{}} {
		var count int64
		if err := tx.Model(model).Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, entryID).Count(&count).Error; err != nil {
			return fmt.Errorf("inspect temporary Catalog entry relationships: %w", err)
		}
		if count != 0 {
			return ErrSourceRebindConflict
		}
	}
	var humanAuditCount int64
	if err := tx.Model(&models.AuditEvent{}).Where(
		"tenant_id = ? AND catalog_entry_id = ? AND event_type <> ?", tenantID, entryID, "source_discovered",
	).Count(&humanAuditCount).Error; err != nil {
		return fmt.Errorf("inspect temporary Catalog entry audit: %w", err)
	}
	if humanAuditCount != 0 {
		return ErrSourceRebindConflict
	}
	return nil
}

func transferDiscoveredComponents(tx *gorm.DB, tenantID int64, targetID, temporaryID uuid.UUID, now time.Time) error {
	var targetComponents []models.Component
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"tenant_id = ? AND catalog_entry_id = ?", tenantID, targetID,
	).Find(&targetComponents).Error; err != nil {
		return fmt.Errorf("lock target Catalog components: %w", err)
	}
	var temporaryComponents []models.Component
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"tenant_id = ? AND catalog_entry_id = ?", tenantID, temporaryID,
	).Order("ordinal ASC, component_key ASC").Find(&temporaryComponents).Error; err != nil {
		return fmt.Errorf("lock temporary Catalog components: %w", err)
	}
	if err := tx.Model(&models.Component{}).Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, targetID).
		Updates(map[string]interface{}{"component_status": models.SourceStatusMissing, "updated_at": now}).Error; err != nil {
		return fmt.Errorf("mark previous Catalog components missing: %w", err)
	}
	targetByKey := make(map[string]models.Component, len(targetComponents))
	for _, component := range targetComponents {
		targetByKey[component.ComponentKey] = component
	}
	for _, component := range temporaryComponents {
		if target, exists := targetByKey[component.ComponentKey]; exists {
			if err := tx.Model(&target).Updates(map[string]interface{}{
				"display_name": component.DisplayName, "data_type": component.DataType,
				"component_status": component.ComponentStatus, "ordinal": component.Ordinal,
				"observed_snapshot": component.ObservedSnapshot, "updated_at": now,
			}).Error; err != nil {
				return fmt.Errorf("refresh rebound Catalog component: %w", err)
			}
			if err := tx.Delete(&component).Error; err != nil {
				return fmt.Errorf("remove merged temporary Catalog component: %w", err)
			}
			continue
		}
		if err := tx.Model(&component).Updates(map[string]interface{}{
			"catalog_entry_id": targetID, "updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("move rebound Catalog component: %w", err)
		}
	}
	return nil
}

func createAuditEvent(tx *gorm.DB, tenantID int64, entryID uuid.UUID, eventType string, actor UpdateEntryActor, details commonModels.JSONMap, now time.Time) error {
	if err := tx.Create(&models.AuditEvent{
		ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entryID, EventType: eventType,
		ActorType: actor.Type, ActorID: actor.ID, Details: details, CreatedAt: now,
	}).Error; err != nil {
		return fmt.Errorf("create Catalog audit event: %w", err)
	}
	return nil
}
