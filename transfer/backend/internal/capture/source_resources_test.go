package capture

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/transfer/internal/models"
)

func TestBuildOracleTransactionObservation(t *testing.T) {
	sampledAt := time.Now()
	empty, err := buildOracleTransactionObservation(0, sql.NullString{}, sql.NullString{}, "0", "0", sampledAt)
	if err != nil || empty.ActiveCount != 0 || empty.OldestDurationSeconds != nil || empty.OldestStartPosition != "" {
		t.Fatalf("empty transaction observation = %#v, %v", empty, err)
	}
	active, err := buildOracleTransactionObservation(1,
		sql.NullString{String: "5852211", Valid: true}, sql.NullString{String: "11", Valid: true}, "1", "3", sampledAt)
	if err != nil || active.ActiveCount != 1 || active.OldestStartPosition != "5852211" || active.OldestDurationSeconds == nil || *active.OldestDurationSeconds != 11 || active.UsedUndoBlocks != "1" || active.UsedUndoRecords != "3" {
		t.Fatalf("active transaction observation = %#v, %v", active, err)
	}
	if _, err := buildOracleTransactionObservation(1, sql.NullString{}, sql.NullString{}, "1", "3", sampledAt); err == nil {
		t.Fatal("invalid active transaction facts succeeded")
	}
}

func TestDatabaseSourceResourcesRejectsChangedConnectionIdentity(t *testing.T) {
	err := (DatabaseSourceResources{}).DropOwnedResources(context.Background(),
		&CapturePlan{SourceType: models.CaptureSourcePostgreSQL, SourceConnectionFingerprint: "new"},
		&models.CaptureResource{
			SourceType: models.CaptureSourcePostgreSQL, SourceConnectionFingerprint: "original",
			PostgreSQL: &models.PostgreSQLCaptureResource{},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "source connection identity changed") {
		t.Fatalf("error = %v", err)
	}
}

func TestOracleSpatialMirrorDDLUsesFrozenFieldsAndGeometryConversion(t *testing.T) {
	plan := &CapturePlan{
		SourceSchema: "BUSINESS", SourceTable: "CUSTOMER_LOCATIONS",
		SourceFields: []string{"ID", "NAME", "SHAPE"}, SourceKeys: []string{"ID"},
		SourceFieldTypes: map[string]datatype.FieldType{
			"ID": datatype.FieldTypeBigInt, "NAME": datatype.FieldTypeString, "SHAPE": datatype.FieldTypeGeometry,
		},
	}
	resource := &models.CaptureResource{Oracle: &models.OracleCaptureResource{
		SpatialMirrorTableName: "ADDP_M_1", SpatialRowTriggerName: "ADDP_R_1", SpatialDDLGuardName: "ADDP_D_1", SpatialArtifactsOwned: true,
	}}
	createSQL, primaryKeySQL, supplementalSQL := oracleSpatialMirrorTableDDL(plan, resource)
	triggerSQL := oracleSpatialMirrorTriggerDDL(plan, resource)
	ddlGuardSQL := oracleSpatialDDLGuardDDL(plan, resource)
	deleteSQL, mergeSQL := oracleSpatialMirrorSynchronizationSQL(plan, resource)
	for _, expected := range []string{
		`SDO_UTIL.TO_WKBGEOMETRY(source_row."SHAPE") AS "SHAPE"`,
		`CREATE TABLE "BUSINESS"."ADDP_M_1"`,
	} {
		if !strings.Contains(createSQL, expected) {
			t.Fatalf("create SQL %q does not contain %q", createSQL, expected)
		}
	}
	if !strings.Contains(primaryKeySQL, `PRIMARY KEY ("ID")`) || !strings.Contains(supplementalSQL, "SUPPLEMENTAL LOG DATA (ALL) COLUMNS") {
		t.Fatalf("primary/supplemental SQL = %q / %q", primaryKeySQL, supplementalSQL)
	}
	if !strings.Contains(triggerSQL, `AFTER INSERT OR UPDATE OR DELETE ON "BUSINESS"."CUSTOMER_LOCATIONS"`) ||
		!strings.Contains(triggerSQL, `SDO_UTIL.TO_WKBGEOMETRY(:NEW."SHAPE")`) ||
		!strings.Contains(triggerSQL, `"ID" = :OLD."ID"`) {
		t.Fatalf("trigger SQL = %q", triggerSQL)
	}
	if !strings.Contains(ddlGuardSQL, "BEFORE ALTER OR DROP OR RENAME ON SCHEMA") ||
		!strings.Contains(ddlGuardSQL, "ORA_DICT_OBJ_NAME = 'CUSTOMER_LOCATIONS'") || !strings.Contains(ddlGuardSQL, "-20042") {
		t.Fatalf("DDL guard SQL = %q", ddlGuardSQL)
	}
	if !strings.Contains(deleteSQL, "NOT EXISTS") || !strings.Contains(mergeSQL, "MERGE INTO") ||
		!strings.Contains(mergeSQL, `SDO_UTIL.TO_WKBGEOMETRY(source_base."SHAPE")`) {
		t.Fatalf("sync SQL = %q / %q", deleteSQL, mergeSQL)
	}
}

func TestDatabaseSourceResourcesAcceptsMySQLWithoutSourceOwnedObjects(t *testing.T) {
	err := (DatabaseSourceResources{}).DropOwnedResources(context.Background(),
		&CapturePlan{SourceType: models.CaptureSourceMySQL, SourceConnectionFingerprint: "same"},
		&models.CaptureResource{
			SourceType: models.CaptureSourceMySQL, SourceConnectionFingerprint: "same",
			MySQL: &models.MySQLCaptureResource{ConnectorServerID: 1, SchemaHistoryTopicName: "history", SchemaHistoryTopicOwned: true},
		})
	if err != nil {
		t.Fatalf("MySQL cleanup error = %v", err)
	}
}

func TestDatabaseSourceResourcesAcceptsOracleWithoutGenerationOwnedDatabaseObjects(t *testing.T) {
	err := (DatabaseSourceResources{}).DropOwnedResources(context.Background(),
		&CapturePlan{SourceType: models.CaptureSourceOracle, SourceConnectionFingerprint: "same"},
		&models.CaptureResource{
			SourceType: models.CaptureSourceOracle, SourceConnectionFingerprint: "same",
			Oracle: &models.OracleCaptureResource{SchemaHistoryTopicName: "history", SchemaHistoryTopicOwned: true},
		})
	if err != nil {
		t.Fatalf("Oracle cleanup error = %v", err)
	}
}
