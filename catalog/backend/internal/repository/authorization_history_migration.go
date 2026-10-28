package repository

import (
	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

// The owner checkpoint makes the approved development reset one-shot. Keep the
// checkpoint in this owner's schema; System must not reach into Catalog tables.
func retireDevelopmentAuthorizationHistory(tx *gorm.DB) error {
	if tx.Dialector.Name() != "postgres" {
		return nil
	}
	return schema.Migrate(tx, "catalog", 1, func(owner *gorm.DB) error {
		for _, table := range []string{"catalog.fulfillment_checks", "catalog.sharing_decisions"} {
			if !owner.Migrator().HasTable(table) {
				continue
			}
			if err := owner.Exec("LOCK TABLE " + table + " IN ACCESS EXCLUSIVE MODE").Error; err != nil {
				return err
			}
			if err := owner.Exec("ALTER TABLE " + table + " DISABLE TRIGGER USER").Error; err != nil {
				return err
			}
			if err := owner.Exec("DELETE FROM " + table).Error; err != nil {
				return err
			}
			if err := owner.Exec("ALTER TABLE " + table + " ENABLE TRIGGER USER").Error; err != nil {
				return err
			}
		}
		return nil
	})
}
