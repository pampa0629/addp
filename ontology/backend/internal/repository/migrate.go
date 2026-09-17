package repository

import (
	_ "embed"

	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

const SchemaVersion int64 = 1

//go:embed migrations/001_revisions.sql
var initialSchema string

// Migrate is the future Backend's sole owner migration path. Shared schema
// initialization belongs to System, never to this owner.
func Migrate(db *gorm.DB) error {
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	return schema.Migrate(db, "ontology", SchemaVersion, func(tx *gorm.DB) error {
		return tx.Exec(initialSchema).Error
	})
}
