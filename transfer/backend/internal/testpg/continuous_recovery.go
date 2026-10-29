package testpg

import (
	"fmt"
	"testing"

	"gorm.io/gorm"
)

// MakeContinuousRecoveryEligible advances only this test's persisted backoff
// fixture. Production claims always use the database clock, never a test clock.
func MakeContinuousRecoveryEligible(t testing.TB, db *gorm.DB, taskID, tenantID uint) {
	t.Helper()
	result := db.Exec(`UPDATE common.task_executions
		SET metadata = jsonb_set(metadata, '{recovery_not_before}', to_jsonb(clock_timestamp() - interval '1 second'))
		WHERE tenant_id = ? AND module = 'transfer' AND task_type = 'sync'
		  AND source_task_id = ? AND execution_boundary = 'continuous' AND status = 'pending'
		  AND metadata->>'recovery_not_before' IS NOT NULL`, tenantID, fmt.Sprint(taskID))
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("make owned recovery fixture eligible: rows=%d error=%v", result.RowsAffected, result.Error)
	}
}
