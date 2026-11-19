package bootstrap

import (
	"database/sql"
	"os"
	"testing"

	"github.com/addp/common/infra/database"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

func TestDatabaseSelection(t *testing.T) {
	for _, name := range []string{"", "postgres", "mysql"} {
		t.Run("provider_"+name, func(t *testing.T) {
			t.Setenv("ADDP_INFRA_DATABASE_PROVIDER", name)
			db, err := OpenDatabase(database.Config{Host: "localhost", Port: "5432"}, &gorm.Config{DisableAutomaticPing: true})
			dsn, nativeErr := PostgreSQLDSN(database.Config{Host: "localhost", Port: "5432"})
			if name == "mysql" {
				if nativeErr == nil || dsn != "" {
					t.Fatal("unimplemented provider accepted for native migration")
				}
				if err == nil || db != nil {
					t.Fatal("unimplemented provider accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if nativeErr != nil {
				t.Fatal(nativeErr)
			}
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			if db.Dialector.Name() != "postgres" {
				t.Fatal("incorrect driver selected")
			}
		})
	}
}

func TestBoundConnectionAgainstPostgres(t *testing.T) {
	name := os.Getenv("ADDP_TEST_POSTGRES_DATABASE")
	if name == "" {
		t.Skip("run make test-common-postgres")
	}
	t.Setenv("TZ", "Asia/Shanghai")
	cfg := database.Config{Host: os.Getenv("ADDP_TEST_POSTGRES_HOST"), Port: os.Getenv("ADDP_TEST_POSTGRES_PORT"),
		User: os.Getenv("ADDP_TEST_POSTGRES_USER"), Password: os.Getenv("ADDP_TEST_POSTGRES_PASSWORD"),
		DBName: name, Schema: "meta", SSLMode: os.Getenv("ADDP_TEST_POSTGRES_SSLMODE")}
	db, err := OpenDatabase(cfg, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var dbName, path, zone string
	if err := db.Raw("SELECT current_database(), current_setting('search_path'), current_setting('TimeZone')").Row().Scan(&dbName, &path, &zone); err != nil {
		t.Fatal(err)
	}
	if dbName != name || path != cfg.Schema || zone != "Asia/Shanghai" {
		t.Fatalf("connection bound to %q, %q, %q", dbName, path, zone)
	}

	// System's existing migration runner consumes Common's DSN through lib/pq.
	// Develop's composite search path must be installed on every connection.
	cfg.Schema = "develop,public"
	dsn, err := PostgreSQLDSN(cfg)
	if err != nil {
		t.Fatal(err)
	}
	connector, err := pq.NewConnector(dsn)
	if err != nil {
		t.Fatal(err)
	}
	native := sql.OpenDB(connector)
	defer native.Close()
	for range 2 {
		conn, err := native.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		err = conn.QueryRowContext(t.Context(), "SELECT current_database(), current_setting('search_path'), current_setting('TimeZone')").Scan(&dbName, &path, &zone)
		if err != nil || dbName != name || path != cfg.Schema || zone != "Asia/Shanghai" {
			t.Fatalf("native connection bound to %q, %q, %q, error=%v", dbName, path, zone, err)
		}
	}
}
