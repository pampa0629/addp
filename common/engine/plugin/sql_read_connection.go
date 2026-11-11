package plugin

import (
	"context"
	"database/sql"
	"errors"
)

// SQLReadSessionBudget belongs to the Provider of the borrowed native connection.
type SQLReadSessionBudget interface {
	BeforeQuery(context.Context, *sql.Tx, string) error
	Close() error
}

type SQLReadSessionBudgetProvider interface {
	OpenSQLReadSessionBudget(context.Context, *sql.Conn) (SQLReadSessionBudget, error)
}

// SQLReadConnection keeps session restoration inside the connection's lifetime.
type SQLReadConnection struct {
	*sql.Conn
	budget SQLReadSessionBudget
}

func AcquireSQLReadConnection(ctx context.Context, db *sql.DB, provider EnginePlugin) (*SQLReadConnection, error) {
	conn, err := AcquireSQLConnection(ctx, db)
	if err != nil {
		return nil, err
	}
	read := &SQLReadConnection{Conn: conn}
	if configured, ok := provider.(SQLReadSessionBudgetProvider); ok {
		read.budget, err = configured.OpenSQLReadSessionBudget(ctx, conn)
		if err != nil {
			_ = conn.Close()
			return nil, SQLExecutionError(err)
		}
	}
	return read, nil
}

func (c *SQLReadConnection) BeforeQuery(ctx context.Context, tx *sql.Tx, query string) error {
	if c.budget == nil {
		return nil
	}
	return c.budget.BeforeQuery(ctx, tx, query)
}

func (c *SQLReadConnection) Close() error {
	var err error
	if c.budget != nil {
		err = c.budget.Close()
		c.budget = nil
	}
	return errors.Join(err, c.Conn.Close())
}
