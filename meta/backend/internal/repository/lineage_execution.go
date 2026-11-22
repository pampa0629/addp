package repository

import (
	"github.com/addp/common/execution"
	"gorm.io/gorm"
)

// UnobservedLineageExecutions selects the terminal inputs for one collector sweep.
// The collector commits all observations and projections of an execution together.
func UnobservedLineageExecutions(db *gorm.DB, terminalStatuses []string) *gorm.DB {
	facts := "jsonb_exists(metadata, 'lineage_facts')"
	if db.Dialector.Name() == "mysql" {
		facts = "JSON_CONTAINS_PATH(metadata, 'one', '$.lineage_facts')"
	}
	return db.Model(&execution.TaskExecution{}).Where("status IN ?", terminalStatuses).
		Where(facts).Where(`NOT EXISTS (SELECT 1 FROM meta.lineage_observations AS observation
			WHERE observation.tenant_id = task_executions.tenant_id
			AND observation.execution_id = task_executions.execution_id)`)
}
