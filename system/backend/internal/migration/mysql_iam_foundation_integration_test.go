package migration

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/common/schema"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMySQLIAMPrincipalUserFoundation(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_SYSTEM_IAM_FOUNDATION") != "1" {
		t.Skip("run the disposable platform-system-iam-foundation MySQL gate")
	}
	dialector, err := platformmysql.Dialector(database.Config{
		Host: "127.0.0.1", Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"),
		DBName: "system", ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(dialector, &gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })

	// Exercise a cold install interrupted after the first committed DDL.
	interrupted := errors.New("fixture interrupt after principal DDL")
	err = schema.MigrateMySQL(db, mysqlIAMFoundationOwner, mysqlIAMFoundationVersion, func(conn *gorm.DB) error {
		ddl, err := mysqlIAMFoundationSQL.ReadFile("mysql_iam/001_principals.sql")
		if err != nil {
			return err
		}
		if err := conn.Exec(string(ddl)).Error; err != nil {
			return err
		}
		return interrupted
	})
	if !errors.Is(err, interrupted) {
		t.Fatalf("initial migration did not reach the interruption: %v", err)
	}
	if err := schema.RequireMySQL(db, mysqlIAMFoundationOwner, mysqlIAMFoundationVersion); err == nil {
		t.Fatal("partial schema passed the revision check")
	}
	if err := MigrateMySQLIAMFoundation(db); err != nil {
		t.Fatal(err)
	}
	if err := schema.RequireMySQL(db, mysqlIAMFoundationOwner, mysqlIAMFoundationVersion); err != nil {
		t.Fatal(err)
	}

	mustExec := func(query string, args ...any) {
		t.Helper()
		if err := db.Exec(query, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	mustExec("INSERT INTO principals (id, principal_type) VALUES (100, 'user'), (101, 'service_principal')")
	mustExec("INSERT INTO users (id, display_name) VALUES (100, ?)", "验证用户 🧪")
	for _, invalid := range []struct{ name, query string }{
		{"missing principal", "INSERT INTO users (id, display_name) VALUES (999, 'User')"},
		{"different principal subtype", "INSERT INTO users (id, display_name) VALUES (101, 'User')"},
		{"existing subtype changes type", "UPDATE principals SET principal_type='service_principal' WHERE id=100"},
		{"inactive principal without time", "UPDATE principals SET status='deactivated' WHERE id=100"},
		{"nonpositive version", "UPDATE principals SET authorization_version=0 WHERE id=100"},
		{"empty display name", "UPDATE users SET display_name=' ' WHERE id=100"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			if err := db.Exec(invalid.query).Error; err == nil {
				t.Fatal("schema accepted the invalid IAM fact")
			}
		})
	}

	mustExec("UPDATE principals SET authorization_version=authorization_version+1, updated_at='2000-01-01' WHERE id=100")
	mustExec("UPDATE users SET updated_at='2000-01-01' WHERE id=100")
	if err := MigrateMySQLIAMFoundation(db); err != nil {
		t.Fatal(err)
	}
	var display string
	var version int64
	var principalUpdated, userUpdated time.Time
	err = db.Raw(`SELECT u.display_name, p.authorization_version, p.updated_at, u.updated_at
		FROM principals p JOIN users u ON u.id=p.id WHERE p.id=100`).Row().Scan(&display, &version, &principalUpdated, &userUpdated)
	if err != nil || display != "验证用户 🧪" || version != 2 || principalUpdated.Year() <= 2000 || userUpdated.Year() <= 2000 {
		t.Fatalf("restarted schema returned wrong persisted data or timestamps: %q %d %v %v %v", display, version, principalUpdated, userUpdated, err)
	}

	rollback := errors.New("fixture rollback after identity writes")
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO principals (id, principal_type) VALUES (102, 'user')").Error; err != nil {
			return err
		}
		if err := tx.Exec("INSERT INTO users (id, display_name) VALUES (102, 'Rollback')").Error; err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("identity transaction did not reach the rollback: %v", err)
	}
	var leftovers int
	if err := db.Raw("SELECT COUNT(*) FROM principals WHERE id=102").Scan(&leftovers).Error; err != nil || leftovers != 0 {
		t.Fatalf("identity rollback left a principal: %d, %v", leftovers, err)
	}
}
