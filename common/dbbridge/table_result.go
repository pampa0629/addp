package dbbridge

import (
	"context"
	"fmt"
	"github.com/addp/common/models"
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/resourcetree"
	"gorm.io/gorm"
	"strings"
)

// ExecuteTableResult writes a caller-compiled, authorized INSERT SELECT to an existing table.
// Overwrite never commits the deletion separately from the insertion.
func ExecuteTableResult(ctx context.Context, engine *models.Engine, target *resourcetree.ResourceLocator, statement string, parameters map[string]interface{}, mode string) (int64, error) {
	if engine == nil || target == nil || target.EngineID != engine.ID || target.Type != resourcetree.TypeTable || len(target.Path) != 2 || (mode != "append" && mode != "overwrite") {
		return 0, fmt.Errorf("invalid table result target or write mode")
	}
	dialect, err := sqlDialectForEngine(engine.EngineType)
	if err != nil {
		return 0, err
	}
	if dialect != commonquery.DialectPostgreSQL {
		return 0, fmt.Errorf("transactional table results require PostgreSQL dialect")
	}
	statement, args, err := bindSQLExecutionParameters(dialect, statement, parameters)
	if err != nil {
		return 0, err
	}
	db, err := GetOrCreatePool(engine, DefaultPoolConfig())
	if err != nil {
		return 0, err
	}
	table := commonquery.ForDialect(dialect).QualifiedTable(target.Path[0], target.Path[1])
	if !strings.HasPrefix(statement, "INSERT INTO "+table+" ") {
		return 0, fmt.Errorf("compiled result does not match target")
	}
	return executeTableResultTransaction(ctx, db, table, statement, args, mode)
}
func executeTableResultTransaction(ctx context.Context, db *gorm.DB, table, statement string, args []interface{}, mode string) (int64, error) {
	var count int64
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize all writes on this target before evaluating the source query.
		if err := tx.Exec("LOCK TABLE " + table + " IN EXCLUSIVE MODE").Error; err != nil {
			return err
		}
		if mode == "overwrite" {
			if err := tx.Exec("DELETE FROM " + table).Error; err != nil {
				return err
			}
		}
		result := tx.Exec(statement, args...)
		if result.Error != nil {
			return result.Error
		}
		count = result.RowsAffected
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
