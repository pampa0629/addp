package opengauss

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
)

func TestOpenGaussOfficialMediaCertification(t *testing.T) {
	if os.Getenv("ADDP_OPENGAUSS_CERTIFICATION") != "1" {
		t.Skip("set ADDP_OPENGAUSS_CERTIFICATION=1 to run the openGauss official-media certification")
	}

	dsn := strings.TrimSpace(os.Getenv("ADDP_TEST_OPENGAUSS_DSN"))
	if dsn == "" {
		t.Fatal("ADDP_TEST_OPENGAUSS_DSN is required")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("PingContext() error = %v", err)
	}

	var version string
	if err := db.QueryRowContext(ctx, "SELECT version()").Scan(&version); err != nil {
		t.Fatalf("query version error = %v", err)
	}
	if !strings.Contains(strings.ToLower(version), "opengauss") {
		t.Fatalf("version() = %q, want openGauss", version)
	}

	var compatibility string
	if err := db.QueryRowContext(ctx, "SHOW sql_compatibility").Scan(&compatibility); err != nil {
		t.Fatalf("query sql_compatibility error = %v", err)
	}
	if strings.ToUpper(strings.TrimSpace(compatibility)) != "PG" {
		t.Fatalf("sql_compatibility = %q, want PG", compatibility)
	}

	const targetTable = "addp_opengauss_certification_target"
	const stagingTable = "addp_opengauss_certification_staging"
	cleanup := func(cleanupContext context.Context) error {
		for _, table := range []string{stagingTable, targetTable} {
			if _, cleanupErr := db.ExecContext(cleanupContext, "DROP TABLE IF EXISTS "+pq.QuoteIdentifier(table)); cleanupErr != nil {
				return fmt.Errorf("drop %s: %w", table, cleanupErr)
			}
		}
		return nil
	}
	if err := cleanup(ctx); err != nil {
		t.Fatalf("drop stale certification tables error = %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := cleanup(cleanupContext); err != nil {
			t.Errorf("cleanup certification tables error = %v", err)
		}
	})

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE addp_opengauss_certification_target (
			id BIGINT PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			external_id VARCHAR(64) NOT NULL,
			name VARCHAR(128) NOT NULL,
			amount NUMERIC(18, 2) NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			UNIQUE (tenant_id, external_id)
		)
	`); err != nil {
		t.Fatalf("create target table error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE addp_opengauss_certification_staging (
			id BIGINT,
			tenant_id BIGINT,
			external_id VARCHAR(64),
			name VARCHAR(128),
			amount NUMERIC(18, 2),
			updated_at TIMESTAMP
		)
	`); err != nil {
		t.Fatalf("create staging table error = %v", err)
	}

	prepared, err := db.PrepareContext(ctx, `
		INSERT INTO addp_opengauss_certification_target
			(id, tenant_id, external_id, name, amount, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`)
	if err != nil {
		t.Fatalf("PrepareContext() error = %v", err)
	}
	baseTime := time.Date(2026, time.September, 8, 10, 11, 12, 123000000, time.UTC)
	if _, err := prepared.ExecContext(ctx, int64(1), int64(7), "OG-001", "首次写入", "12.34", baseTime); err != nil {
		_ = prepared.Close()
		t.Fatalf("parameterized insert error = %v", err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("close prepared statement error = %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	copyStatement, err := tx.PrepareContext(ctx, pq.CopyIn(stagingTable, "id", "tenant_id", "external_id", "name", "amount", "updated_at"))
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("prepare COPY error = %v", err)
	}
	for _, row := range []struct {
		id         int64
		externalID string
		name       string
		amount     string
		updatedAt  time.Time
	}{
		{id: 1, externalID: "OG-001", name: "批量更新", amount: "56.78", updatedAt: baseTime.Add(time.Microsecond)},
		{id: 2, externalID: "OG-002", name: "批量新增", amount: "90.12", updatedAt: baseTime.Add(2 * time.Microsecond)},
	} {
		if _, err := copyStatement.ExecContext(ctx, row.id, int64(7), row.externalID, row.name, row.amount, row.updatedAt); err != nil {
			_ = copyStatement.Close()
			_ = tx.Rollback()
			t.Fatalf("COPY row error = %v", err)
		}
	}
	if _, err := copyStatement.ExecContext(ctx); err != nil {
		_ = copyStatement.Close()
		_ = tx.Rollback()
		t.Fatalf("flush COPY error = %v", err)
	}
	if err := copyStatement.Close(); err != nil {
		_ = tx.Rollback()
		t.Fatalf("close COPY statement error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit COPY error = %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		MERGE INTO addp_opengauss_certification_target t
		USING addp_opengauss_certification_staging s
		ON (t.tenant_id = s.tenant_id AND t.external_id = s.external_id)
		WHEN MATCHED THEN UPDATE SET
			name = s.name,
			amount = s.amount,
			updated_at = s.updated_at
		WHEN NOT MATCHED THEN INSERT
			(id, tenant_id, external_id, name, amount, updated_at)
		VALUES
			(s.id, s.tenant_id, s.external_id, s.name, s.amount, s.updated_at)
	`); err != nil {
		t.Fatalf("MERGE error = %v", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, name, amount::text
		FROM addp_opengauss_certification_target
		WHERE (updated_at, id) > ($1, $2)
		ORDER BY updated_at, id
	`, baseTime, int64(1))
	if err != nil {
		t.Fatalf("tuple watermark query error = %v", err)
	}
	var names []string
	for rows.Next() {
		var id int64
		var name string
		var amount string
		if err := rows.Scan(&id, &name, &amount); err != nil {
			t.Fatalf("scan tuple watermark row error = %v", err)
		}
		names = append(names, name+":"+amount)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tuple watermark rows error = %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close tuple watermark rows error = %v", err)
	}
	if got, want := strings.Join(names, ","), "批量更新:56.78,批量新增:90.12"; got != want {
		t.Fatalf("tuple watermark rows = %q, want %q", got, want)
	}

	var uniqueColumns int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.table_constraints constraints
		JOIN information_schema.key_column_usage columns
		  ON columns.constraint_schema = constraints.constraint_schema
		 AND columns.constraint_name = constraints.constraint_name
		 AND columns.table_schema = constraints.table_schema
		 AND columns.table_name = constraints.table_name
		WHERE constraints.table_schema = current_schema()
		  AND constraints.table_name = $1
		  AND constraints.constraint_type = 'UNIQUE'
		  AND columns.column_name IN ('tenant_id', 'external_id')
	`, targetTable).Scan(&uniqueColumns); err != nil {
		t.Fatalf("unique-key catalog query error = %v", err)
	}
	if uniqueColumns != 2 {
		t.Fatalf("unique-key catalog columns = %d, want 2", uniqueColumns)
	}

	cancelContext, cancelQuery := context.WithTimeout(context.Background(), 200*time.Millisecond)
	startedAt := time.Now()
	_, cancelErr := db.ExecContext(cancelContext, "SELECT pg_sleep(5)")
	cancelQuery()
	if cancelErr == nil {
		t.Fatal("cancelled query error = nil")
	}
	if !errors.Is(cancelContext.Err(), context.DeadlineExceeded) {
		t.Fatalf("cancel context error = %v, want deadline exceeded; query error = %v", cancelContext.Err(), cancelErr)
	}
	if elapsed := time.Since(startedAt); elapsed > 3*time.Second {
		t.Fatalf("cancelled query took %s, want <= 3s", elapsed)
	}
}
