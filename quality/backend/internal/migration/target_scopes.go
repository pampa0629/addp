package migration

import (
	"encoding/json"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
)

// Only the last observation's immutable execution snapshot is evidence of scope.
// Missing or invalid historical evidence remains explicitly unrecorded (NULL).
func backfillIssueTargetScopes(tx *gorm.DB) error {
	var afterID int64
	for {
		var rows []struct {
			ID              int64
			ExecutionConfig json.RawMessage
		}
		if err := tx.Raw(`SELECT i.id, e.execution_config FROM quality.issues i
			LEFT JOIN common.task_executions e ON e.tenant_id=i.tenant_id AND e.execution_id=i.last_execution_id
			AND e.module='quality' AND e.task_type='quality_plan' AND e.source_task_id=i.plan_id::text
			WHERE i.target_key IS NULL AND i.id > ? ORDER BY i.id LIMIT 200`, afterID).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			afterID = row.ID
			var config struct {
				TableBindings []models.PlanTableBinding `json:"table_bindings"`
			}
			if json.Unmarshal(row.ExecutionConfig, &config) != nil {
				continue
			}
			key, err := models.PlanTargetKey(config.TableBindings)
			if err != nil {
				continue
			}
			if err := tx.Exec("UPDATE quality.issues SET target_key=? WHERE id=?", key, row.ID).Error; err != nil {
				return err
			}
		}
	}
}
