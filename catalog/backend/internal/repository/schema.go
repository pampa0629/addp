package repository

import (
	"fmt"
	"time"

	"github.com/addp/catalog/internal/models"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const catalogSchemaLockID int64 = 2026082601

func Migrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("catalog schema database is required")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS catalog").Error; err != nil {
				return fmt.Errorf("create catalog schema: %w", err)
			}
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", catalogSchemaLockID).Error; err != nil {
				return fmt.Errorf("acquire catalog schema lock: %w", err)
			}
		}
		if err := tx.AutoMigrate(
			&models.Entry{},
			&models.SourceBinding{},
			&models.Component{},
			&models.Responsibility{},
			&models.GovernanceTask{},
			&models.EntryMark{},
			&models.Collection{},
			&models.CollectionEntry{},
			&models.CollectionAuditEvent{},
			&models.SemanticAssociation{},
			&models.StandardMapping{},
			&models.SourceCheckpoint{},
			&models.ProjectionTask{},
			&models.AuditEvent{},
		); err != nil {
			return fmt.Errorf("auto migrate catalog schema: %w", err)
		}
		if err := migrateLegacyComponentElements(tx); err != nil {
			return err
		}
		if err := migrateBusinessResponsibilityEvidence(tx); err != nil {
			return err
		}
		return applyConstraints(tx)
	})
}

// Positive legacy evidence is safe to recover. Missing or count-only history
// cannot prove a negative; it remains NULL, including previously discovered rows.
func migrateBusinessResponsibilityEvidence(tx *gorm.DB) error {
	if tx.Dialector.Name() != "postgres" {
		return nil
	}
	err := tx.Exec(`UPDATE catalog.entries AS entry
		SET business_responsibility_established = TRUE
		WHERE entry.business_responsibility_established IS NULL AND (
			entry.governance_status IN ('curated', 'certified', 'deprecated')
			OR EXISTS (SELECT 1 FROM catalog.responsibilities AS responsibility
				WHERE responsibility.tenant_id = entry.tenant_id AND responsibility.catalog_entry_id = entry.id
				AND responsibility.role = 'business_owner' AND responsibility.subject_type = 'user')
			OR EXISTS (SELECT 1 FROM catalog.audit_events AS audit
				WHERE audit.tenant_id = entry.tenant_id AND audit.catalog_entry_id = entry.id AND (
					audit.details->>'governance_status' IN ('curated', 'certified', 'deprecated')
					OR audit.details->>'previous_governance_status' IN ('curated', 'certified', 'deprecated')
					OR EXISTS (SELECT 1 FROM jsonb_array_elements(
						CASE WHEN jsonb_typeof(audit.details->'responsibilities') = 'array'
						THEN audit.details->'responsibilities' ELSE '[]'::jsonb END
					) AS subject WHERE subject->>'role' = 'business_owner' AND subject->>'subject_type' = 'user'
						AND subject->>'subject_id' ~ '^[1-9][0-9]*$')
				))
		)`).Error
	if err != nil {
		return fmt.Errorf("recover Catalog business responsibility evidence: %w", err)
	}
	return nil
}

// The old table is read only during the single transactional replacement. An
// unknown historical revision must never become an approved mapping.
type legacyComponentElementAssociation struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey"`
	TenantID         int64
	CatalogEntryID   uuid.UUID
	ComponentID      uuid.UUID
	ElementID        int64
	ObservedVersion  int64
	ObservedSnapshot commonModels.JSONMap `gorm:"type:jsonb"`
	VerifiedAt       time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (legacyComponentElementAssociation) TableName() string {
	return "catalog.component_element_associations"
}

func migrateLegacyComponentElements(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&legacyComponentElementAssociation{}) {
		return nil
	}
	var legacy []legacyComponentElementAssociation
	if err := tx.Find(&legacy).Error; err != nil {
		return fmt.Errorf("read legacy Catalog component associations: %w", err)
	}
	for _, old := range legacy {
		evidence := commonModels.JSONMap{
			"legacy_observed_version":  old.ObservedVersion,
			"legacy_observed_snapshot": old.ObservedSnapshot,
			"legacy_verified_at":       old.VerifiedAt.Format(time.RFC3339Nano),
		}
		row := models.StandardMapping{
			ID: old.ID, TenantID: old.TenantID, CatalogEntryID: old.CatalogEntryID,
			ComponentID: old.ComponentID, ElementID: old.ElementID,
			Source: models.StandardMappingSourceLegacy, Evidence: evidence,
			ReviewStatus: models.StandardMappingProposed, Version: 1,
			ProposedByType: "migration", ProposedByID: "legacy-component-element",
			CreatedAt: old.CreatedAt, UpdatedAt: old.UpdatedAt,
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("migrate legacy Catalog component association: %w", err)
		}
	}
	if err := tx.Migrator().DropTable(&legacyComponentElementAssociation{}); err != nil {
		return fmt.Errorf("drop replaced Catalog component association table: %w", err)
	}
	return nil
}

func applyConstraints(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	statements := []string{
		`CREATE OR REPLACE FUNCTION catalog.preserve_business_responsibility_establishment()
		RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
		BEGIN
			IF NEW.business_responsibility_established IS DISTINCT FROM OLD.business_responsibility_established
				AND NEW.business_responsibility_established IS DISTINCT FROM TRUE THEN
				RAISE EXCEPTION 'Catalog business responsibility establishment evidence cannot be reset'
					USING ERRCODE = '23514';
			END IF;
			RETURN NEW;
		END $$`,
		`DROP TRIGGER IF EXISTS preserve_business_responsibility_establishment ON catalog.entries`,
		`CREATE TRIGGER preserve_business_responsibility_establishment
			BEFORE UPDATE OF business_responsibility_established ON catalog.entries
			FOR EACH ROW EXECUTE FUNCTION catalog.preserve_business_responsibility_establishment()`,
		`ALTER TABLE catalog.entries DROP CONSTRAINT IF EXISTS ck_catalog_entries_entry_type`,
		`ALTER TABLE catalog.entries ADD CONSTRAINT ck_catalog_entries_entry_type CHECK (entry_type IN ('data_item', 'business_entity', 'logical_model', 'metric', 'data_service', 'development_artifact', 'data_application'))`,
		`ALTER TABLE catalog.entries DROP CONSTRAINT IF EXISTS ck_catalog_entries_entry_status`,
		`ALTER TABLE catalog.entries ADD CONSTRAINT ck_catalog_entries_entry_status CHECK (entry_status IN ('active', 'merged'))`,
		`ALTER TABLE catalog.entries DROP CONSTRAINT IF EXISTS ck_catalog_entries_governance_status`,
		`ALTER TABLE catalog.entries ADD CONSTRAINT ck_catalog_entries_governance_status CHECK (governance_status IN ('discovered', 'curated', 'certified', 'deprecated'))`,
		`ALTER TABLE catalog.entries DROP CONSTRAINT IF EXISTS ck_catalog_entries_visibility`,
		`ALTER TABLE catalog.entries ADD CONSTRAINT ck_catalog_entries_visibility CHECK (visibility IN ('inventory', 'department', 'tenant'))`,
		`ALTER TABLE catalog.entries DROP CONSTRAINT IF EXISTS ck_catalog_entries_version`,
		`ALTER TABLE catalog.entries ADD CONSTRAINT ck_catalog_entries_version CHECK (version > 0)`,
		`ALTER TABLE catalog.entries DROP CONSTRAINT IF EXISTS ck_catalog_entries_merge_shape`,
		`ALTER TABLE catalog.entries ADD CONSTRAINT ck_catalog_entries_merge_shape CHECK (
			(entry_status = 'active' AND merged_into_entry_id IS NULL)
			OR (entry_status = 'merged' AND merged_into_entry_id IS NOT NULL AND merged_into_entry_id <> id)
		)`,
		`ALTER TABLE catalog.entries DROP CONSTRAINT IF EXISTS ck_catalog_entries_successor_shape`,
		`ALTER TABLE catalog.entries ADD CONSTRAINT ck_catalog_entries_successor_shape CHECK (
			recommended_successor_entry_id IS NULL
			OR (entry_status = 'active' AND governance_status = 'deprecated' AND recommended_successor_entry_id <> id)
		)`,
		`CREATE INDEX IF NOT EXISTS ix_catalog_entries_recommended_successor ON catalog.entries (tenant_id, recommended_successor_entry_id) WHERE recommended_successor_entry_id IS NOT NULL`,
		`ALTER TABLE catalog.entries DROP CONSTRAINT IF EXISTS ck_catalog_entries_governance_visibility`,
		`ALTER TABLE catalog.entries ADD CONSTRAINT ck_catalog_entries_governance_visibility CHECK (
			(governance_status = 'discovered' AND visibility = 'inventory')
			OR governance_status = 'curated'
			OR (governance_status = 'certified' AND visibility IN ('department', 'tenant'))
			OR governance_status = 'deprecated'
		)`,
		`ALTER TABLE catalog.source_bindings DROP CONSTRAINT IF EXISTS ck_catalog_source_status`,
		`ALTER TABLE catalog.source_bindings ADD CONSTRAINT ck_catalog_source_status CHECK (source_status IN ('active', 'missing'))`,
		`ALTER TABLE catalog.source_bindings DROP CONSTRAINT IF EXISTS ck_catalog_source_shape`,
		`ALTER TABLE catalog.source_bindings ADD CONSTRAINT ck_catalog_source_shape CHECK (
			(source_module = 'meta' AND source_type = 'data_item' AND char_length(btrim(source_identity)) > 0)
			OR (source_module = 'model' AND source_type IN ('entity', 'logical_table') AND source_identity ~ '^[1-9][0-9]*$')
			OR (source_module = 'standard' AND source_type = 'metric' AND source_identity ~ '^[1-9][0-9]*$')
			OR (source_module = 'service' AND source_type = 'query_service' AND source_identity ~ '^[1-9][0-9]*$')
			OR (source_module = 'develop' AND source_type = 'dev_task' AND source_identity ~ '^[1-9][0-9]*$')
			OR (source_module = 'workbench' AND source_type = 'data_application' AND source_identity ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_current_source_identity ON catalog.source_bindings (tenant_id, source_module, source_type, source_identity) WHERE is_current`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_entry_current_source ON catalog.source_bindings (tenant_id, catalog_entry_id) WHERE is_current`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_component_key ON catalog.components (tenant_id, catalog_entry_id, component_key)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_checkpoint ON catalog.source_checkpoints (tenant_id, source_module, feed_name)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_responsibility ON catalog.responsibilities (tenant_id, catalog_entry_id, role, subject_type, subject_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_semantic_association ON catalog.semantic_associations (tenant_id, catalog_entry_id, semantic_type, semantic_id, relation_role)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_primary_domain ON catalog.semantic_associations (tenant_id, catalog_entry_id) WHERE semantic_type = 'domain' AND relation_role = 'primary'`,
		`ALTER TABLE catalog.standard_mappings DROP CONSTRAINT IF EXISTS ck_catalog_standard_mapping_shape`,
		`ALTER TABLE catalog.standard_mappings ADD CONSTRAINT ck_catalog_standard_mapping_shape CHECK (
			element_id > 0 AND version > 0
			AND source IN ('manual', 'copilot', 'legacy')
			AND review_status IN ('proposed', 'approved', 'rejected', 'withdrawn')
			AND (confidence IS NULL OR confidence BETWEEN 0 AND 1)
			AND ((element_revision_id IS NOT NULL AND element_revision_id > 0) OR (source = 'legacy' AND review_status = 'proposed' AND element_revision_id IS NULL))
			AND char_length(btrim(proposed_by_type)) > 0 AND char_length(btrim(proposed_by_id)) > 0
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_approved_standard_mapping ON catalog.standard_mappings (tenant_id, component_id) WHERE review_status = 'approved'`,
		`ALTER TABLE catalog.semantic_associations DROP CONSTRAINT IF EXISTS ck_catalog_semantic_shape`,
		`ALTER TABLE catalog.semantic_associations ADD CONSTRAINT ck_catalog_semantic_shape CHECK (
			(semantic_type = 'domain' AND relation_role IN ('primary', 'secondary'))
			OR (semantic_type = 'glossary' AND relation_role = 'applies')
		)`,
		`ALTER TABLE catalog.responsibilities DROP CONSTRAINT IF EXISTS ck_catalog_responsibility_shape`,
		`ALTER TABLE catalog.responsibilities ADD CONSTRAINT ck_catalog_responsibility_shape CHECK (
			(role = 'accountable_department' AND subject_type = 'department')
			OR (role IN ('business_owner', 'data_steward', 'technical_owner') AND subject_type = 'user')
		)`,
		`ALTER TABLE catalog.responsibilities DROP CONSTRAINT IF EXISTS ck_catalog_responsibility_status`,
		`ALTER TABLE catalog.responsibilities ADD CONSTRAINT ck_catalog_responsibility_status CHECK (status IN ('active', 'needs_transfer'))`,
		`ALTER TABLE catalog.governance_tasks DROP CONSTRAINT IF EXISTS ck_catalog_governance_task_type`,
		`ALTER TABLE catalog.governance_tasks ADD CONSTRAINT ck_catalog_governance_task_type CHECK (task_type IN ('responsibility_transfer'))`,
		`ALTER TABLE catalog.governance_tasks DROP CONSTRAINT IF EXISTS ck_catalog_governance_task_status`,
		`ALTER TABLE catalog.governance_tasks ADD CONSTRAINT ck_catalog_governance_task_status CHECK (status IN ('open', 'resolved'))`,
		`ALTER TABLE catalog.governance_tasks DROP CONSTRAINT IF EXISTS ck_catalog_governance_task_reason`,
		`ALTER TABLE catalog.governance_tasks ADD CONSTRAINT ck_catalog_governance_task_reason CHECK (reason IN ('subject_not_found', 'subject_not_referenceable'))`,
		`ALTER TABLE catalog.governance_tasks DROP CONSTRAINT IF EXISTS ck_catalog_governance_task_resolution`,
		`ALTER TABLE catalog.governance_tasks ADD CONSTRAINT ck_catalog_governance_task_resolution CHECK (
			(status = 'open' AND resolved_at IS NULL AND resolution IS NULL)
			OR (status = 'resolved' AND resolved_at IS NOT NULL AND resolution IN ('reference_restored', 'responsibility_replaced'))
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_open_governance_task ON catalog.governance_tasks
			(tenant_id, catalog_entry_id, task_type, responsibility_role, subject_type, subject_id) WHERE status = 'open'`,
		`ALTER TABLE catalog.entry_marks DROP CONSTRAINT IF EXISTS ck_catalog_entry_mark_type`,
		`ALTER TABLE catalog.entry_marks ADD CONSTRAINT ck_catalog_entry_mark_type CHECK (mark_type IN ('favorite', 'following'))`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_entry_mark ON catalog.entry_marks (tenant_id, user_id, catalog_entry_id, mark_type)`,
		`ALTER TABLE catalog.collections DROP CONSTRAINT IF EXISTS ck_catalog_collection_shape`,
		`ALTER TABLE catalog.collections ADD CONSTRAINT ck_catalog_collection_shape CHECK (project_group_id > 0 AND version > 0 AND created_by > 0 AND char_length(btrim(name)) BETWEEN 1 AND 200 AND char_length(description) <= 2000)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_collection_name ON catalog.collections (tenant_id, project_group_id, lower(name))`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_collection_entry ON catalog.collection_entries (tenant_id, collection_id, catalog_entry_id)`,
		`ALTER TABLE catalog.collection_entries DROP CONSTRAINT IF EXISTS ck_catalog_collection_entry_shape`,
		`ALTER TABLE catalog.collection_entries ADD CONSTRAINT ck_catalog_collection_entry_shape CHECK (added_by > 0)`,
		`ALTER TABLE catalog.collection_audit_events DROP CONSTRAINT IF EXISTS ck_catalog_collection_audit_shape`,
		`ALTER TABLE catalog.collection_audit_events ADD CONSTRAINT ck_catalog_collection_audit_shape CHECK (actor_id > 0 AND event_type IN ('catalog.collection.created', 'catalog.collection.updated', 'catalog.collection.deleted'))`,
		`ALTER TABLE catalog.entries DROP CONSTRAINT IF EXISTS fk_catalog_entries_merged_into`,
		`ALTER TABLE catalog.entries ADD CONSTRAINT fk_catalog_entries_merged_into FOREIGN KEY (merged_into_entry_id) REFERENCES catalog.entries(id) ON DELETE RESTRICT`,
		`ALTER TABLE catalog.entries DROP CONSTRAINT IF EXISTS fk_catalog_entries_recommended_successor`,
		`ALTER TABLE catalog.entries ADD CONSTRAINT fk_catalog_entries_recommended_successor FOREIGN KEY (recommended_successor_entry_id) REFERENCES catalog.entries(id) ON DELETE RESTRICT`,
		`ALTER TABLE catalog.source_bindings DROP CONSTRAINT IF EXISTS fk_catalog_source_entry`,
		`ALTER TABLE catalog.source_bindings ADD CONSTRAINT fk_catalog_source_entry FOREIGN KEY (catalog_entry_id) REFERENCES catalog.entries(id) ON DELETE RESTRICT`,
		`ALTER TABLE catalog.source_bindings DROP CONSTRAINT IF EXISTS fk_catalog_source_replaced_binding`,
		`ALTER TABLE catalog.source_bindings ADD CONSTRAINT fk_catalog_source_replaced_binding FOREIGN KEY (replaced_binding_id) REFERENCES catalog.source_bindings(id) ON DELETE RESTRICT`,
		`ALTER TABLE catalog.components DROP CONSTRAINT IF EXISTS fk_catalog_components_entry`,
		`ALTER TABLE catalog.components ADD CONSTRAINT fk_catalog_components_entry FOREIGN KEY (catalog_entry_id) REFERENCES catalog.entries(id) ON DELETE RESTRICT`,
		`ALTER TABLE catalog.responsibilities DROP CONSTRAINT IF EXISTS fk_catalog_responsibilities_entry`,
		`ALTER TABLE catalog.responsibilities ADD CONSTRAINT fk_catalog_responsibilities_entry FOREIGN KEY (catalog_entry_id) REFERENCES catalog.entries(id) ON DELETE RESTRICT`,
		`ALTER TABLE catalog.governance_tasks DROP CONSTRAINT IF EXISTS fk_catalog_governance_tasks_entry`,
		`ALTER TABLE catalog.governance_tasks ADD CONSTRAINT fk_catalog_governance_tasks_entry FOREIGN KEY (catalog_entry_id) REFERENCES catalog.entries(id) ON DELETE RESTRICT`,
		`ALTER TABLE catalog.entry_marks DROP CONSTRAINT IF EXISTS fk_catalog_entry_marks_entry`,
		`ALTER TABLE catalog.entry_marks ADD CONSTRAINT fk_catalog_entry_marks_entry FOREIGN KEY (catalog_entry_id) REFERENCES catalog.entries(id) ON DELETE CASCADE`,
		`ALTER TABLE catalog.collection_entries DROP CONSTRAINT IF EXISTS fk_catalog_collection_entries_collection`,
		`ALTER TABLE catalog.collection_entries ADD CONSTRAINT fk_catalog_collection_entries_collection FOREIGN KEY (collection_id) REFERENCES catalog.collections(id) ON DELETE CASCADE`,
		`ALTER TABLE catalog.collection_entries DROP CONSTRAINT IF EXISTS fk_catalog_collection_entries_entry`,
		`ALTER TABLE catalog.collection_entries ADD CONSTRAINT fk_catalog_collection_entries_entry FOREIGN KEY (catalog_entry_id) REFERENCES catalog.entries(id) ON DELETE RESTRICT`,
		`ALTER TABLE catalog.semantic_associations DROP CONSTRAINT IF EXISTS fk_catalog_semantic_entry`,
		`ALTER TABLE catalog.semantic_associations ADD CONSTRAINT fk_catalog_semantic_entry FOREIGN KEY (catalog_entry_id) REFERENCES catalog.entries(id) ON DELETE RESTRICT`,
		`ALTER TABLE catalog.standard_mappings DROP CONSTRAINT IF EXISTS fk_catalog_standard_mapping_entry`,
		`ALTER TABLE catalog.standard_mappings ADD CONSTRAINT fk_catalog_standard_mapping_entry FOREIGN KEY (catalog_entry_id) REFERENCES catalog.entries(id) ON DELETE RESTRICT`,
		`ALTER TABLE catalog.standard_mappings DROP CONSTRAINT IF EXISTS fk_catalog_standard_mapping_component`,
		`ALTER TABLE catalog.standard_mappings ADD CONSTRAINT fk_catalog_standard_mapping_component FOREIGN KEY (component_id) REFERENCES catalog.components(id) ON DELETE RESTRICT`,
		`ALTER TABLE catalog.projection_tasks DROP CONSTRAINT IF EXISTS fk_catalog_projection_entry`,
		`ALTER TABLE catalog.projection_tasks ADD CONSTRAINT fk_catalog_projection_entry FOREIGN KEY (catalog_entry_id) REFERENCES catalog.entries(id) ON DELETE CASCADE`,
		`ALTER TABLE catalog.audit_events DROP CONSTRAINT IF EXISTS fk_catalog_audit_entry`,
		`ALTER TABLE catalog.audit_events ADD CONSTRAINT fk_catalog_audit_entry FOREIGN KEY (catalog_entry_id) REFERENCES catalog.entries(id) ON DELETE RESTRICT`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("apply catalog constraint: %w", err)
		}
	}
	return nil
}
