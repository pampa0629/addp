package repository

import (
	"fmt"

	"github.com/addp/common/infra/objectstore/upgrade"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
)

// MigrateInfraObjectReferences changes mutable definitions only. Frozen execution
// inputs and authorization hashes remain intact and must be reauthorized normally.
func MigrateInfraObjectReferences(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&models.TransferTask{}) {
		return nil
	}
	var tasks []models.TransferTask
	if err := tx.Unscoped().Where("config::text LIKE ?", "%addp-infra://minio/%").Find(&tasks).Error; err != nil {
		return err
	}
	for _, task := range tasks {
		var active int64
		if err := tx.Table("common.task_executions").Where("module=? AND source_task_id=? AND status IN ?", "transfer", fmt.Sprint(task.ID), []string{"pending", "running"}).Count(&active).Error; err != nil {
			return err
		}
		if active != 0 {
			return fmt.Errorf("Transfer task %d has %d active executions; drain before object reference upgrade", task.ID, active)
		}
		changed := false
		for _, endpointName := range []string{"source", "target"} {
			endpoint, ok := task.Config[endpointName].(map[string]interface{})
			if !ok {
				continue
			}
			for _, field := range []string{"locator", "parent_locator"} {
				value, ok := endpoint[field].(string)
				if !ok {
					continue
				}
				converted, err := upgrade.Reference(value)
				if err != nil {
					return fmt.Errorf("Transfer task %d: %w", task.ID, err)
				}
				if converted != value {
					endpoint[field] = converted
					changed = true
				}
			}
		}
		if changed {
			if err := tx.Unscoped().Model(&models.TransferTask{}).Where("id=?", task.ID).Update("config", task.Config).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
