package repository

import (
	"testing"

	"github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
)

// Included in the existing registered owner migration PostgreSQL test.
func exerciseProductionAuthorizationRetirement(t *testing.T, db *gorm.DB, item *execution.TaskExecution) {
	t.Helper()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec(`CREATE SCHEMA IF NOT EXISTS transfer;
 CREATE TABLE IF NOT EXISTS transfer.startup_schema_revision(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),version bigint NOT NULL);
 INSERT INTO transfer.startup_schema_revision VALUES(true,6) ON CONFLICT(singleton) DO UPDATE SET version=6`).Error; err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"pending", "issued", "denied"} {
		t.Run("retire_"+status, func(t *testing.T) {
			state := models.ProductionAuthorization{SchemaVersion: "transfer.initial-authorization/v1", Status: status, Attempts: 3, NextAttemptUnix: 100}
			if status == "issued" {
				state.GrantRequestID = "00000000-0000-4000-8000-000000000001"
			}
			item.Metadata[models.ProductionAuthorizationMetadataKey] = state
			if err := tx.Model(item).Update("metadata", item.Metadata).Error; err != nil {
				t.Fatal(err)
			}
			snapshot := func() string {
				var value string
				if err := tx.Raw(`SELECT (to_jsonb(t)-'metadata')::text || (metadata-'initial_authorization')::text FROM common.task_executions t WHERE id=?`, item.ID).Scan(&value).Error; err != nil {
					t.Fatal(err)
				}
				return value
			}
			before := snapshot()
			if err := RetireDevelopmentAuthorizationHistory(tx); err != nil {
				t.Fatal(err)
			}
			var stored execution.TaskExecution
			if err := tx.First(&stored, item.ID).Error; err != nil {
				t.Fatal(err)
			}
			retired, err := models.DecodeProductionAuthorization(stored.Metadata[models.ProductionAuthorizationMetadataKey])
			if err != nil || retired.Status != "denied" || retired.Code != models.ProductionAuthorizationRetired || retired.GrantRequestID != "" || retired.Attempts != 3 {
				t.Fatalf("state=%+v %v", retired, err)
			}
			if snapshot() != before {
				t.Fatal("retirement modified physical execution or unrelated metadata")
			}
		})
	}
	if err := tx.Exec("UPDATE transfer.startup_schema_revision SET version=7").Error; err != nil {
		t.Fatal(err)
	}
	item.Metadata[models.ProductionAuthorizationMetadataKey] = models.ProductionAuthorization{SchemaVersion: "transfer.initial-authorization/v1", Status: "pending"}
	if err := tx.Model(item).Update("metadata", item.Metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := RetireDevelopmentAuthorizationHistory(tx); err != nil {
		t.Fatal(err)
	}
	var current execution.TaskExecution
	if err := tx.First(&current, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	state, err := models.DecodeProductionAuthorization(current.Metadata[models.ProductionAuthorizationMetadataKey])
	if err != nil || state.Status != "pending" {
		t.Fatalf("later startup retired a new authorization: %+v %v", state, err)
	}
	if err := RetireDevelopmentAuthorizationHistory(db); err == nil {
		t.Fatal("retirement accepted outside owner transaction")
	}
}
