//go:build dameng_odbc

package dameng

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/alexbrainman/odbc"
)

func TestDamengOfficialMediaCertification(t *testing.T) {
	if os.Getenv("ADDP_DAMENG_CERTIFICATION") != "1" {
		t.Skip("set ADDP_DAMENG_CERTIFICATION=1 to run the DM8 official-media certification")
	}
	dsn := strings.TrimSpace(os.Getenv("ADDP_TEST_DAMENG_DSN"))
	if dsn == "" {
		t.Fatal("ADDP_TEST_DAMENG_DSN is required")
	}
	expectedBuildID := strings.TrimSpace(os.Getenv("ADDP_DAMENG_EXPECTED_BUILD_ID"))
	if expectedBuildID == "" {
		t.Fatal("ADDP_DAMENG_EXPECTED_BUILD_ID is required")
	}

	db, err := sql.Open("odbc", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("PingContext() error = %v", err)
	}

	var versionBanner string
	var buildID string
	if err := db.QueryRowContext(ctx, `SELECT BANNER FROM V$VERSION WHERE BANNER LIKE 'DM Database Server%'`).Scan(&versionBanner); err != nil {
		t.Fatalf("query V$VERSION error = %v", err)
	}
	if !strings.Contains(versionBanner, "DM Database Server 64 V8") {
		t.Fatalf("V$VERSION banner = %q, want DM Database Server 64 V8", versionBanner)
	}
	if err := db.QueryRowContext(ctx, `SELECT BUILD_VERSION FROM V$INSTANCE`).Scan(&buildID); err != nil {
		t.Fatalf("query V$INSTANCE error = %v", err)
	}
	if buildID != expectedBuildID {
		t.Fatalf("V$INSTANCE BUILD_VERSION = %q, want %q", buildID, expectedBuildID)
	}

	var serverType int
	var expiration string
	if err := db.QueryRowContext(ctx, `SELECT SERVER_TYPE, TO_CHAR(EXPIRED_DATE, 'YYYY-MM-DD') FROM V$LICENSE`).Scan(&serverType, &expiration); err != nil {
		t.Fatalf("query V$LICENSE error = %v", err)
	}
	if serverType != 3 || expiration != "2027-07-07" {
		t.Fatalf("V$LICENSE = type %d expiration %q, want trial type 3 expiring 2027-07-07", serverType, expiration)
	}

	var boundValue string
	if err := db.QueryRowContext(ctx, "SELECT ? FROM DUAL", "dm8-parameter-binding").Scan(&boundValue); err != nil {
		t.Fatalf("parameter binding error = %v", err)
	}
	if boundValue != "dm8-parameter-binding" {
		t.Fatalf("parameter binding value = %q", boundValue)
	}

	const targetTable = "ADDP_DM8_CERTIFICATION_TARGET"
	dropTable := func(cleanupContext context.Context) error {
		var count int
		if err := db.QueryRowContext(cleanupContext, "SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = ?", targetTable).Scan(&count); err != nil {
			return fmt.Errorf("query target table: %w", err)
		}
		if count == 1 {
			if _, err := db.ExecContext(cleanupContext, "DROP TABLE "+targetTable); err != nil {
				return fmt.Errorf("drop target table: %w", err)
			}
		}
		return nil
	}
	if err := dropTable(ctx); err != nil {
		t.Fatalf("drop stale certification table error = %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := dropTable(cleanupContext); err != nil {
			t.Errorf("cleanup certification table error = %v", err)
		}
	})

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE ADDP_DM8_CERTIFICATION_TARGET (
			ID BIGINT PRIMARY KEY,
			TENANT_ID BIGINT NOT NULL,
			EXTERNAL_ID VARCHAR(64) NOT NULL UNIQUE,
			NAME VARCHAR(128) NOT NULL,
			AMOUNT DECIMAL(18, 2) NOT NULL,
			UPDATED_AT TIMESTAMP(6) NOT NULL
		)
	`); err != nil {
		t.Fatalf("create target table error = %v", err)
	}

	baseTime := time.Date(2026, time.September, 13, 8, 0, 0, 123000000, time.Local)
	prepared, err := db.PrepareContext(ctx, `
		INSERT INTO ADDP_DM8_CERTIFICATION_TARGET
			(ID, TENANT_ID, EXTERNAL_ID, NAME, AMOUNT, UPDATED_AT)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		t.Fatalf("PrepareContext() error = %v", err)
	}
	if _, err := prepared.ExecContext(ctx, int64(1), int64(7), "DM8-001", "首次写入", "12.34", baseTime); err != nil {
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
	if _, err := tx.ExecContext(ctx, `INSERT INTO ADDP_DM8_CERTIFICATION_TARGET (ID, TENANT_ID, EXTERNAL_ID, NAME, AMOUNT, UPDATED_AT) VALUES (?, ?, ?, ?, ?, ?)`, int64(99), int64(7), "DM8-ROLLBACK", "回滚", "1.00", baseTime); err != nil {
		_ = tx.Rollback()
		t.Fatalf("transactional insert error = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	var rollbackRows int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ADDP_DM8_CERTIFICATION_TARGET WHERE ID = ?", int64(99)).Scan(&rollbackRows); err != nil {
		t.Fatalf("query rolled-back row error = %v", err)
	}
	if rollbackRows != 0 {
		t.Fatalf("rolled-back row count = %d, want 0", rollbackRows)
	}

	merge := func(name string, amount string, updatedAt time.Time) error {
		_, mergeErr := db.ExecContext(ctx, `
			MERGE INTO ADDP_DM8_CERTIFICATION_TARGET TARGET
			USING (
				SELECT ? AS ID, ? AS TENANT_ID, ? AS EXTERNAL_ID,
				       ? AS NAME, ? AS AMOUNT, ? AS UPDATED_AT
				FROM DUAL
			) SOURCE
			ON (TARGET.ID = SOURCE.ID)
			WHEN MATCHED THEN UPDATE SET
				TARGET.NAME = SOURCE.NAME,
				TARGET.AMOUNT = SOURCE.AMOUNT,
				TARGET.UPDATED_AT = SOURCE.UPDATED_AT
			WHEN NOT MATCHED THEN INSERT
				(ID, TENANT_ID, EXTERNAL_ID, NAME, AMOUNT, UPDATED_AT)
			VALUES
				(SOURCE.ID, SOURCE.TENANT_ID, SOURCE.EXTERNAL_ID, SOURCE.NAME, SOURCE.AMOUNT, SOURCE.UPDATED_AT)
		`, int64(2), int64(7), "DM8-002", name, amount, updatedAt)
		return mergeErr
	}
	watermarkTime := baseTime.Add(time.Microsecond)
	if err := merge("首次 MERGE", "56.78", watermarkTime); err != nil {
		t.Fatalf("initial MERGE error = %v", err)
	}
	if err := merge("幂等 MERGE", "90.12", watermarkTime); err != nil {
		t.Fatalf("idempotent MERGE error = %v", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT ID, NAME
		FROM ADDP_DM8_CERTIFICATION_TARGET
		WHERE UPDATED_AT > ? OR (UPDATED_AT = ? AND ID > ?)
		ORDER BY UPDATED_AT, ID
	`, baseTime, baseTime, int64(1))
	if err != nil {
		t.Fatalf("compound watermark query error = %v", err)
	}
	var watermarkRows []string
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			_ = rows.Close()
			t.Fatalf("scan watermark row error = %v", err)
		}
		watermarkRows = append(watermarkRows, fmt.Sprintf("%d:%s", id, name))
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		t.Fatalf("iterate watermark rows error = %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close watermark rows error = %v", err)
	}
	if got, want := strings.Join(watermarkRows, ","), "2:幂等 MERGE"; got != want {
		t.Fatalf("compound watermark rows = %q, want %q", got, want)
	}

	var mergedRows int
	var mergedAmount string
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), CAST(MAX(AMOUNT) AS VARCHAR(32)) FROM ADDP_DM8_CERTIFICATION_TARGET WHERE ID = ?`, int64(2)).Scan(&mergedRows, &mergedAmount); err != nil {
		t.Fatalf("query merged row error = %v", err)
	}
	if mergedRows != 1 || strings.TrimSpace(mergedAmount) != "90.12" {
		t.Fatalf("merged row = count %d amount %q, want 1 and 90.12", mergedRows, mergedAmount)
	}

	var uniqueColumns int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM USER_CONSTRAINTS CONSTRAINTS
		JOIN USER_CONS_COLUMNS COLUMNS
		  ON COLUMNS.CONSTRAINT_NAME = CONSTRAINTS.CONSTRAINT_NAME
		WHERE CONSTRAINTS.TABLE_NAME = ?
		  AND CONSTRAINTS.CONSTRAINT_TYPE = 'U'
		  AND COLUMNS.COLUMN_NAME = 'EXTERNAL_ID'
	`, targetTable).Scan(&uniqueColumns); err != nil {
		t.Fatalf("unique-key catalog query error = %v", err)
	}
	if uniqueColumns != 1 {
		t.Fatalf("unique-key catalog columns = %d, want 1", uniqueColumns)
	}
}
