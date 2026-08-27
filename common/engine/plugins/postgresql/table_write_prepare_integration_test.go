package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	_ "github.com/lib/pq"
)

func TestIntegrationPostgresPrepareTableWriteEvolvesSafeMissingColumns(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	defer db.Close()

	ctx := context.Background()
	schemaName := "common_pg_it"
	tableName := fmt.Sprintf("prepare_safe_%d", time.Now().UnixNano())
	createPostgresPrepareBaseTable(t, ctx, db, schemaName, tableName, `"id" bigint`)
	defer dropPostgresPrepareTable(db, schemaName, tableName)

	err := pg.PrepareTableWrite(ctx, connInfo, postgresPrepareTablePath(schemaName, tableName), plugin.TableWriteOptions{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt},
			{Name: "name", Type: datatype.FieldTypeString, Nullable: true},
			{Name: "status", Type: datatype.FieldTypeString, Nullable: false, DefaultExpression: "'new'"},
		},
	})
	if err != nil {
		t.Fatalf("PrepareTableWrite failed: %v", err)
	}

	assertPostgresPrepareColumn(t, ctx, db, schemaName, tableName, "name", "text", "YES", "")
	assertPostgresPrepareColumn(t, ctx, db, schemaName, tableName, "status", "text", "NO", "'new'::text")
}

func TestIntegrationPostgresPrepareTableWriteRejectsUnsafeMissingNonNullColumn(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	defer db.Close()

	ctx := context.Background()
	schemaName := "common_pg_it"
	tableName := fmt.Sprintf("prepare_reject_nonnull_%d", time.Now().UnixNano())
	createPostgresPrepareBaseTable(t, ctx, db, schemaName, tableName, `"id" bigint`)
	defer dropPostgresPrepareTable(db, schemaName, tableName)

	err := pg.PrepareTableWrite(ctx, connInfo, postgresPrepareTablePath(schemaName, tableName), plugin.TableWriteOptions{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt},
			{Name: "name", Type: datatype.FieldTypeString, Nullable: false},
		},
	})
	if err == nil {
		t.Fatal("PrepareTableWrite succeeded with unsafe missing non-null column, want error")
	}
	if postgresPrepareColumnExists(t, ctx, db, schemaName, tableName, "name") {
		t.Fatal("unsafe missing column was added")
	}
}

func TestIntegrationPostgresPrepareTableWriteRejectsSpatialFactMismatch(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, true)
	defer db.Close()

	ctx := context.Background()
	schemaName := "common_pg_it"
	tableName := fmt.Sprintf("prepare_spatial_%d", time.Now().UnixNano())
	createPostgresPrepareBaseTable(t, ctx, db, schemaName, tableName, `"id" bigint, "geom" geometry(Point,4326)`)
	defer dropPostgresPrepareTable(db, schemaName, tableName)

	err := pg.PrepareTableWrite(ctx, connInfo, postgresPrepareTablePath(schemaName, tableName), plugin.TableWriteOptions{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt},
			{Name: "geom", Type: datatype.FieldTypeGeometry, Nullable: true},
		},
		SpatialInfo: datatype.NewSingleGeometrySpatialInfo("geom", "Polygon", 4326, 0),
	})
	if err == nil {
		t.Fatal("PrepareTableWrite succeeded with geometry type mismatch, want error")
	}

	err = pg.PrepareTableWrite(ctx, connInfo, postgresPrepareTablePath(schemaName, tableName), plugin.TableWriteOptions{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt},
			{Name: "geom", Type: datatype.FieldTypeGeometry, Nullable: true},
		},
		SpatialInfo: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 3857, 0),
	})
	if err == nil {
		t.Fatal("PrepareTableWrite succeeded with SRID mismatch, want error")
	}
}

func openPostgresPrepareIntegration(t *testing.T, requirePostGIS bool) (*sql.DB, *PostgreSQLPlugin, plugin.ConnectionInfo) {
	t.Helper()
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}

	ctx := context.Background()
	pg := &PostgreSQLPlugin{}
	connInfo := postgresPrepareIntegrationConnInfo()
	connStr, err := pg.BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN failed: %v", err)
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("open postgres failed: %v", err)
	}
	if _, err := db.ExecContext(ctx, "SELECT 1"); err != nil {
		_ = db.Close()
		t.Skipf("PostgreSQL is not available: %v", err)
	}
	if requirePostGIS {
		if _, err := db.ExecContext(ctx, "SELECT postgis_version()"); err != nil {
			_ = db.Close()
			t.Skipf("PostGIS is not available: %v", err)
		}
	}
	return db, pg, connInfo
}

func createPostgresPrepareBaseTable(t *testing.T, ctx context.Context, db *sql.DB, schemaName, tableName, definitions string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS "%s"`, schemaName)); err != nil {
		t.Fatalf("create schema failed: %v", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE "%s"."%s" (%s)`, schemaName, tableName, definitions)); err != nil {
		t.Fatalf("create table failed: %v", err)
	}
}

func assertPostgresPrepareColumn(t *testing.T, ctx context.Context, db *sql.DB, schemaName, tableName, columnName, wantType, wantNullable, wantDefault string) {
	t.Helper()
	var dataType, nullable string
	var columnDefault sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2 AND column_name = $3
	`, schemaName, tableName, columnName).Scan(&dataType, &nullable, &columnDefault)
	if err != nil {
		t.Fatalf("query column %s failed: %v", columnName, err)
	}
	if dataType != wantType || nullable != wantNullable {
		t.Fatalf("column %s = (%q, %q), want (%q, %q)", columnName, dataType, nullable, wantType, wantNullable)
	}
	gotDefault := ""
	if columnDefault.Valid {
		gotDefault = columnDefault.String
	}
	if gotDefault != wantDefault {
		t.Fatalf("column %s default = %q, want %q", columnName, gotDefault, wantDefault)
	}
}

func postgresPrepareColumnExists(t *testing.T, ctx context.Context, db *sql.DB, schemaName, tableName, columnName string) bool {
	t.Helper()
	var exists bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = $2 AND column_name = $3
		)
	`, schemaName, tableName, columnName).Scan(&exists); err != nil {
		t.Fatalf("query column exists failed: %v", err)
	}
	return exists
}

func dropPostgresPrepareTable(db *sql.DB, schemaName, tableName string) {
	_, _ = db.ExecContext(context.Background(), fmt.Sprintf(`DROP TABLE IF EXISTS "%s"."%s"`, schemaName, tableName))
}

func postgresPrepareIntegrationConnInfo() plugin.ConnectionInfo {
	return plugin.ConnectionInfo{
		"host":     postgresPrepareIntegrationEnv("ADDP_TEST_POSTGRES_HOST", "localhost"),
		"port":     postgresPrepareIntegrationEnv("ADDP_TEST_POSTGRES_PORT", "15432"),
		"user":     postgresPrepareIntegrationEnv("ADDP_TEST_POSTGRES_USER", "addp"),
		"password": postgresPrepareIntegrationEnv("ADDP_TEST_POSTGRES_PASSWORD", "addp_password"),
		"database": postgresPrepareIntegrationEnv("ADDP_TEST_POSTGRES_DATABASE", "addp_test"),
		"sslmode":  postgresPrepareIntegrationEnv("ADDP_TEST_POSTGRES_SSLMODE", "disable"),
	}
}

func postgresPrepareIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func postgresPrepareTablePath(schemaName, tableName string) plugin.EngineCatalogPath {
	return plugin.EngineCatalogPath{
		Version: plugin.EngineCatalogPathVersion,
		Segments: []plugin.EngineCatalogSegment{
			{Term: plugin.EngineCatalogTermSchema, Kind: plugin.EngineCatalogKindNamespace, Name: schemaName},
			{Term: plugin.EngineCatalogTermTable, Kind: plugin.EngineCatalogKindTable, Name: tableName},
		},
	}
}
