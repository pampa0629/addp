package postgresql

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/lib/pq"
)

func TestIntegrationPostgresPrepareTableWritePreservesDecimalDefinition(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	schemaName := "common_pg_it"
	tableName := fmt.Sprintf("prepare_decimal_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS "%s"."%s"`, schemaName, tableName)); err != nil {
			t.Errorf("clean up decimal table: %v", err)
		}
		var remaining bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_tables WHERE schemaname=$1 AND tablename=$2)`, schemaName, tableName).Scan(&remaining); err != nil || remaining {
			t.Errorf("decimal table cleanup not proven: remaining=%v error=%v", remaining, err)
		}
	})
	path := postgresPrepareTablePath(schemaName, tableName)
	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 8, Scale: 2, Nullable: true},
		{Name: "integer_decimal", Type: datatype.FieldTypeDecimal, Precision: 8, Nullable: true},
		{Name: "max_decimal", Type: datatype.FieldTypeDecimal, Precision: 1000, Scale: 1000, Nullable: true},
		{Name: "unbounded", Type: datatype.FieldTypeDecimal, Nullable: true},
	}
	if err := pg.PrepareTableUpsert(ctx, connInfo, path, plugin.TableUpsertOptions{Fields: fields, Keys: []string{"id"}}); err != nil {
		t.Fatal(err)
	}
	fields = append(fields, datatype.FieldInfo{Name: "precise", Type: datatype.FieldTypeDecimal, Precision: 20, Scale: 10, Nullable: true})
	for i := 0; i < 2; i++ {
		if err := pg.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
			t.Fatal(err)
		}
	}
	columns, err := postgresTableColumns(ctx, db, schemaName, tableName)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]int{"amount": {8, 2}, "integer_decimal": {8, 0}, "max_decimal": {1000, 1000}, "precise": {20, 10}, "unbounded": {0, 0}}
	for _, column := range columns {
		expected, ok := want[column.Name]
		if !ok {
			continue
		}
		if expected[0] == 0 {
			if column.NumericPrecision.Valid || column.NumericScale.Valid {
				t.Fatalf("unbounded decimal became constrained: %#v", column)
			}
		} else if !column.NumericPrecision.Valid || !column.NumericScale.Valid || column.NumericPrecision.Int64 != int64(expected[0]) || column.NumericScale.Int64 != int64(expected[1]) {
			t.Fatalf("column %s precision/scale = %v/%v, want %v", column.Name, column.NumericPrecision, column.NumericScale, expected)
		}
		delete(want, column.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing decimal columns: %v", want)
	}
	for _, replacement := range []datatype.FieldInfo{
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 9, Scale: 2},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 8, Scale: 3},
		{Name: "amount", Type: datatype.FieldTypeDecimal},
		{Name: "unbounded", Type: datatype.FieldTypeDecimal, Precision: 8, Scale: 2},
	} {
		// A conflict must abort the entire schema evolution before adding columns.
		request := []datatype.FieldInfo{{Name: "marker", Type: datatype.FieldTypeString, Nullable: true}, replacement}
		if err := pg.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: request}); err == nil {
			t.Fatalf("accepted conflicting decimal definition: %#v", replacement)
		}
		if postgresPrepareColumnExists(t, ctx, db, schemaName, tableName, "marker") {
			t.Fatal("schema changed despite a conflicting decimal definition")
		}
	}
}

func TestIntegrationPostgresPrepareTableWriteRejectsInvalidDecimalBeforeDDL(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	schemaName := fmt.Sprintf("prepare_invalid_decimal_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)); err != nil {
			t.Errorf("clean up decimal schema: %v", err)
		}
		var remaining bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname=$1)`, schemaName).Scan(&remaining); err != nil || remaining {
			t.Errorf("decimal schema cleanup not proven: remaining=%v error=%v", remaining, err)
		}
	})
	for _, definition := range [][2]int{{-1, 0}, {0, 2}, {8, -1}, {8, 9}, {1001, 0}} {
		err := pg.PrepareTableWrite(ctx, connInfo, postgresPrepareTablePath(schemaName, "target"), plugin.TableWriteOptions{
			Fields: []datatype.FieldInfo{{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: definition[0], Scale: definition[1], Nullable: true}},
		})
		if err == nil {
			t.Fatalf("accepted invalid decimal definition %v", definition)
		}
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname=$1)`, schemaName).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("created schema before rejecting decimal definition %v", definition)
		}
	}
}

func TestIntegrationPostgresPrepareTableWriteUsesSchemaOnlyPrivileges(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	roleName := "prepare_writer_" + suffix
	schemaName := "prepare_existing_" + suffix
	missingSchema := "prepare_missing_" + suffix
	password := rand.Text()
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE ROLE "%s" LOGIN PASSWORD '%s'`, roleName, password)); err != nil {
		t.Fatalf("create writer role: %v", err)
	}
	defer func() {
		for _, statement := range []string{
			fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName),
			fmt.Sprintf(`DROP OWNED BY "%s"`, roleName),
			fmt.Sprintf(`DROP ROLE "%s"`, roleName),
		} {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				t.Errorf("clean up owned writer fixture: %v", err)
			}
		}
		var remaining bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname=$1)
			OR EXISTS (SELECT 1 FROM pg_namespace WHERE nspname IN ($2,$3))`, roleName, schemaName, missingSchema).Scan(&remaining); err != nil || remaining {
			t.Errorf("writer fixture cleanup not proven: remaining=%v error=%v", remaining, err)
		}
	}()
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA "%s"; GRANT USAGE, CREATE ON SCHEMA "%s" TO "%s"`, schemaName, schemaName, roleName)); err != nil {
		t.Fatalf("grant existing schema permissions: %v", err)
	}
	writerInfo := make(plugin.ConnectionInfo, len(connInfo))
	for key, value := range connInfo {
		writerInfo[key] = value
	}
	writerInfo["user"], writerInfo["password"] = roleName, password
	dsn, err := pg.BuildDSN(writerInfo)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	var databaseCreate, schemaCreate bool
	if err := writer.QueryRowContext(ctx, `SELECT has_database_privilege(current_database(),'CREATE'), has_schema_privilege($1,'CREATE')`, schemaName).Scan(&databaseCreate, &schemaCreate); err != nil || databaseCreate || !schemaCreate {
		t.Fatalf("writer must have schema-only CREATE: database=%v schema=%v error=%v", databaseCreate, schemaCreate, err)
	}
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true}}
	t.Run("existing schema table creation and evolution", func(t *testing.T) {
		path := postgresPrepareTablePath(schemaName, "target")
		if err := pg.PrepareTableWrite(ctx, writerInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
			t.Fatal(err)
		}
		evolved := append(append([]datatype.FieldInfo{}, fields...), datatype.FieldInfo{Name: "label", Type: datatype.FieldTypeString, Nullable: true})
		if err := pg.PrepareTableWrite(ctx, writerInfo, path, plugin.TableWriteOptions{Fields: evolved}); err != nil {
			t.Fatal(err)
		}
		assertPostgresPrepareColumn(t, ctx, db, schemaName, "target", "label", "text", "YES", "")
	})
	t.Run("existing schema upsert preparation", func(t *testing.T) {
		if err := pg.PrepareTableUpsert(ctx, writerInfo, postgresPrepareTablePath(schemaName, "upsert_target"), plugin.TableUpsertOptions{Fields: fields, Keys: []string{"id"}}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("missing schema requires database CREATE", func(t *testing.T) {
		err := pg.PrepareTableWrite(ctx, writerInfo, postgresPrepareTablePath(missingSchema, "target"), plugin.TableWriteOptions{Fields: fields})
		var permissionError *pq.Error
		if !errors.As(err, &permissionError) || permissionError.Code != "42501" {
			t.Fatalf("missing schema must preserve permission denial: %v", err)
		}
	})
	t.Run("authorized missing schema creation", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`GRANT CREATE ON DATABASE "%s" TO "%s"`, connInfo["database"], roleName)); err != nil {
			t.Fatal(err)
		}
		if err := pg.PrepareTableWrite(ctx, writerInfo, postgresPrepareTablePath(missingSchema, "target"), plugin.TableWriteOptions{Fields: fields}); err != nil {
			t.Fatal(err)
		}
	})
}

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
