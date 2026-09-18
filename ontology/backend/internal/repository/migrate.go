package repository

import (
	_ "embed"

	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

const SchemaVersion int64 = 3

//go:embed migrations/001_revisions.sql
var initialSchema string

//go:embed migrations/002_projection_runtime.sql
var projectionSchema string

//go:embed migrations/003_projection_rebuild.sql
var rebuildSchema string

// Migrate is the Backend's sole owner migration path. Shared schema
// initialization belongs to System, never to this owner.
func Migrate(db *gorm.DB) error {
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	return schema.Migrate(db, "ontology", SchemaVersion, func(tx *gorm.DB) error {
		var current int64
		if err := tx.Raw("SELECT COALESCE(MAX(version),0) FROM ontology.startup_schema_revision").Scan(&current).Error; err != nil {
			return err
		}
		if current == 0 {
			if err := tx.Exec(initialSchema).Error; err != nil {
				return err
			}
		}
		if current < 2 {
			if err := tx.Exec(projectionSchema).Error; err != nil {
				return err
			}
		}
		return tx.Exec(rebuildSchema).Error
	})
}
