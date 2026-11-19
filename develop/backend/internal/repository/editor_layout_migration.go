package repository

import (
	"fmt"

	"gorm.io/gorm"
)

func migrateSharedEditorLayout(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(2026082605)).Error; err != nil {
			return err
		}
		migration := tx.Exec(`INSERT INTO develop.data_migrations (version, name)
			VALUES (2026111901, 'remove_shared_editor_viewport') ON CONFLICT (version) DO NOTHING`)
		if migration.Error != nil || migration.RowsAffected == 0 {
			return migration.Error
		}
		if err := tx.Exec(`UPDATE develop.dev_tasks SET editor_layout = editor_layout - 'viewport'
			WHERE jsonb_typeof(editor_layout) = 'object' AND jsonb_exists(editor_layout, 'viewport')`).Error; err != nil {
			return fmt.Errorf("remove Develop shared editor viewport: %w", err)
		}
		return nil
	})
}
