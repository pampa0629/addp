package service

import (
	"database/sql"
	"github.com/addp/model/internal/models"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresMaterializedTargetDecommissionIsOwnedExactAndIdempotent(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_MODEL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_MODEL_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schemaName := "model_retire_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if err := db.Exec("CREATE SCHEMA " + quoteIdentifier(schemaName)).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA " + quoteIdentifier(schemaName) + " CASCADE").Error })

	ownedMarker := materializationMarker(7, strings.Repeat("a", 64), uuid.NewString())
	foreignMarker := materializationMarker(8, strings.Repeat("b", 64), uuid.NewString())
	for _, statement := range []string{
		"CREATE TABLE " + qualifiedIdentifier(schemaName, "owned") + " (value BIGINT)",
		"COMMENT ON TABLE " + qualifiedIdentifier(schemaName, "owned") + " IS " + quoteSQLLiteral(ownedMarker),
		"CREATE TABLE " + qualifiedIdentifier(schemaName, "foreign_owned") + " (value BIGINT)",
		"COMMENT ON TABLE " + qualifiedIdentifier(schemaName, "foreign_owned") + " IS " + quoteSQLLiteral(foreignMarker),
		"CREATE TABLE " + qualifiedIdentifier(schemaName, "unmarked") + " (value BIGINT)",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("prepare table: %v", err)
		}
	}

	if err := dropOwnedMaterializedTarget(db, schemaName, "owned", 7); err != nil {
		t.Fatalf("drop owned target: %v", err)
	}
	assertMaterializationTableExists(t, db, schemaName, "owned", false)
	if err := dropOwnedMaterializedTarget(db, schemaName, "owned", 7); err != nil {
		t.Fatalf("idempotent missing target: %v", err)
	}
	for _, tableName := range []string{"foreign_owned", "unmarked"} {
		if err := dropOwnedMaterializedTarget(db, schemaName, tableName, 7); err == nil {
			t.Fatalf("drop accepted %s", tableName)
		}
		assertMaterializationTableExists(t, db, schemaName, tableName, true)
	}
}

func assertMaterializationTableExists(t *testing.T, db *gorm.DB, schemaName, tableName string, expected bool) {
	t.Helper()
	var relation sql.NullString
	if err := db.Raw("SELECT to_regclass(?)::text", schemaName+"."+tableName).Scan(&relation).Error; err != nil {
		t.Fatalf("inspect %s: %v", tableName, err)
	}
	if relation.Valid != expected {
		t.Fatalf("%s existence = %v, want %v", tableName, relation.Valid, expected)
	}
}

func TestPostgresMaterializedTargetCreationPreservesRowsAndRejectsDrift(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_MODEL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_MODEL_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := "model_create_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if err := db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DROP SCHEMA " + schema + " CASCADE"); pool, _ := db.DB(); pool.Close() })
	table := &models.LogicalTable{ID: 77, Code: "target", Materialization: models.JSONB{"target_parent_locator": "addp://engine/1/path/" + schema + "?type=schema", "target_name": "target"}}
	fields := []models.LogicalField{
		{ColumnName: "id", DataType: "int", IsPK: true, Nullable: false},
		{ColumnName: "amount", DataType: "decimal", Nullable: false},
	}
	fingerprint, err := materializationSchemaFingerprint(table, fields)
	if err != nil {
		t.Fatal(err)
	}
	svc := &MaterializationService{logicalTableSvc: &LogicalTableService{}}
	create := func() error {
		return db.Transaction(func(tx *gorm.DB) error {
			return svc.ensureMaterializedTable(tx, table, fields, schema, "target", fingerprint, uuid.NewString())
		})
	}
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { done <- create() }()
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	var amountType string
	if err := db.Raw(`SELECT pg_catalog.format_type(a.atttypid, a.atttypmod)
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = ? AND c.relname = 'target' AND a.attname = 'amount'`, schema).Scan(&amountType).Error; err != nil {
		t.Fatal(err)
	}
	if amountType != "numeric(38,18)" {
		t.Fatalf("decimal physical type = %q", amountType)
	}
	if err := db.Exec("INSERT INTO " + qualifiedIdentifier(schema, "target") + " VALUES (42, 123.456)").Error; err != nil {
		t.Fatal(err)
	}
	if err := create(); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Table(qualifiedIdentifier(schema, "target")).Count(&count)
	if count != 1 {
		t.Fatalf("rows lost: %d", count)
	}
	db.Exec("ALTER TABLE " + qualifiedIdentifier(schema, "target") + " ADD COLUMN extra text")
	if err := create(); err == nil {
		t.Fatal("untracked structural drift accepted")
	}
	db.Exec("ALTER TABLE " + qualifiedIdentifier(schema, "target") + " DROP COLUMN extra")
	if err := db.Exec("ALTER TABLE " + qualifiedIdentifier(schema, "target") + " ALTER COLUMN amount TYPE numeric").Error; err != nil {
		t.Fatal(err)
	}
	if err := create(); err == nil {
		t.Fatal("decimal precision drift accepted")
	}
	if err := db.Exec("ALTER TABLE " + qualifiedIdentifier(schema, "target") + " ALTER COLUMN amount TYPE numeric(38,18)").Error; err != nil {
		t.Fatal(err)
	}
	table.ID = 78
	if err := create(); err == nil {
		t.Fatal("foreign ownership accepted")
	}
}
