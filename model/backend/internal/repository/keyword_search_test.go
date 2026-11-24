package repository

import (
	"os"
	"testing"

	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestModelRepositoryReads(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{"ATTACH DATABASE ':memory:' AS model", "PRAGMA case_sensitive_like = ON"} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	checkModelRepositoryReads(t, db)
}

func TestModelRepositoryReadsAgainstMySQL(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_MODEL_REPOSITORY") != "1" {
		t.Skip("requires a gate-owned Model database")
	}
	driver, err := platformmysql.Dialector(database.Config{
		Host: os.Getenv("ADDP_TEST_MYSQL_HOST"), Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"), DBName: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	checkModelRepositoryReads(t, db)
}

func checkModelRepositoryReads(t *testing.T, db *gorm.DB) {
	t.Helper()
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, table := range []string{"model.entities", "model.logical_tables"} {
		if err := db.Exec("CREATE TABLE " + table + " (id BIGINT PRIMARY KEY, tenant_id BIGINT, name TEXT, code TEXT, description TEXT, domain_id BIGINT, status VARCHAR(16), created_at DATETIME)").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT INTO " + table + " (id, tenant_id, name, code, description, domain_id, status) VALUES (1, 7, '订单', 'Order_Region', NULL, 42, 'approved'), (2, 8, 'Order', 'other', NULL, 42, 'draft'), (3, 7, '历史', 'sales', 'Order 明细', 43, 'draft')").Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Run("keyword lists", func(t *testing.T) { checkModelKeywordLists(t, db) })
	t.Run("domain standard references", func(t *testing.T) { checkDomainReferenceReads(t, db) })
}

func checkModelKeywordLists(t *testing.T, db *gorm.DB) {
	t.Helper()
	entities, total, err := NewEntityRepository(db).List(7, ListEntityOptions{Keyword: "oRdEr", PageSize: 1})
	if err != nil || total != 2 || len(entities) != 1 || entities[0].ID != 3 {
		t.Fatalf("entities=%v total=%d err=%v", entities, total, err)
	}
	tables, total, err := NewLogicalTableRepository(db).List(7, ListLogicalTableOptions{Keyword: "oRdEr", Page: 2, PageSize: 1})
	if err != nil || total != 2 || len(tables) != 1 || tables[0].ID != 1 {
		t.Fatalf("tables=%v total=%d err=%v", tables, total, err)
	}
}

func checkDomainReferenceReads(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, stmt := range []string{
		"CREATE TABLE model.entity_attributes (entity_id BIGINT, element_id BIGINT, element_revision_id BIGINT, name VARCHAR(64))",
		"CREATE TABLE model.logical_fields (table_id BIGINT, element_id BIGINT, element_revision_id BIGINT, column_name VARCHAR(64))",
		"CREATE TABLE model.metric_implementations (id BIGINT PRIMARY KEY, tenant_id BIGINT, fact_table_id BIGINT, metric_definition_id BIGINT, name VARCHAR(64))",
		"CREATE TABLE model.metric_implementation_revisions (id BIGINT PRIMARY KEY, implementation_id BIGINT, tenant_id BIGINT, revision_no BIGINT, status VARCHAR(16), metric_definition_revision_id BIGINT)",
		"INSERT INTO model.entity_attributes VALUES (1,9007199254740993,9007199254740995,'field'),(2,100,101,'foreign'),(3,102,103,'other domain')",
		"INSERT INTO model.logical_fields VALUES (1,9007199254740993,NULL,'field'),(2,100,101,'foreign'),(3,102,103,'other domain')",
		"INSERT INTO model.metric_implementations VALUES (11,7,1,200,'with revisions'),(12,7,1,201,'without revisions'),(13,8,2,202,'foreign')",
		"INSERT INTO model.metric_implementation_revisions VALUES (21,11,7,1,'published',301),(22,11,7,2,'published',302),(23,11,7,3,'draft',303),(24,11,8,4,'published',304)",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := NewEntityRepository(db)
	for _, tc := range []struct{ kind, revision string }{{"entity", "9007199254740995"}, {"logical_table", ""}} {
		page, err := repo.ListDomainStandardReferences(t.Context(), 7, 42, tc.kind, 1, 10)
		if err != nil || page.Total != 1 || len(page.Data) != 1 || page.Data[0].SourceIdentity != "9007199254740993" ||
			len(page.Data[0].Evidence) != 1 || page.Data[0].Evidence[0].RevisionID != tc.revision {
			t.Fatalf("%s page=%+v error=%v", tc.kind, page, err)
		}
	}
	page, err := repo.ListDomainStandardReferences(t.Context(), 7, 42, "metric_implementation", 1, 10)
	if err != nil || page.Total != 2 || len(page.Data) != 2 || len(page.Data[0].Evidence) != 1 || len(page.Data[1].Evidence) != 1 {
		t.Fatalf("metric page=%+v error=%v", page, err)
	}
	published, draft := page.Data[0].Evidence[0], page.Data[1].Evidence[0]
	if page.Data[0].SourceIdentity != "200" || published.RevisionID != "302" || published.ResourceRevisionID != "22" || published.State != "published" ||
		page.Data[1].SourceIdentity != "201" || draft.RevisionID != "" || draft.ResourceRevisionID != "" || draft.State != "draft" {
		t.Fatalf("metric revision selection: %+v", page)
	}
}
