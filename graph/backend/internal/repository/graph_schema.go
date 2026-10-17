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
		if err := tx.AutoMigrate(&models.Ontology{}, &models.EntityType{}, &models.RelationType{}, &models.OntologyVersion{}, &models.KnowledgeGraph{}, &models.BuildTask{}, &models.BuildMaterial{}, &models.ReviewItem{}); err != nil {
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
