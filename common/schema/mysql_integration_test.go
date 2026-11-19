package schema

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"gorm.io/gorm"
)

func TestPlatformMySQLSchemaMigration(t *testing.T) {
	databaseName := os.Getenv("ADDP_TEST_MYSQL_PLATFORM_DATABASE")
	if databaseName == "" {
		t.Skip("run make test-common-mysql-data-protection with --test platform-database")
	}
	if databaseName != "addp_platform_mysql_test" && databaseName != "addp_ci" {
		t.Fatalf("unexpected disposable database %q", databaseName)
	}
	adapter, err := platformmysql.Dialector(database.Config{
		Host: os.Getenv("ADDP_TEST_MYSQL_HOST"), Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"),
		DBName: databaseName, ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(adapter, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	owner := fmt.Sprintf("platform_migration_probe_%d", time.Now().UnixNano())
	firstTable, secondTable := owner+"_one", owner+"_two"
	if err := RequireMySQL(db, owner, 1); err == nil {
		t.Fatal("a Worker accepted a missing schema revision")
	}
	if err := MigrateMySQL(db, owner, 1, func(conn *gorm.DB) error {
		return conn.Exec("CREATE TABLE IF NOT EXISTS " + firstTable + " (id bigint PRIMARY KEY)").Error
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec("DROP TABLE IF EXISTS " + firstTable + ", " + secondTable).Error; err != nil {
			t.Error(err)
		}
		if err := db.Exec("DELETE FROM startup_schema_revision WHERE owner = ?", owner).Error; err != nil {
			t.Error(err)
		}
	})
	if err := RequireMySQL(db, owner, 1); err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("injected interruption after DDL")
	if err := MigrateMySQL(db, owner, 2, func(conn *gorm.DB) error {
		if err := conn.Exec("CREATE TABLE IF NOT EXISTS " + secondTable + " (id bigint PRIMARY KEY)").Error; err != nil {
			return err
		}
		return interrupted
	}); !errors.Is(err, interrupted) {
		t.Fatalf("expected interrupted migration, got %v", err)
	}
	if !db.Migrator().HasTable(secondTable) {
		t.Fatal("interrupted migration did not leave its committed DDL")
	}
	for _, version := range []int64{1, 2} {
		if err := RequireMySQL(db, owner, version); err == nil {
			t.Fatalf("worker version %d accepted an incomplete migration", version)
		}
	}
	if err := MigrateMySQL(db, owner, 3, func(*gorm.DB) error {
		t.Error("a different version tried to migrate an unfinished schema")
		return nil
	}); err == nil {
		t.Fatal("different version accepted an incomplete migration")
	}

	var applied atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	results := make(chan error, 2)
	apply := func(conn *gorm.DB) error {
		if applied.Add(1) == 1 {
			close(entered)
			<-release
		}
		return conn.Exec("CREATE TABLE IF NOT EXISTS " + secondTable + " (id bigint PRIMARY KEY)").Error
	}
	go func() { results <- MigrateMySQL(db, owner, 2, apply) }()
	select {
	case <-entered:
	case <-time.After(8 * time.Second):
		close(release)
		t.Fatal("resumed migration did not enter")
	}
	go func() { results <- MigrateMySQL(db, owner, 2, apply) }()
	if err := RequireMySQL(db, owner, 2); err == nil {
		t.Error("worker accepted the schema during the resumed migration")
	}
	close(release)
	for attempt := 0; attempt < 2; attempt++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(8 * time.Second):
			t.Fatal("concurrent startup migration did not finish")
		}
	}
	if applied.Load() != 1 {
		t.Fatalf("owner migration ran %d times", applied.Load())
	}
	if err := RequireMySQL(db, owner, 2); err != nil {
		t.Fatal(err)
	}
	if err := RequireMySQL(db, owner, 1); err == nil {
		t.Fatal("old worker accepted the migrated schema")
	}
}
