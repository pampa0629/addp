package plugin

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

type singleStatementRecord struct {
	query                  string
	args                   []driver.NamedValue
	queries, begins, pools int
	failure                error
}

type singleStatementConnector struct{ record *singleStatementRecord }

func (c singleStatementConnector) Driver() driver.Driver { return resultSchemaDriver{} }
func (c singleStatementConnector) Connect(context.Context) (driver.Conn, error) {
	return &singleStatementConn{record: c.record}, nil
}

type singleStatementConn struct {
	resultSchemaConn
	record *singleStatementRecord
}

func (c *singleStatementConn) Begin() (driver.Tx, error) {
	c.record.begins++
	return nil, errors.New("explicit transactions are unavailable")
}
func (c *singleStatementConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.record.queries++
	c.record.query, c.record.args = query, append([]driver.NamedValue(nil), args...)
	if c.record.failure != nil {
		return nil, c.record.failure
	}
	return &resultSchemaRows{query: query}, nil
}

type singleStatementProvider struct {
	fakeSQLRuntimeProvider
	record   *singleStatementRecord
	boundary ControlledReadOnlySQLBoundary
}

func (p *singleStatementProvider) ControlledReadOnlySQLBoundary() ControlledReadOnlySQLBoundary {
	return p.boundary
}
func (*singleStatementProvider) SQLDialect() string  { return "mysql" }
func (*singleStatementProvider) GORMDialect() string { return "mysql" }
func (p *singleStatementProvider) CreateConnectionPool(ConnectionInfo, *PoolConfig) (*gorm.DB, error) {
	p.record.pools++
	db := sql.OpenDB(singleStatementConnector{record: p.record})
	return gorm.Open(singleStatementDialect{db: db}, &gorm.Config{})
}

// The fixture only exposes the database/sql pool; it does not import another
// database dialect into the shared package's test dependency graph.
type singleStatementDialect struct{ db *sql.DB }

func (singleStatementDialect) Name() string                                   { return "single-statement-test" }
func (d singleStatementDialect) Initialize(db *gorm.DB) error                 { db.ConnPool = d.db; return nil }
func (singleStatementDialect) Migrator(*gorm.DB) gorm.Migrator                { return nil }
func (singleStatementDialect) DataTypeOf(*schema.Field) string                { return "" }
func (singleStatementDialect) DefaultValueOf(*schema.Field) clause.Expression { return nil }
func (singleStatementDialect) BindVarTo(w clause.Writer, _ *gorm.Statement, _ interface{}) {
	_ = w.WriteByte('?')
}
func (singleStatementDialect) QuoteTo(w clause.Writer, value string)         { _, _ = w.WriteString(value) }
func (singleStatementDialect) Explain(query string, _ ...interface{}) string { return query }

func singleStatementMapper(native string) datatype.FieldType {
	switch {
	case native == "INT4":
		return datatype.FieldTypeInt
	case native == "INT8":
		return datatype.FieldTypeBigInt
	case strings.HasPrefix(native, "NUMERIC("):
		return datatype.FieldTypeDecimal
	default:
		return datatype.FieldTypeUnknown
	}
}

func TestSingleStatementReadPreservesBindingAndNativeResultsWithoutTransaction(t *testing.T) {
	record := &singleStatementRecord{}
	p := &singleStatementProvider{record: record, boundary: ControlledReadOnlySQLBoundaryValidatedStatement}
	result, err := ExecuteSingleStatementReadOnlySQLWithConnectionPool(t.Context(), p, nil,
		"SELECT small,large,amount,nullable FROM source WHERE small=:value",
		QueryOptions{ReadOnly: true, Parameters: map[string]interface{}{"value": "private bound value"}, Limit: 3}, SQLResultFieldMapper(singleStatementMapper))
	if err != nil {
		t.Fatal(err)
	}
	if record.begins != 0 || record.queries != 1 || record.pools != 1 || len(record.args) != 1 || record.args[0].Value != "private bound value" || strings.Contains(record.query, "private bound value") || !strings.Contains(record.query, "?") {
		t.Fatalf("statement was interpolated, retried or put in a transaction: %+v", record)
	}
	if len(result.Fields) != 4 || result.Fields[2].Precision != 38 || result.Fields[2].Scale != 18 || len(result.Rows) != 1 || result.Rows[0]["large"] != int64(9007199254740993) {
		t.Fatalf("native schema/values changed: %+v", result)
	}
}

func TestSingleStatementReadRejectsInvalidBoundaryAndSideEffectsBeforeConnection(t *testing.T) {
	for _, target := range []struct {
		name, query string
		readOnly    bool
		boundary    ControlledReadOnlySQLBoundary
	}{
		{"write", "DELETE FROM source", true, ControlledReadOnlySQLBoundaryValidatedStatement},
		{"export", "SELECT * FROM source INTO OUTFILE '/tmp/unexpected'", true, ControlledReadOnlySQLBoundaryValidatedStatement},
		{"lock", "SELECT * FROM source FOR UPDATE", true, ControlledReadOnlySQLBoundaryValidatedStatement},
		{"multiple", "SELECT * FROM source; DELETE FROM source", true, ControlledReadOnlySQLBoundaryValidatedStatement},
		{"writable options", "SELECT * FROM source", false, ControlledReadOnlySQLBoundaryValidatedStatement},
		{"database transaction", "SELECT * FROM source", true, ControlledReadOnlySQLBoundaryDatabaseTransaction},
		{"unknown boundary", "SELECT * FROM source", true, ""},
	} {
		t.Run(target.name, func(t *testing.T) {
			record := &singleStatementRecord{}
			p := &singleStatementProvider{record: record, boundary: target.boundary}
			_, err := ExecuteSingleStatementReadOnlySQLWithConnectionPool(t.Context(), p, nil, target.query, QueryOptions{ReadOnly: target.readOnly}, SQLResultFieldMapper(singleStatementMapper))
			if err == nil || record.pools != 0 {
				t.Fatalf("invalid read opened a connection: %v %+v", err, record)
			}
		})
	}
}

func TestSingleStatementReadReturnsQueryFailureWithoutRetry(t *testing.T) {
	record := &singleStatementRecord{failure: context.DeadlineExceeded}
	p := &singleStatementProvider{record: record, boundary: ControlledReadOnlySQLBoundaryValidatedStatement}
	_, err := ExecuteSingleStatementReadOnlySQLWithConnectionPool(t.Context(), p, nil, "SELECT * FROM source WHERE id=?", QueryOptions{ReadOnly: true, Args: []interface{}{2}}, SQLResultFieldMapper(singleStatementMapper))
	if !errors.Is(err, context.DeadlineExceeded) || record.queries != 1 || record.begins != 0 {
		t.Fatalf("query failure retried or changed: %v %+v", err, record)
	}
	if statement, ok := ExecutedSQLForError(err); !ok || statement != record.query {
		t.Fatal("statement error source lost")
	}
}
