package repository

import (
	"encoding/json"
	"fmt"
	"github.com/addp/common/infra/objectstore"
	"gorm.io/gorm"
	"strings"
)

const notebookStorageVersion int64 = 2026111601

// migrateNotebookStorage changes mutable definitions only, including soft-deleted tasks.
func migrateNotebookStorage(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", notebookStorageVersion).Error; err != nil {
			return err
		}
		var applied int64
		if err := tx.Raw("SELECT count(*) FROM develop.data_migrations WHERE version=?", notebookStorageVersion).Scan(&applied).Error; err != nil {
			return err
		}
		if applied != 0 {
			return nil
		}
		var rows []struct {
			ID       uint
			TenantID uint
			Content  []byte
		}
		if err := tx.Raw(`SELECT id,tenant_id,content FROM develop.dev_tasks WHERE dev_type='script' AND (jsonb_exists(content,'notebook_path') OR jsonb_exists(content,'minio_path')) ORDER BY id FOR UPDATE`).Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			var content map[string]interface{}
			if err := json.Unmarshal(row.Content, &content); err != nil {
				return err
			}
			name, nameOK := content["notebook_path"].(string)
			key, keyOK := content["minio_path"].(string)
			_, already := content["notebook_locator"]
			locator := objectstore.ObjectURI(objectstore.Develop, key)
			if !nameOK || !keyOK || already || !strings.HasSuffix(name, ".ipynb") || key != fmt.Sprintf("tenant_%d/notebooks/%s", row.TenantID, name) || locator == "" {
				return fmt.Errorf("Notebook storage migration: task %d has inconsistent file identity", row.ID)
			}
			delete(content, "notebook_path")
			delete(content, "minio_path")
			content["notebook_locator"] = locator
			encoded, err := json.Marshal(content)
			if err != nil {
				return err
			}
			if err := tx.Exec("UPDATE develop.dev_tasks SET content=?::jsonb WHERE id=?", string(encoded), row.ID).Error; err != nil {
				return err
			}
		}
		return tx.Exec("INSERT INTO develop.data_migrations(version,name) VALUES (?,?)", notebookStorageVersion, "notebook_object_reference").Error
	})
}
