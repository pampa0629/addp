package planner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	plugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/engine/plugins/rustfs"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/parquet"
	"github.com/addp/common/production"
	"github.com/addp/transfer/internal/executor"
)

type countedRustFSReader struct {
	plugin.ContentReadableProvider
	opens int
}

func (r *countedRustFSReader) OpenContent(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, options plugin.ReadOptions) (io.ReadCloser, error) {
	r.opens++
	return r.ContentReadableProvider.OpenContent(ctx, conn, path, options)
}

// The owned RustFS gate supplies random credentials and a disposable PostgreSQL target.
// System's real-DB authorization decisions are verified separately by its IAM gate.
func TestIntegrationRustFSParquetImport(t *testing.T) {
	if os.Getenv("ADDP_RUSTFS_INTEGRATION") != "1" {
		t.Skip("run make test-common-rustfs")
	}
	ctx := t.Context()
	p := &rustfs.RustFSPlugin{}
	conn := plugin.ConnectionInfo{"endpoint": os.Getenv("ADDP_RUSTFS_TEST_ENDPOINT"), "access_key": os.Getenv("ADDP_RUSTFS_TEST_ACCESS_KEY"), "secret_key": os.Getenv("ADDP_RUSTFS_TEST_SECRET_KEY"), "use_ssl": false}
	path := plugin.ObjectItemPath(91, os.Getenv("ADDP_RUSTFS_TEST_BUCKET"), "transfer/orders.parquet")
	fields := []datatype.FieldInfo{{Name: "order_id", Type: datatype.FieldTypeString}, {Name: "customer_id", Type: datatype.FieldTypeString}, {Name: "product_id", Type: datatype.FieldTypeString},
		{Name: "quantity", Type: datatype.FieldTypeInt}, {Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 12, Scale: 2}, {Name: "order_date", Type: datatype.FieldTypeDate},
		{Name: "paid", Type: datatype.FieldTypeBool}, {Name: "status", Type: datatype.FieldTypeString}, {Name: "note", Type: datatype.FieldTypeString, Nullable: true}}
	var buffer bytes.Buffer
	writer, err := parquet.NewPlugin().OpenTableWriter(ctx, &buffer, &datatype.TableInfo{Fields: fields}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 20; i++ {
		amount := i * 1010
		var note any
		if i%3 != 0 {
			note = "中文示范"
		}
		if i%5 == 0 {
			note = ""
		}
		if err := writer.WriteRows(ctx, []map[string]interface{}{{"order_id": fmt.Sprintf("DEMO%04d", i), "customer_id": "C01", "product_id": "P01", "quantity": i%3 + 1, "amount": fmt.Sprintf("%d.%02d", amount/100, amount%100), "order_date": fmt.Sprintf("2026-11-%02d", i), "paid": i%2 == 0, "status": "completed", "note": note}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	out, err := p.CreateContent(ctx, conn, path, plugin.WriteOptions{ContentType: "application/vnd.apache.parquet"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write(buffer.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	pgConn := plannerIntegrationPostgresConnInfo(t)
	db := openPlannerIntegrationPostgres(t, ctx, &postgresql.PostgreSQLPlugin{}, pgConn)
	schema := plannerIntegrationPostgresTestSchema(t, ctx, db)
	allowed := true
	checks := 0
	reader := &countedRustFSReader{ContentReadableProvider: p}
	run := func(name string, revoke bool) (*executor.TablePipelineMetrics, error) {
		pipeline, err := executor.NewTableTransferExecutor("rustfs", "postgresql", format.FormatParquet, "")
		if err != nil {
			return nil, err
		}
		pipeline.SourceContentReader = reader
		pipeline.AuthorizeObjectSource = func(_ context.Context, actual plugin.EngineCatalogPath) error {
			checks++
			if !allowed || !reflect.DeepEqual(path, actual) {
				return fmt.Errorf("exact object authorization denied")
			}
			return nil
		}
		plan := executor.TableTransferPlan{BatchSize: 7, Source: executor.TableSourcePlan{Kind: executor.TableEndpointEncoded, ConnInfo: conn, Path: path, EngineVersion: 17, Format: format.FormatParquet, Layout: format.LayoutSingle},
			Target: executor.TableTargetPlan{Kind: executor.TableEndpointNative, ConnInfo: pgConn, Path: plugin.TabularItemPath(92, "schema", schema, name), DeleteBeforeWrite: true, TableWrite: plugin.BatchWriteOptions{Method: "copy"}}}
		if revoke {
			plan.ProgressCallback = func(_ context.Context, event executor.TableProgressEvent) error {
				if event.RecordsWritten >= 7 {
					allowed = false
				}
				return nil
			}
		}
		return pipeline.Execute(ctx, plan)
	}
	allowed = false
	if _, err := run("denied", false); err == nil || reader.opens != 0 {
		t.Fatal("denied source performed content I/O", err, reader.opens)
	}
	allowed = true
	metrics, err := run("orders", false)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.RecordsRead != 20 || metrics.RecordsWritten != 20 || metrics.Batches != 3 || checks < 6 {
		t.Fatalf("incorrect bounded import: %+v checks=%d", metrics, checks)
	}
	actualSources, _ := plugin.NewQueryReadSet(path)
	if !reflect.DeepEqual(metrics.SourceReadSet, actualSources) || !reflect.DeepEqual(metrics.SourceEngineVersions, []production.EngineVersion{{EngineID: 91, Version: 17}}) {
		t.Fatalf("actual object read set or connection version lost: %+v", metrics)
	}
	var count, columns int
	var total, minDate, maxDate string
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*),sum(amount)::text,min(order_date)::text,max(order_date)::text FROM "%s".orders`, schema)).Scan(&count, &total, &minDate, &maxDate); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=$1 AND table_name='orders'`, schema).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if count != 20 || columns != 9 || total != "2121.00" || minDate != "2026-11-01" || maxDate != "2026-11-20" {
		t.Fatalf("target facts rows=%d columns=%d total=%s dates=%s..%s", count, columns, total, minDate, maxDate)
	}
	var names, dateType string
	var precision, scale int
	if err := db.QueryRowContext(ctx, `SELECT string_agg(column_name,',' ORDER BY column_name) FROM information_schema.columns WHERE table_schema=$1 AND table_name='orders'`, schema).Scan(&names); err != nil {
		t.Fatal(err)
	}
	if names != "amount,customer_id,note,order_date,order_id,paid,product_id,quantity,status" {
		t.Fatalf("target fields changed: %s", names)
	}
	if err := db.QueryRowContext(ctx, `SELECT numeric_precision,numeric_scale FROM information_schema.columns WHERE table_schema=$1 AND table_name='orders' AND column_name='amount' AND data_type='numeric'`, schema).Scan(&precision, &scale); err != nil || precision != 12 || scale != 2 {
		t.Fatal("target lost Decimal(12,2)", precision, scale, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT data_type FROM information_schema.columns WHERE table_schema=$1 AND table_name='orders' AND column_name='order_date'`, schema).Scan(&dateType); err != nil || dateType != "date" {
		t.Fatal("target lost DATE", dateType, err)
	}
	if _, err := run("revoked", true); err == nil || !errors.Is(err, executor.ErrTargetMayHaveChanged) {
		t.Fatal("withdrawal lost source failure or target warning", err)
	}
	// The existing preparer commits DDL before the COPY session. Withdrawal
	// rolls back its rows; the empty table remains and must carry the risk marker.
	var remaining int
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM "%s".revoked`, schema)).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("withdrawal committed rows", remaining, err)
	}

	t.Log("RustFS Parquet -> PostgreSQL: 9 fields, 20 rows, DECIMAL 2121.00, DATE range; no-Grant opens=0; batch withdrawal rolled back rows and preserved target-change warning")
}
