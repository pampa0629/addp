package repository

import (
	"fmt"

	"github.com/addp/common/infra/objectstore/upgrade"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

func migrateImportObjectReferences(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var rows []models.ImportSource
		if err := tx.Where("source_locator LIKE ?", "addp-infra://minio/%").Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if row.TransferTaskID != nil {
				var active int64
				if err := tx.Table("common.task_executions").Where("module=? AND source_task_id=? AND status IN ?", "transfer", fmt.Sprint(*row.TransferTaskID), []string{"pending", "running"}).Count(&active).Error; err != nil {
					return err
				}
				if active != 0 {
					return fmt.Errorf("import upload %s is bound to an active task; drain before upgrading", row.UploadUUID)
				}
			}
			value, err := upgrade.Reference(row.SourceLocator)
			if err != nil {
				return fmt.Errorf("import upload %s: %w", row.UploadUUID, err)
			}
			if err := tx.Model(&models.ImportSource{}).Where("upload_uuid=? AND source_locator=?", row.UploadUUID, row.SourceLocator).Update("source_locator", value).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
