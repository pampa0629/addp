// Package cleanup supplies the single Cleanup runtime and result protocol.
// Owners retain their domain selection, mutations and closed statistics fields.
package cleanup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const ResultSchema = "addp.cleanup-professional-result/v1"

// Result contains Owner-projected counters only. Arbitrary provider errors,
// paths, SQL and unbounded object lists cannot be represented by this protocol.
type Result struct {
	Schema      string                      `json:"schema"`
	ExecutionID string                      `json:"execution_id"`
	RequestID   string                      `json:"request_id"`
	Module      string                      `json:"module"`
	Attempt     int                         `json:"attempt"`
	Action      string                      `json:"action"`
	CleanupMode string                      `json:"cleanup_mode,omitempty"`
	Status      string                      `json:"status"`
	Summary     events.CleanupResultSummary `json:"summary"`
	Statistics  map[string]int64            `json:"statistics"`
	Reason      string                      `json:"reason,omitempty"`
}

type resultRecord struct {
	ExecutionID   string    `gorm:"primaryKey;size:36"`
	TenantID      int       `gorm:"not null"`
	Attempt       int       `gorm:"not null"`
	RequestDigest string    `gorm:"size:64;not null"`
	Payload       string    `gorm:"type:text;not null"`
	CreatedAt     time.Time `gorm:"not null"`
}

func resultTable(owner string) (string, error) {
	if !events.CleanupModuleValid(owner) {
		return "", fmt.Errorf("invalid cleanup Owner")
	}
	return owner + ".cleanup_results", nil
}

// MigrateResults is called by the Owner's standard schema migration. Each Owner
// stores its own rows; System never reads these tables.
func MigrateResults(db *gorm.DB, owner string) error {
	table, err := resultTable(owner)
	if err != nil {
		return err
	}
	if err := db.Table(table).AutoMigrate(&resultRecord{}); err != nil {
		return err
	}
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	return db.Exec(`CREATE OR REPLACE FUNCTION ` + owner + `.guard_cleanup_result_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'cleanup result is immutable'; END; $$;
DROP TRIGGER IF EXISTS cleanup_result_immutable ON ` + table + `;
CREATE TRIGGER cleanup_result_immutable BEFORE UPDATE ON ` + table + ` FOR EACH ROW EXECUTE FUNCTION ` + owner + `.guard_cleanup_result_immutable();`).Error
}

var counterKey = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){0,3}$`)

func validateResult(value Result, receipt execution.CleanupReceipt) error {
	if value.Schema != ResultSchema || value.ExecutionID != receipt.ExecutionID || value.RequestID != receipt.RequestID || value.Module != receipt.Module || value.Attempt != receipt.Attempt || value.Action != receipt.Action || value.CleanupMode != receipt.CleanupMode || value.Status != receipt.Status || value.Reason != receipt.Reason || value.Summary != receipt.Summary || value.Statistics == nil || len(value.Statistics) > 128 {
		return fmt.Errorf("invalid professional cleanup result")
	}
	for key, n := range value.Statistics {
		if !counterKey.MatchString(key) || n < 0 || n > events.CleanupCountMax {
			return fmt.Errorf("invalid cleanup statistic")
		}
	}
	data, err := json.Marshal(value)
	if err != nil || len(data) > events.CleanupProtocolMaxBytes {
		return fmt.Errorf("cleanup professional result exceeds limit")
	}
	return nil
}

// Complete is composed inside the Owner transaction holding C before domain
// locks. Private result, public receipt and terminal are committed together.
func Complete(ctx context.Context, tx *gorm.DB, lease execution.Lease, receipt execution.CleanupReceipt, statistics map[string]int64) error {
	return tx.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		item, err := execution.LockActiveLease(ctx, db, lease)
		if err != nil {
			return err
		}
		if err := receipt.Validate(*item); err != nil {
			return err
		}
		value := Result{Schema: ResultSchema, ExecutionID: item.ExecutionID, RequestID: receipt.RequestID, Module: item.Module, Attempt: item.Attempt, Action: receipt.Action, CleanupMode: receipt.CleanupMode, Status: receipt.Status, Summary: receipt.Summary, Statistics: statistics, Reason: receipt.Reason}
		if err := validateResult(value, receipt); err != nil {
			return err
		}
		payload, err := json.Marshal(value)
		if err != nil {
			return err
		}
		now, err := execution.DatabaseNow(db)
		if err != nil {
			return err
		}
		table, err := resultTable(item.Module)
		if err != nil {
			return err
		}
		record := resultRecord{ExecutionID: item.ExecutionID, TenantID: item.TenantID, Attempt: item.Attempt, RequestDigest: receipt.RequestDigest, Payload: string(payload), CreatedAt: now}
		if err := db.Table(table).Create(&record).Error; err != nil {
			return err
		}
		return execution.CompleteCleanupWithReceipt(ctx, db, lease, receipt)
	})
}

// ReadResultHandler is attached to an Owner route, with its explicit new
// Permission. It never proxies a User or reads a different Owner's table.
func ReadResultHandler(db *gorm.DB, owner string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if db == nil || !events.CleanupModuleValid(owner) {
			commonapi.RespondError(c, http.StatusServiceUnavailable, commoni18n.T(c, commoni18n.MsgCleanupResultUnavailable))
			return
		}
		scope, ok := execution.OwnerReadScopeFromGin(c, owner, map[string]string{execution.TaskTypeCleanupExecutor: "system.cleanup.read"})
		if !ok || !commonauth.HasRolePermission(c, owner+".cleanup_result.read") {
			commonapi.RespondError(c, http.StatusForbidden, commoni18n.T(c, commoni18n.MsgForbidden))
			return
		}
		id, parseErr := uuid.Parse(c.Param("execution_id"))
		if parseErr != nil || id == uuid.Nil || id.String() != c.Param("execution_id") {
			commonapi.RespondError(c, http.StatusBadRequest, commoni18n.T(c, commoni18n.MsgInvalidParams))
			return
		}
		ctx := execution.WithReadScopes(c.Request.Context(), []execution.ReadScope{*scope})
		var result Result
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var child execution.TaskExecution
			if err := execution.ApplyReadScopes(ctx, tx).Where("execution_id = ? AND tenant_id = ? AND module = ? AND task_type = ? AND source = ? AND source_task_id IS NULL AND triggered_by = ?", c.Param("execution_id"), scope.TenantID, owner, execution.TaskTypeCleanupExecutor, execution.ModuleSystem, scope.PrincipalID).First(&child).Error; err != nil {
				return err
			}
			receipt, err := execution.ReadCleanupReceipt(child)
			if err != nil {
				return err
			}
			if receipt == nil {
				return commonapi.ErrConflict
			}
			table, _ := resultTable(owner)
			var row resultRecord
			if err := tx.Table(table).Where("execution_id = ? AND tenant_id = ? AND attempt = ?", child.ExecutionID, child.TenantID, child.Attempt).First(&row).Error; err != nil {
				return err
			}
			if row.RequestDigest != receipt.RequestDigest {
				return fmt.Errorf("cleanup result binding mismatch")
			}
			if err := events.CleanupStrictDecode([]byte(row.Payload), &result); err != nil {
				return err
			}
			return validateResult(result, *receipt)
		}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		if err != nil {
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				commonapi.RespondError(c, http.StatusNotFound, commoni18n.T(c, commoni18n.MsgCleanupResultNotFound))
			case errors.Is(err, commonapi.ErrConflict):
				c.JSON(http.StatusConflict, gin.H{"error_code": "cleanup_result_not_ready", "error": commoni18n.T(c, commoni18n.MsgCleanupResultNotReady)})
			default:
				commonapi.RespondError(c, http.StatusServiceUnavailable, commoni18n.T(c, commoni18n.MsgCleanupResultUnavailable))
			}
			return
		}
		commonapi.RespondSuccess(c, result)
	}
}
