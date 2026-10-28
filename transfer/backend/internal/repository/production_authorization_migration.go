package repository

import (
	"fmt"

	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
)

// Called under schema.Migrate's transaction and module lock. Revision 7 retires
// delivery only; physical task outcomes, provenance and output data stay intact.
func RetireDevelopmentAuthorizationHistory(tx *gorm.DB) error {
	if tx == nil || tx.Dialector.Name() != "postgres" {
		return fmt.Errorf("authorization retirement requires PostgreSQL")
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return fmt.Errorf("authorization retirement requires owner migration transaction")
	}
	var revision int64
	if err := tx.Raw("SELECT COALESCE(MAX(version), 0) FROM transfer.startup_schema_revision").Scan(&revision).Error; err != nil {
		return err
	}
	if revision >= 7 {
		return nil
	}
	return tx.Exec(`UPDATE common.task_executions
 SET metadata = jsonb_set(metadata, '{initial_authorization}',
   ((metadata->'initial_authorization') - 'grant_request_id') ||
   jsonb_build_object('status','denied','code',CAST(? AS text),'next_attempt_unix',0), false)
 WHERE module='transfer' AND task_type='sync' AND execution_boundary='bounded'
   AND status='success' AND jsonb_typeof(metadata->'initial_authorization')='object'`,
		models.ProductionAuthorizationRetired).Error
}
