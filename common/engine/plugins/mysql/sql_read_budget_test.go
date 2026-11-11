package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func TestMySQLBudgetHintBoundary(t *testing.T) {
	for _, tc := range []struct {
		sql    string
		reject bool
	}{
		{"SELECT /*+ MAX_EXECUTION_TIME(100) */ 1", false},
		{"SELECT /*+ MAX_EXECUTION_TIME(0) */ 1", false},
		{"SELECT /*+ max_execution_time(101) */ 1", true},
		{"SELECT /*+ MAX_EXECUTION_TIME(4294967296) */ 1", true},
		{"SELECT /*+ SET_VAR(max_execution_time=10000) */ 1", true},
		{"SELECT /*+ SET_VAR( max_execution_time = 0 ) */ 1", true},
		{"SELECT /*+ MAX_EXECUTION_TIME(1) MAX_EXECUTION_TIME(10000) */ 1", true},
		{"SELECT '/*+ MAX_EXECUTION_TIME(10000) */'", false},
		{"SELECT 1 /* MAX_EXECUTION_TIME(10000) */", false},
		{"SELECT /*+ INDEX(t max_execution_time) */ * FROM t", false},
		{"SELECT /*+ SET_VAR(sort_buffer_size=1000) */ 1", false},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			err := validateMySQLBudgetHints(tc.sql, 100)
			if (err != nil) != tc.reject {
				t.Fatalf("hint rejection=%t, error=%v", tc.reject, err)
			}
			if tc.reject && plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeExecutionBudgetInvalid {
				t.Fatalf("hint error lost its controlled code: %v", err)
			}
		})
	}
}

// A failed restoration must retire the physical connection.
// Restoration uses its own bounded context.
func TestMySQLBudgetRestoreFailureDiscardsConnection(t *testing.T) {
	native := &budgetRestoreFailureConn{}
	db := sql.OpenDB(budgetRestoreFailureConnector{conn: native})
	defer db.Close()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	budget := mysqlReadBudget{conn: conn, original: 0}
	if err := budget.Close(); err == nil {
		t.Fatal("restoration failure was hidden")
	}
	if !native.closed || !native.boundedCleanup || db.Stats().OpenConnections != 0 {
		t.Fatalf("failed restoration returned a reusable connection: %+v stats=%+v", native, db.Stats())
	}
	_ = conn.Close()
}

type budgetRestoreFailureConnector struct{ conn *budgetRestoreFailureConn }

func (c budgetRestoreFailureConnector) Connect(context.Context) (driver.Conn, error) {
	return c.conn, nil
}
func (c budgetRestoreFailureConnector) Driver() driver.Driver { return budgetRestoreFailureDriver{} }

type budgetRestoreFailureDriver struct{}

func (budgetRestoreFailureDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("connector required")
}

type budgetRestoreFailureConn struct{ closed, boundedCleanup bool }

func (c *budgetRestoreFailureConn) Close() error { c.closed = true; return nil }
func (*budgetRestoreFailureConn) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (*budgetRestoreFailureConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *budgetRestoreFailureConn) ExecContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	deadline, ok := ctx.Deadline()
	c.boundedCleanup = ok && ctx.Err() == nil && time.Until(deadline) <= 2*time.Second
	return nil, errors.New("restoration rejected")
}
