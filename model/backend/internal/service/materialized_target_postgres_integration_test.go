package service

import (
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
