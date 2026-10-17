package repository

import (
	"encoding/json"

	"github.com/addp/security/internal/models"
	"gorm.io/gorm"
)

// migrateAccessRequestScopes is a one-way storage migration. Existing Manager
// grants remain Manager-only; no runtime compatibility fields are retained.
func migrateAccessRequestScopes(db *gorm.DB) error {
	exists, err := accessRequestColumnExists(db, "consumer_owner")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			ID            string
			ConsumerOwner string
			Action        string
		}
		if err := tx.Table("security.protection_access_requests").Select("id,consumer_owner,action").Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			encoded, err := json.Marshal([]models.ProtectionAccessScope{{ConsumerOwner: row.ConsumerOwner, Action: row.Action}})
			if err != nil {
				return err
			}
			if err := tx.Table("security.protection_access_requests").Where("id = ?", row.ID).Update("scopes", string(encoded)).Error; err != nil {
				return err
			}
		}
		index := "security.uq_security_pending_access_request"
		if err := tx.Exec("DROP INDEX IF EXISTS " + index).Error; err != nil {
			return err
		}
		for _, column := range []string{"consumer_owner", "action"} {
			if err := tx.Exec("ALTER TABLE security.protection_access_requests DROP COLUMN " + column).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func accessRequestColumnExists(db *gorm.DB, name string) (bool, error) {
	if db.Dialector.Name() != "sqlite" {
		return db.Migrator().HasColumn(&models.ProtectionAccessRequest{}, name), nil
	}
	var columns []struct{ Name string }
	if err := db.Raw("PRAGMA security.table_info(protection_access_requests)").Scan(&columns).Error; err != nil {
		return false, err
	}
	for _, column := range columns {
		if column.Name == name {
			return true, nil
		}
	}
	return false, nil
}
