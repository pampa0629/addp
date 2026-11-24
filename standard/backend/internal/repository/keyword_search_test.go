package repository

import (
	"os"
	"testing"

	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"gorm.io/gorm"
)

func TestStandardKeywordSearch(t *testing.T) {
	db := openVersionedTestDB(t)
	if err := db.Exec("PRAGMA case_sensitive_like = ON").Error; err != nil {
		t.Fatal(err)
	}
	checkGlossaryKeywordSearch(t, db)
}

func TestStandardKeywordSearchAgainstMySQL(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_STANDARD_SEARCH") != "1" {
		t.Skip("requires a gate-owned Standard database")
	}
	dialect, err := platformmysql.Dialector(database.Config{
		Host: os.Getenv("ADDP_TEST_MYSQL_HOST"), Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"), DBName: "standard",
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(dialect, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	checkGlossaryKeywordSearch(t, db)
}

// Search is exercised through the repository with case-sensitive database defaults.
func checkGlossaryKeywordSearch(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, stmt := range []string{
		"CREATE TABLE standard.glossaries (id BIGINT PRIMARY KEY, tenant_id BIGINT, scope_type VARCHAR(32), owner_domain_id BIGINT, code VARCHAR(100), lifecycle_state VARCHAR(32), version BIGINT)",
		"CREATE TABLE standard.glossary_revisions (id BIGINT PRIMARY KEY, glossary_id BIGINT, revision_no BIGINT, name VARCHAR(100), status VARCHAR(32), definition VARCHAR(200), effective_from DATETIME, effective_to DATETIME)",
		"INSERT INTO standard.glossaries (id,tenant_id,code,lifecycle_state,version) VALUES (1,7,'one','active',1),(2,8,'two','active',1),(3,7,'three','active',1)",
		"INSERT INTO standard.glossary_revisions VALUES (1,1,1,'one','published','Customer 术语','2020-01-01',NULL),(2,2,1,'two','published','Customer 术语','2020-01-01',NULL),(3,3,1,'three','draft','Customer 术语',NULL,NULL)",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, total, err := NewReferenceResolutionRepository(db).ListGlossaryCandidates(t.Context(), 7, "  cUsToMeR 术语 ", 1, 10)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != 1 {
		t.Fatalf("keyword search: rows=%v total=%d err=%v", rows, total, err)
	}
}
