package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// LoadSpatialExtension 确保 DuckDB spatial 扩展可用。
func LoadSpatialExtension(ctx context.Context, conn *sql.Conn) error {
	if conn == nil {
		return fmt.Errorf("duckdb connection is nil")
	}

	loadErr := execDuckDBStatement(ctx, conn, "LOAD spatial")
	if loadErr == nil {
		return nil
	}

	installErr := execDuckDBStatement(ctx, conn, "INSTALL spatial")
	if installErr != nil && !isDuckDBAlreadyStateError(installErr) {
		return fmt.Errorf("install spatial failed: %w", installErr)
	}

	if err := execDuckDBStatement(ctx, conn, "LOAD spatial"); err != nil && !isDuckDBAlreadyStateError(err) {
		return fmt.Errorf("load spatial failed: %w", err)
	}

	return nil
}

func execDuckDBStatement(ctx context.Context, conn *sql.Conn, stmt string) error {
	_, err := conn.ExecContext(ctx, stmt)
	return err
}

func isDuckDBAlreadyStateError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already installed") ||
		strings.Contains(msg, "already loaded")
}
