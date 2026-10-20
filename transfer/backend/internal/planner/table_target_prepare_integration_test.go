package planner

import (
	"fmt"
	"os"
	"testing"

	plugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/transfer/internal/executor"
)

func TestIntegrationPlannerTargetPreparationOutcomes(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run make test-transfer-postgres for real PostgreSQL target preparation")
	}
	ctx := t.Context()
	conn := plannerIntegrationPostgresConnInfo(t)
	db := openPlannerIntegrationPostgres(t, ctx, &postgresql.PostgreSQLPlugin{}, conn)
	schema := plannerIntegrationPostgresTestSchema(t, ctx, db)
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE "%[1]s".source (id bigint); INSERT INTO "%[1]s".source VALUES (1),(2)`, schema)); err != nil {
		t.Fatal(err)
	}
	var previousOID int64
	for _, step := range []struct {
		name        string
		replace     bool
		disposition plugin.TableTargetDisposition
		rows        int
	}{
		{"first create", true, plugin.TableTargetCreated, 2},
		{"replace existing", true, plugin.TableTargetRecreated, 2},
		{"append existing", false, plugin.TableTargetReused, 4},
	} {
		t.Run(step.name, func(t *testing.T) {
			pipeline, err := executor.NewTableTransferExecutor("postgresql", "postgresql", "", "")
			if err != nil {
				t.Fatal(err)
			}
			metrics, err := pipeline.Execute(ctx, executor.TableTransferPlan{
				Source: executor.TableSourcePlan{Kind: executor.TableEndpointNative, ConnInfo: conn, Path: plugin.TabularItemPath(1, plugin.EngineCatalogTermSchema, schema, "source")},
				Target: executor.TableTargetPlan{Kind: executor.TableEndpointNative, ConnInfo: conn, Path: plugin.TabularItemPath(1, plugin.EngineCatalogTermSchema, schema, "target"), DeleteBeforeWrite: step.replace, TableWrite: plugin.BatchWriteOptions{Method: "copy"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if metrics.TargetPreparation.Disposition != step.disposition || metrics.RecordsWritten != 2 {
				t.Fatalf("target operation was not preserved: %+v", metrics)
			}
			if metrics.TargetCreatedAndCommitted != (step.disposition == plugin.TableTargetCreated) {
				t.Fatalf("created-table commit evidence was lost or inferred from replace/append: %s", step.disposition)
			}
			if metrics.SourceReadSet == nil || len(metrics.SourceReadSet.Paths) != 1 || metrics.SourceReadSet.Paths[0].Segments[2].Name != "source" {
				t.Fatalf("actual PG cursor source evidence was lost: %+v", metrics.SourceReadSet)
			}
			var count int
			if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM "%s".target`, schema)).Scan(&count); err != nil || count != step.rows {
				t.Fatalf("actual rows=%d want=%d error=%v", count, step.rows, err)
			}
			var oid int64
			if err := db.QueryRowContext(ctx, "SELECT $1::regclass::oid", fmt.Sprintf(`"%s".target`, schema)).Scan(&oid); err != nil {
				t.Fatal(err)
			}
			if step.disposition == plugin.TableTargetRecreated && oid == previousOID {
				t.Fatal("replace stopped rebuilding the table")
			}
			if step.disposition == plugin.TableTargetReused && oid != previousOID {
				t.Fatal("append rebuilt the table")
			}
			previousOID = oid
		})
	}
}
