package repository

import (
	"os"
	"testing"

	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestModelKeywordLists(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{"ATTACH DATABASE ':memory:' AS model", "PRAGMA case_sensitive_like = ON"} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	checkModelKeywordLists(t, db)
}

func TestModelKeywordListsAgainstMySQL(t *testing.T) {
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
	checkModelKeywordLists(t, db)
}

func checkModelKeywordLists(t *testing.T, db *gorm.DB) {
	t.Helper()
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, table := range []string{"model.entities", "model.logical_tables"} {
		if err := db.Exec("CREATE TABLE " + table + " (id BIGINT PRIMARY KEY, tenant_id BIGINT, name TEXT, code TEXT, description TEXT, created_at DATETIME)").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT INTO " + table + " (id, tenant_id, name, code, description) VALUES (1, 7, '订单', 'Order_Region', NULL), (2, 8, 'Order', 'other', NULL), (3, 7, '历史', 'sales', 'Order 明细')").Error; err != nil {
			t.Fatal(err)
		}
	}
	entities, total, err := NewEntityRepository(db).List(7, ListEntityOptions{Keyword: "oRdEr", PageSize: 1})
	if err != nil || total != 2 || len(entities) != 1 || entities[0].ID != 3 {
		t.Fatalf("entities=%v total=%d err=%v", entities, total, err)
	}
	tables, total, err := NewLogicalTableRepository(db).List(7, ListLogicalTableOptions{Keyword: "oRdEr", Page: 2, PageSize: 1})
	if err != nil || total != 2 || len(tables) != 1 || tables[0].ID != 1 {
		t.Fatalf("tables=%v total=%d err=%v", tables, total, err)
	}
}
