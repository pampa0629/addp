package capture

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	oracleplugin "github.com/addp/common/engine/plugins/oracle"
	"github.com/addp/transfer/internal/models"
)

func TestIntegrationOracleSpatialMirrorLifecycle(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_SPATIAL_CDC_INTEGRATION") != "1" {
		t.Skip("set ADDP_ORACLE_SPATIAL_CDC_INTEGRATION=1 to run Oracle Spatial CDC integration test")
	}
	port, err := strconv.Atoi(oracleSpatialIntegrationEnv("ADDP_TEST_ORACLE_PORT", "ORACLE_PORT", "15210"))
	if err != nil {
		t.Fatal(err)
	}
	connInfo := engineplugin.ConnectionInfo{
		"host": oracleSpatialIntegrationEnv("ADDP_TEST_ORACLE_HOST", "", "127.0.0.1"), "port": port,
		"service_name": oracleSpatialIntegrationEnv("ADDP_TEST_ORACLE_SERVICE_NAME", "ORACLE_SERVICE_NAME", "FREEPDB1"),
		"user":         oracleSpatialIntegrationEnv("ADDP_TEST_ORACLE_USER", "ORACLE_APP_USER", "business"),
		"password":     oracleSpatialIntegrationEnv("ADDP_TEST_ORACLE_PASSWORD", "ORACLE_APP_PASSWORD", "business_oracle_password"),
	}
	schema := strings.ToUpper(engineplugin.GetString(connInfo, "user"))
	plan := &CapturePlan{
		SourceType: models.CaptureSourceOracle, SourceConnInfo: connInfo,
		SourceConnectionFingerprint: "oracle-spatial-integration", SourceSchema: schema, SourceTable: "CUSTOMER_LOCATIONS",
		SourceFields: []string{"ID", "NAME", "SHAPE"}, SourceKeys: []string{"ID"},
		SourceFieldTypes: map[string]datatype.FieldType{
			"ID": datatype.FieldTypeBigInt, "NAME": datatype.FieldTypeString, "SHAPE": datatype.FieldTypeGeometry,
		},
		SourceSpatialInfo: datatype.NewSingleGeometrySpatialInfo("SHAPE", "Point", 4326, 2),
	}
	resource := &models.CaptureResource{
		ID: 900001, SourceType: models.CaptureSourceOracle, SourceConnectionFingerprint: plan.SourceConnectionFingerprint,
		SourceSchema: schema, SourceTable: plan.SourceTable,
		Oracle: &models.OracleCaptureResource{
			SpatialMirrorTableName: "ADDP_M_IT", SpatialRowTriggerName: "ADDP_R_IT", SpatialDDLGuardName: "ADDP_D_IT", SpatialArtifactsOwned: true,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	control := DatabaseSourceResources{}
	if err := control.DropOwnedResources(ctx, plan, resource); err != nil {
		t.Fatalf("clean stale integration resources: %v", err)
	}
	defer func() {
		if err := control.DropOwnedResources(context.Background(), plan, resource); err != nil {
			t.Errorf("cleanup Oracle Spatial integration resources: %v", err)
		}
	}()
	if err := control.EnsureOwnedResources(ctx, plan, resource); err != nil {
		t.Fatal(err)
	}
	items, err := (&oracleplugin.OraclePlugin{}).ListChildren(ctx, connInfo,
		engineplugin.TabularNamespacePath(92001, engineplugin.EngineCatalogTermSchema, schema), engineplugin.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Name == resource.Oracle.SpatialMirrorTableName {
			t.Fatalf("Oracle Catalog exposed internal Spatial mirror table %#v", item)
		}
	}
	db, err := openOracle(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, ddlErr := db.ExecContext(ctx, `ALTER TABLE "BUSINESS"."CUSTOMER_LOCATIONS" ADD "ADDP_DDL_PROBE" NUMBER`); ddlErr == nil {
		if dropErr := control.DropOwnedResources(ctx, plan, resource); dropErr != nil {
			t.Fatalf("DDL guard missing and cleanup failed: %v", dropErr)
		}
		_, _ = db.ExecContext(ctx, `ALTER TABLE "BUSINESS"."CUSTOMER_LOCATIONS" DROP COLUMN "ADDP_DDL_PROBE"`)
		t.Fatal("Oracle Spatial DDL guard allowed source table ALTER")
	} else if !strings.Contains(ddlErr.Error(), "ORA-20042") {
		t.Fatalf("Oracle Spatial DDL guard error = %v", ddlErr)
	}
	defer db.ExecContext(context.Background(), `DELETE FROM "BUSINESS"."CUSTOMER_LOCATIONS" WHERE "ID" = 990010`)
	if _, err := db.ExecContext(ctx, `INSERT INTO "BUSINESS"."CUSTOMER_LOCATIONS" ("ID", "NAME", "SHAPE") VALUES (990010, 'Spatial CDC integration', MDSYS.SDO_GEOMETRY(2001, 4326, MDSYS.SDO_POINT_TYPE(116.397, 39.908, NULL), NULL, NULL))`); err != nil {
		t.Fatal(err)
	}
	var mirrorRows, wkbBytes int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(DBMS_LOB.GETLENGTH("SHAPE")), 0) FROM "BUSINESS"."ADDP_M_IT" WHERE "ID" = 990010`).Scan(&mirrorRows, &wkbBytes); err != nil {
		t.Fatal(err)
	}
	if mirrorRows != 1 || wkbBytes == 0 {
		t.Fatalf("Oracle Spatial mirror row count=%d wkb_bytes=%d", mirrorRows, wkbBytes)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM "BUSINESS"."CUSTOMER_LOCATIONS" WHERE "ID" = 990010`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "BUSINESS"."ADDP_M_IT" WHERE "ID" = 990010`).Scan(&mirrorRows); err != nil {
		t.Fatal(err)
	}
	if mirrorRows != 0 {
		t.Fatalf("Oracle Spatial mirror delete left %d rows", mirrorRows)
	}
	if err := control.DropOwnedResources(ctx, plan, resource); err != nil {
		t.Fatal(err)
	}
}

func oracleSpatialIntegrationEnv(primary, secondary, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(primary)); value != "" {
		return value
	}
	if secondary != "" {
		if value := strings.TrimSpace(os.Getenv(secondary)); value != "" {
			return value
		}
	}
	return fallback
}
