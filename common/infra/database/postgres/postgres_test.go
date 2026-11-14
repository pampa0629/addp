package postgres

import (
	"testing"

	"github.com/addp/common/infra/database"
	"github.com/jackc/pgx/v5"
	driver "gorm.io/driver/postgres"
)

func TestPlatformConnectionParameters(t *testing.T) {
	t.Setenv("TZ", "Asia/Shanghai")
	cfg := database.Config{Host: "localhost", Port: "6543", User: "user name", Password: `quote' and \slash`, DBName: "platform db", Schema: "meta"}
	dialector, err := Dialector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := pgx.ParseConfig(dialector.(*driver.Dialector).DSN)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != cfg.Host || parsed.Port != 6543 || parsed.User != cfg.User || parsed.Password != cfg.Password || parsed.Database != cfg.DBName || parsed.TLSConfig != nil || parsed.RuntimeParams["search_path"] != "meta" || parsed.RuntimeParams["timezone"] != "Asia/Shanghai" {
		t.Fatal("PG parsed different platform connection parameters")
	}
}
