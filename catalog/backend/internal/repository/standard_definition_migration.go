package repository

import (
	"fmt"
	"github.com/addp/catalog/internal/models"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"time"
)

// Archive before clearing; the entry lock and history insert share one transaction.
// No owner database is queried and no text is inferred or merged.
func migrateStandardDefinitionOwnership(tx *gorm.DB) error {
	var entries []models.Entry
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id,tenant_id,business_name,business_description,version").Where("entry_type = ? AND (business_name IS NOT NULL OR business_description IS NOT NULL)", models.EntryTypeMetric).Order("id ASC").Find(&entries).Error; err != nil {
		return err
	}
	for _, entry := range entries {
		var sources []models.SourceBinding
		if err := tx.Select("id,source_module,source_type,source_identity").Where("tenant_id = ? AND catalog_entry_id = ? AND is_current = ?", entry.TenantID, entry.ID, true).Find(&sources).Error; err != nil {
			return err
		}
		if len(sources) != 1 || sources[0].SourceModule != models.SourceModuleStandard || sources[0].SourceType != models.SourceTypeMetric {
			return fmt.Errorf("metric %s has ambiguous current source; definition migration requires review", entry.ID)
		}
		source := sources[0]
		id, err := strconv.ParseInt(source.SourceIdentity, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != source.SourceIdentity {
			return fmt.Errorf("metric %s has invalid source identity; definition migration requires review", entry.ID)
		}
		now := time.Now().UTC()
		audit := models.AuditEvent{ID: uuid.New(), TenantID: entry.TenantID, CatalogEntryID: entry.ID, EventType: "catalog.standard_definition.archived", ActorType: "migration", ActorID: "2026102201", CreatedAt: now,
			Details: commonModels.JSONMap{"business_name": entry.BusinessName, "business_description": entry.BusinessDescription, "entry_version": entry.Version, "source_binding_id": source.ID.String(), "source_identity": source.SourceIdentity, "source_module": source.SourceModule, "source_type": source.SourceType}}
		if err := tx.Create(&audit).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Entry{}).Where("id = ? AND tenant_id = ?", entry.ID, entry.TenantID).Updates(map[string]any{"business_name": nil, "business_description": nil, "version": entry.Version + 1, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.ProjectionTask{TenantID: entry.TenantID, CatalogEntryID: entry.ID, Projection: "search", Status: "pending", AvailableAt: now, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			return err
		}
	}
	return nil
}
