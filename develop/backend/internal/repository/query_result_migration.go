package repository

import "gorm.io/gorm"

// Owner data migration only: shared execution DDL remains owned by Common/System.
func migrateQueryResultStorage(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(2026082605)).Error; err != nil {
			return err
		}
		var applied int64
		if err := tx.Table("develop.data_migrations").Where("version = ?", int64(2026101701)).Count(&applied).Error; err != nil {
			return err
		}
		if applied != 0 {
			return nil
		}
		if err := tx.Exec(`UPDATE common.task_executions
			SET metadata = metadata - 'protected_query_history'
			WHERE module = 'develop' AND task_type = 'query' AND metadata ? 'protected_query_history'`).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO develop.data_migrations(version, name) VALUES (2026101701, 'query_history_default_protection_only')`).Error
	})
}
