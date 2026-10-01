package service

import (
	"fmt"

	"github.com/addp/catalog/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Locate without locking, then lock all aggregates before their bindings. Event
// order must not reverse the entry-first order used by curation and rebinding.
func lockSourceChangeBindings(tx *gorm.DB, tenantID int64, references []CatalogSourceReference) (map[string]*models.SourceBinding, error) {
	bindings := make(map[string]*models.SourceBinding)
	conditions := make([]clause.Expression, 0, len(references))
	seen := make(map[string]bool)
	for _, reference := range references {
		key := sourceReferenceKey(reference.SourceModule, reference.SourceType, reference.SourceIdentity)
		if seen[key] {
			continue
		}
		seen[key] = true
		conditions = append(conditions, clause.And(
			clause.Eq{Column: "source_module", Value: reference.SourceModule},
			clause.Eq{Column: "source_type", Value: reference.SourceType},
			clause.Eq{Column: "source_identity", Value: reference.SourceIdentity},
		))
	}
	if len(conditions) == 0 {
		return bindings, nil
	}
	var located []models.SourceBinding
	if err := tx.Where("tenant_id = ? AND is_current = ?", tenantID, true).
		Where(clause.Or(conditions...)).Find(&located).Error; err != nil {
		return nil, fmt.Errorf("locate Catalog source bindings: %w", err)
	}
	if len(located) == 0 {
		return bindings, nil
	}
	entryIDs := make([]uuid.UUID, 0, len(located))
	bindingIDs := make([]uuid.UUID, 0, len(located))
	entrySet := make(map[uuid.UUID]bool)
	original := make(map[uuid.UUID]string)
	for _, binding := range located {
		if !entrySet[binding.CatalogEntryID] {
			entryIDs = append(entryIDs, binding.CatalogEntryID)
			entrySet[binding.CatalogEntryID] = true
		}
		bindingIDs = append(bindingIDs, binding.ID)
		original[binding.ID] = sourceReferenceKey(binding.SourceModule, binding.SourceType, binding.SourceIdentity)
	}
	var entries []models.Entry
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND id IN ?", tenantID, entryIDs).Order("id ASC").Find(&entries).Error; err != nil {
		return nil, fmt.Errorf("lock Catalog source entries: %w", err)
	}
	if len(entries) != len(entryIDs) {
		return nil, fmt.Errorf("Catalog source entries changed concurrently")
	}
	var current []models.SourceBinding
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND id IN ? AND catalog_entry_id IN ? AND is_current = ?", tenantID, bindingIDs, entryIDs, true).
		Order("id ASC").Find(&current).Error; err != nil {
		return nil, fmt.Errorf("lock Catalog source bindings: %w", err)
	}
	// Never follow a moved binding to an aggregate outside the locked set. The
	// caller rolls back the batch and leaves its checkpoint available for retry.
	if len(current) != len(located) {
		return nil, fmt.Errorf("Catalog source bindings changed concurrently")
	}
	for index := range current {
		binding := &current[index]
		key := sourceReferenceKey(binding.SourceModule, binding.SourceType, binding.SourceIdentity)
		if original[binding.ID] != key {
			return nil, fmt.Errorf("Catalog source binding identity changed concurrently")
		}
		bindings[key] = binding
	}
	return bindings, nil
}
