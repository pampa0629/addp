package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/dolthub/vitess/go/vt/sqlparser"
	mysqldriver "github.com/go-sql-driver/mysql"
)

type mysqlReadBudget struct {
	conn     *sql.Conn
	original uint64
}

func (p *MySQLPlugin) OpenSQLReadSessionBudget(ctx context.Context, conn *sql.Conn) (plugin.SQLReadSessionBudget, error) {
	if _, bounded := ctx.Deadline(); !bounded {
		return nil, nil
	}
	budget := &mysqlReadBudget{conn: conn}
	if err := conn.QueryRowContext(ctx, "SELECT @@session.max_execution_time").Scan(&budget.original); err != nil {
		return nil, err
	}
	if err := budget.BeforeQuery(ctx, nil, ""); err != nil {
		return nil, errors.Join(err, budget.Close())
	}
	return budget, nil
}

func (b *mysqlReadBudget) BeforeQuery(ctx context.Context, tx *sql.Tx, query string) error {
	deadline, _ := ctx.Deadline()
	remaining := time.Until(deadline).Milliseconds()
	if remaining < 1 {
		return context.DeadlineExceeded
	}
	limit := uint64(remaining)
	if b.original > 0 && b.original < limit {
		limit = b.original
	}
	// MySQL's native variable is an unsigned 32-bit millisecond count.
	if limit > 4294967295 {
		limit = 4294967295
	}
	if err := validateMySQLBudgetHints(query, limit); err != nil {
		return err
	}
	statement := fmt.Sprintf("SET SESSION max_execution_time=%d", limit)
	if tx != nil {
		_, err := tx.ExecContext(ctx, statement)
		return err
	}
	_, err := b.conn.ExecContext(ctx, statement)
	return err
}

func (b *mysqlReadBudget) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := b.conn.ExecContext(ctx, fmt.Sprintf("SET SESSION max_execution_time=%d", b.original))
	if errors.Is(err, sql.ErrConnDone) || errors.Is(err, driver.ErrBadConn) {
		return nil // The driver already discarded this physical connection.
	}
	if err != nil {
		_ = b.conn.Raw(func(any) error { return driver.ErrBadConn })
		return fmt.Errorf("restore MySQL read session budget: %w", err)
	}
	return nil
}

func validateMySQLBudgetHints(query string, limit uint64) error {
	tokens := sqlparser.NewStringTokenizer(query)
	for {
		kind, value := tokens.Scan()
		if kind == 0 {
			return nil
		}
		if kind == sqlparser.LEX_ERROR {
			return plugin.NewQueryError(plugin.QueryErrorCodeSyntaxInvalid, errors.New("invalid SQL token"))
		}
		if kind != sqlparser.COMMENT || !strings.HasPrefix(string(value), "/*+") {
			continue
		}
		hints := sqlparser.NewStringTokenizer(string(value[3 : len(value)-2]))
		depth := 0
		setVar := false
		for {
			token, name := hints.Scan()
			if token == 0 {
				break
			}
			if depth == 0 && token != '(' && token != ')' {
				setVar = strings.EqualFold(string(name), "SET_VAR")
			}
			if token == '(' {
				depth++
				continue
			}
			if token == ')' {
				depth--
				continue
			}
			if !strings.EqualFold(string(name), "MAX_EXECUTION_TIME") {
				continue
			}
			if depth > 0 && !setVar {
				continue
			} // An index or table can have this name.
			invalid := plugin.NewQueryError(plugin.QueryErrorCodeExecutionBudgetInvalid,
				errors.New("query hint cannot override the execution budget"))
			if setVar {
				return invalid
			}
			open, _ := hints.Scan()
			_, count := hints.Scan()
			close, _ := hints.Scan()
			milliseconds, err := strconv.ParseUint(string(count), 10, 32)
			if open != '(' || close != ')' || err != nil || milliseconds > limit {
				return invalid
			}
		}
	}
}

func mysqlReadError(err error) error {
	var native *mysqldriver.MySQLError
	if errors.As(err, &native) && native.Number == 3024 {
		return plugin.NewQueryError(plugin.QueryErrorCodeSQLExecutionTimeout, errors.Join(context.DeadlineExceeded, err))
	}
	return err
}
