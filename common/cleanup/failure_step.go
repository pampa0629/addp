package cleanup

import (
	"context"
	"fmt"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"gorm.io/gorm"
	"strings"
)

// RecordFailureStep stores evidence only under the existing C lease. The Owner
// supplies its closed stage; Common owns the single fenced write/event route.
func RecordFailureStep(ctx context.Context, db *gorm.DB, lease execution.Lease, stage string) error {
	if db == nil || stage == "" || len(stage) > 96 || execution.SafeEventStepID(stage) != stage {
		return fmt.Errorf("invalid cleanup failure stage")
	}
	for _, r := range stage {
		if r != '_' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return fmt.Errorf("invalid cleanup failure stage")
		}
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := execution.LockActiveLease(ctx, tx, lease)
		if err != nil {
			return err
		}
		if item.TaskType != execution.TaskTypeCleanupExecutor || item.Source != execution.ModuleSystem || !strings.HasPrefix(stage, item.Module+"_cleanup_") {
			return fmt.Errorf("cleanup failure stage belongs to another Owner")
		}
		if _, err := execution.CleanupRequestFromExecution(*item); err != nil {
			return err
		}
		metadata := item.Metadata.Clone()
		if metadata == nil {
			metadata = models.JSONMap{}
		}
		steps, _ := metadata["step_results"].(map[string]interface{})
		if steps == nil {
			steps = map[string]interface{}{}
		}
		steps[stage] = map[string]interface{}{"status": "failed", "phase": "terminal"}
		metadata["step_results"] = steps
		if err := execution.UpdateWithLease(ctx, tx, lease, map[string]interface{}{"current_step": stage, "metadata": metadata}); err != nil {
			return err
		}
		return execution.AppendBoundedEvent(ctx, tx, lease, execution.EventInput{Kind: "progress", StepID: stage})
	})
}
