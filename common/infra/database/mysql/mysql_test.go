package mysql

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/common/infra/database"
	dialect "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestConnectionConfiguration(t *testing.T) {
	t.Setenv("TZ", "Asia/Tokyo")
	cfg := database.Config{Host: "::1", Port: "3306", User: "platform", Password: "test@p:/credentials", DBName: "fixture", SSLMode: "verify-full", ConnectTimeout: 3 * time.Second}
	value, err := Dialector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	parsed := value.(*dialect.Dialector).DSNConfig
	if parsed.Addr != "[::1]:3306" || parsed.Passwd != cfg.Password || parsed.DBName != cfg.DBName ||
		parsed.TLSConfig != "true" || parsed.Timeout != cfg.ConnectTimeout || parsed.Loc.String() != "Asia/Tokyo" {
		t.Fatal("platform MySQL connection settings changed")
	}
	cfg.Schema = "common"
	if _, err := Dialector(cfg); err == nil {
		t.Fatal("PostgreSQL namespace accepted as a MySQL connection setting")
	}
}

func TestPlatformConnectionAgainstMySQL(t *testing.T) {
	name := os.Getenv("ADDP_TEST_MYSQL_PLATFORM_DATABASE")
	if name == "" {
		t.Skip("run the platform-database MySQL gate")
	}
	t.Setenv("TZ", "Asia/Tokyo")
	driver, err := Dialector(database.Config{
		Host: os.Getenv("ADDP_TEST_MYSQL_HOST"), Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"),
		DBName: name, ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(driver, &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal("failed to open gate-owned MySQL:", err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	db = db.WithContext(t.Context())
	check := func(conn *gorm.DB) (int64, error) {
		var id int64
		var physicalDB, zone, charset string
		var value time.Time
		err := conn.Raw("SELECT CONNECTION_ID(), DATABASE(), @@SESSION.time_zone, @@SESSION.character_set_connection, CAST('2026-01-01 12:00:00' AS DATETIME)").
			Row().Scan(&id, &physicalDB, &zone, &charset, &value)
		if err != nil {
			return 0, err
		}
		if physicalDB != name || zone != "Asia/Tokyo" || charset != "utf8mb4" ||
			value.Format(time.RFC3339) != "2026-01-01T12:00:00+09:00" {
			t.Fatalf("session %d database=%q zone=%q charset=%q datetime=%v", id, physicalDB, zone, charset, value)
		}
		return id, nil
	}
	err = db.Connection(func(conn *gorm.DB) error {
		first, err := check(conn)
		if err != nil {
			return err
		}
		if err := db.Connection(func(other *gorm.DB) error {
			second, err := check(other)
			if err == nil && second == first {
				t.Fatal("expected two physical connections")
			}
			return err
		}); err != nil {
			return err
		}
		if err := conn.Exec("CREATE TEMPORARY TABLE platform_mysql_probe (id INT PRIMARY KEY, label VARCHAR(40)) ENGINE=InnoDB").Error; err != nil {
			return err
		}
		defer func() {
			if err := conn.Session(&gorm.Session{NewDB: true}).Exec("DROP TEMPORARY TABLE platform_mysql_probe").Error; err != nil {
				t.Errorf("drop connection-local fixture: %v", err)
			}
		}()

		rollback := errors.New("rollback fixture")
		err = conn.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("INSERT INTO platform_mysql_probe VALUES (1, ?)", "会话 🧪").Error; err != nil {
				return err
			}
			var label string
			if err := tx.Raw("SELECT label FROM platform_mysql_probe WHERE id = 1").Row().Scan(&label); err != nil {
				return err
			}
			if label != "会话 🧪" {
				t.Fatalf("unexpected Unicode value %q", label)
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatalf("transaction did not return its rollback cause: %v", err)
		}
		if err := conn.Exec("INSERT INTO platform_mysql_probe VALUES (1, 'after rollback')").Error; err != nil {
			return err
		}
		err = conn.Session(&gorm.Session{}).Exec("INSERT INTO platform_mysql_probe VALUES (1, 'conflict')").Error
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			t.Fatalf("duplicate key was not translated: %v", err)
		}
		t.Logf("two physical sessions bound to %s with Asia/Tokyo and utf8mb4, InnoDB rollback and duplicate key translation verified", name)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
