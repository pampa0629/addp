package repository

import (
	"github.com/addp/graph/internal/models"
	"gorm.io/gorm"
)

// MigrateGraphTables applies the current schema atomically. No runtime reads or
// writes use the removed database/neo4j_id fields. Existing incompatible bindings
// are not guessed, backfilled or deleted; a failed migration rolls back intact.
func MigrateGraphTables(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(&models.Ontology{}, &models.EntityType{}, &models.RelationType{}, &models.OntologyVersion{}, &models.KnowledgeGraph{}, &models.BuildTask{}, &models.BuildMaterial{}, &models.ReviewItem{}, &models.TenantAdmissionLock{}, &models.BuildExecutionControl{}, &models.GraphOperation{}); err != nil {
			return err
		}
		// Provision G for every existing Tenant before any runtime admission.
		if err := tx.Exec("INSERT INTO tenant_admission_locks (tenant_id) SELECT DISTINCT tenant_id FROM knowledge_graphs WHERE 1=1 ON CONFLICT (tenant_id) DO NOTHING").Error; err != nil {
			return err
		}
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec(`ALTER TABLE knowledge_graphs DROP COLUMN IF EXISTS database`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE review_items DROP COLUMN IF EXISTS neo4j_id`).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
