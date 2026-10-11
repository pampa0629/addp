package planner

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/transfer/internal/executor"
)

func TestIntegrationPlannerWriteDefinitionBeforeReplace(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard PostgreSQL gate")
	}
	ctx := context.Background()
	conn := plannerIntegrationPostgresConnInfo(t)
	pg := &postgresql.PostgreSQLPlugin{}
	db := openPlannerIntegrationPostgres(t, ctx, pg, conn)
	schema := plannerIntegrationPostgresTestSchema(t, ctx, db)
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE "%s".source (members integer[]); INSERT INTO "%s".source VALUES (ARRAY[1,2]); CREATE TABLE "%s".target (sentinel text); INSERT INTO "%s".target VALUES ('preserved')`, schema, schema, schema, schema)); err != nil {
		t.Fatal(err)
	}
	var originalOID uint32
	if err := db.QueryRowContext(ctx, `SELECT c.oid FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname='target'`, schema).Scan(&originalOID); err != nil {
		t.Fatal(err)
	}
	spec := overridableNativeTableSpec()
	spec.Source.Locator = tableLocator(1, schema, "source")
	spec.Target.ParentLocator = schemaLocator(2, schema)
	spec.Target.Name = "target"
	spec.Target.OverridePolicy = ""
	spec.Target.Policy = map[string]interface{}{"apply_mode": "replace"}
	spec.Transforms[0].Fields = []FieldMappingSpec{{Source: "members", Target: "members", TargetType: "array"}}
	caps := pg.Capabilities()
	build, err := BuildTableTransferPlan(spec, StaticEngineResolver{1: {Type: "postgresql", ConnInfo: conn, Capabilities: &caps}, 2: {Type: "postgresql", ConnInfo: conn, Capabilities: &caps}})
	if err != nil {
		t.Fatal(err)
	}
	runner := &executor.TableTransferExecutor{SourceNativeReader: pg, SourceTableSessionProvider: pg, TargetNativePreparer: pg, TargetTableSessionProvider: pg, TargetDeleteProvider: pg}
	if _, err := runner.Execute(ctx, build.Plan); err == nil || !strings.Contains(err.Error(), "array_element_type_unsupported") {
		t.Fatalf("expected pre-effect rejection; got %v", err)
	}
	var sentinel string
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT sentinel FROM "%s".target`, schema)).Scan(&sentinel); err != nil || sentinel != "preserved" {
		t.Fatalf("target changed: sentinel=%q err=%v", sentinel, err)
	}
	var sourceCount int
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM "%s".source`, schema)).Scan(&sourceCount); err != nil || sourceCount != 1 {
		t.Fatalf("source changed: %d %v", sourceCount, err)
	}
	report := pg.CheckTableWriteDefinition(engineplugin.TableWriteDefinition{Fields: []engineplugin.TableWriteFieldDefinition{{Target: datatype.FieldInfo{Name: "members", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON}}}})
	if report.UnsupportedError() == nil {
		t.Fatal("object array declaration unexpectedly supported")
	}
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT sentinel FROM "%s".target`, schema)).Scan(&sentinel); err != nil || sentinel != "preserved" {
		t.Fatalf("pure check changed target: %v", err)
	}
	var finalOID uint32
	if err := db.QueryRowContext(ctx, `SELECT c.oid FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname='target'`, schema).Scan(&finalOID); err != nil || finalOID != originalOID {
		t.Fatalf("target identity changed: before=%d after=%d error=%v", originalOID, finalOID, err)
	}
}
