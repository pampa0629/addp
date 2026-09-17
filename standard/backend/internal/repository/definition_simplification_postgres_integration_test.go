package repository

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
)

func TestPostgresSimplifiesStandardDefinitions(t *testing.T) {
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("STANDARD_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`DROP SCHEMA IF EXISTS standard CASCADE`,
		`CREATE SCHEMA standard`,
		`CREATE TABLE standard.elements (id BIGINT PRIMARY KEY, version BIGINT, steward_id BIGINT)`,
		`CREATE TABLE standard.glossaries (steward_id BIGINT)`,
		`CREATE TABLE standard.code_sets (steward_id BIGINT)`,
		`CREATE TABLE standard.metric_definitions (steward_id BIGINT)`,
		`CREATE TABLE standard.documents (steward_id BIGINT)`,
		`CREATE TABLE standard.element_revisions (id BIGINT PRIMARY KEY, element_id BIGINT, data_type TEXT, status TEXT, length INTEGER, format TEXT, compiled_quality_rules JSONB)`,
		`INSERT INTO standard.elements VALUES (1,3,7),(2,5,NULL)`,
		`INSERT INTO standard.element_revisions VALUES (11,1,'text','published',32,'^[A-Z]+$','{"historical":true}'),(12,1,'text','draft',NULL,'',NULL),(21,2,'string','draft',16,'',NULL)`,
	} {
		if err := tx.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := simplifyStandardDefinitions(tx); err != nil {
			t.Fatal(err)
		}
	}
	var columns int64
	if err := tx.Raw(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='standard' AND column_name='steward_id'`).Scan(&columns).Error; err != nil {
		t.Fatal(err)
	}
	if columns != 0 {
		t.Fatal("retired steward columns remain")
	}
	var count int64
	if err := tx.Raw(`SELECT COUNT(*) FROM standard.element_revisions WHERE data_type='string'`).Scan(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("normalized revisions=%d", count)
	}
	var unchanged bool
	if err := tx.Raw(`SELECT status='published' AND length=32 AND format='^[A-Z]+$' AND compiled_quality_rules='{"historical":true}'::jsonb FROM standard.element_revisions WHERE id=11`).Scan(&unchanged).Error; err != nil {
		t.Fatal(err)
	}
	if !unchanged {
		t.Fatal("business constraints or historical snapshot changed")
	}
	var versions []int64
	if err := tx.Raw(`SELECT version FROM standard.elements ORDER BY id`).Scan(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0] != 4 || versions[1] != 5 {
		t.Fatalf("versions=%v", versions)
	}
}
