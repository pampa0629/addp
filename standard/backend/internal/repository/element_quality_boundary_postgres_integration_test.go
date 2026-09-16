package repository

import (
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresRemovesElementExtraQualityRules(t *testing.T) {
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
		`CREATE TABLE standard.elements (id BIGINT PRIMARY KEY, version BIGINT, draft_revision_id BIGINT)`,
		`CREATE TABLE standard.element_revisions (id BIGINT PRIMARY KEY, status TEXT, extra_quality_rules JSONB, compiled_quality_rules JSONB, submitted_by BIGINT, submitted_at TIMESTAMPTZ)`,
		`INSERT INTO standard.elements VALUES (1,3,11),(2,5,21)`,
		`INSERT INTO standard.element_revisions VALUES
			(10,'published','{"rules":[{"type":"unique"}]}','{"rules":[{"type":"unique","rule_key":"historical"}]}',1,NOW()),
			(11,'in_review','{"rules":[{"type":"unique"}]}',NULL,1,NOW()),
			(21,'draft','{"rules":[]}',NULL,NULL,NULL)`,
	} {
		if err := tx.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := removeElementExtraQualityRules(tx); err != nil {
			t.Fatal(err)
		}
	}
	var columnCount int64
	if err := tx.Raw(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='standard' AND table_name='element_revisions' AND column_name='extra_quality_rules'`).Scan(&columnCount).Error; err != nil {
		t.Fatal(err)
	}
	if columnCount != 0 {
		t.Fatal("retired column remains")
	}
	var affected, unaffected int64
	tx.Raw(`SELECT version FROM standard.elements WHERE id=1`).Scan(&affected)
	tx.Raw(`SELECT version FROM standard.elements WHERE id=2`).Scan(&unaffected)
	if affected != 4 || unaffected != 5 {
		t.Fatalf("versions = %d, %d", affected, unaffected)
	}
	var state struct {
		Status      string
		SubmittedBy *int64
	}
	if err := tx.Table("standard.element_revisions").Where("id=11").Scan(&state).Error; err != nil {
		t.Fatal(err)
	}
	if state.Status != "draft" || state.SubmittedBy != nil {
		t.Fatalf("review was not invalidated: %#v", state)
	}
	var snapshot string
	if err := tx.Raw(`SELECT compiled_quality_rules->'rules'->0->>'rule_key' FROM standard.element_revisions WHERE id=10 AND status='published'`).Scan(&snapshot).Error; err != nil {
		t.Fatal(err)
	}
	if snapshot != "historical" {
		t.Fatal("published snapshot was rewritten")
	}
}
