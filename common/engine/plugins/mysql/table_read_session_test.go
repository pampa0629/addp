package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	mysqldriver "github.com/go-sql-driver/mysql"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

func TestMySQLSpatialReadExpressionUsesExplicitAxisOrderAndTransform(t *testing.T) {
	column := mysqlColumnInfo{Name: "geom", DataType: "point", SRSID: sql.NullInt64{Int64: 4326, Valid: true}}
	expression, err := mysqlSpatialReadExpression("`geom`", column, map[string]interface{}{
		plugin.TableReadHintGeometryTargetSRID:      3857,
		plugin.TableReadHintGeometryTransformPolicy: "required",
	}, format.GeometryEncodingEWKB)
	if err != nil {
		t.Fatal(err)
	}
	want := "ST_AsWKB(ST_Transform(`geom`, 3857), 'axis-order=long-lat')"
	if expression != want {
		t.Fatalf("expression = %q, want %q", expression, want)
	}
}

func TestMySQLSpatialReadExpressionRejectsRequiredTransformWithoutSourceSRID(t *testing.T) {
	_, err := mysqlSpatialReadExpression("`geom`", mysqlColumnInfo{Name: "geom", DataType: "geometry"}, map[string]interface{}{
		plugin.TableReadHintGeometryTargetSRID:      3857,
		plugin.TableReadHintGeometryTransformPolicy: "required",
	}, format.GeometryEncodingGeoJSON)
	if err == nil || !strings.Contains(err.Error(), "known source SRID") {
		t.Fatalf("error = %v", err)
	}
}

func TestMySQLSpatialInfoFromColumnsAppliesTargetSRID(t *testing.T) {
	columns := []mysqlColumnInfo{{Name: "geom", DataType: "point", NativeType: "point", SRSID: sql.NullInt64{Int64: 4326, Valid: true}}}
	fields := []datatype.FieldInfo{{Name: "geom", Type: datatype.FieldTypeGeometry}}
	info := mysqlSpatialInfoFromColumns(columns, fields, map[string]interface{}{
		plugin.TableReadHintGeometryTargetSRID: 3857,
	}, format.GeometryEncodingEWKB)
	if info == nil || info.GeometryColumns[0].SRID == nil || *info.GeometryColumns[0].SRID != 3857 {
		t.Fatalf("spatial info = %#v", info)
	}
}

func TestMySQLTableSessionClassifiesNativeStreamingDeadline(t *testing.T) {
	for _, shared := range []bool{false, true} {
		db := sql.OpenDB(mysqlDeadlineConnector{})
		t.Cleanup(func() { db.Close() })
		rows, err := db.QueryContext(t.Context(), "SELECT id FROM table_fixture")
		if err != nil {
			t.Fatal(err)
		}
		session := &mysqlTableReadSession{db: db, rows: rows, shared: shared, fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}}
		batch, err := session.ReadBatch(t.Context(), 1)
		var native *mysqldriver.MySQLError
		if batch != nil || plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeSQLExecutionTimeout || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &native) || native.Number != 3024 {
			t.Fatalf("shared=%t streaming deadline lost code/cause: %#v %v", shared, batch, err)
		}
		if err := session.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

type mysqlDeadlineConnector struct{}

func (mysqlDeadlineConnector) Connect(context.Context) (driver.Conn, error) {
	return mysqlDeadlineConn{}, nil
}
func (mysqlDeadlineConnector) Driver() driver.Driver { return mysqlDeadlineDriver{} }

type mysqlDeadlineDriver struct{}

func (mysqlDeadlineDriver) Open(string) (driver.Conn, error) { return mysqlDeadlineConn{}, nil }

type mysqlDeadlineConn struct{}

func (mysqlDeadlineConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (mysqlDeadlineConn) Close() error                        { return nil }
func (mysqlDeadlineConn) Begin() (driver.Tx, error)           { return nil, errors.New("not used") }
func (mysqlDeadlineConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return mysqlDeadlineRows{}, nil
}

type mysqlDeadlineRows struct{}

func (mysqlDeadlineRows) Columns() []string { return []string{"id"} }
func (mysqlDeadlineRows) Close() error      { return nil }
func (mysqlDeadlineRows) Next([]driver.Value) error {
	return &mysqldriver.MySQLError{Number: 3024, Message: "maximum statement execution time exceeded"}
}
