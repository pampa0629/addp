package repository_test

import (
	"os"
	"strings"
	"testing"

	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/common/schema"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRestrictedCommonStartupAgainstMySQL(t *testing.T) {
	state := os.Getenv("ADDP_TEST_MYSQL_COMMON_STARTUP")
	if state == "" {
		t.Skip("requires the disposable Common installation gate")
	}
	for _, owner := range []string{"system", "meta"} {
		t.Run(owner, func(t *testing.T) {
			driver, err := platformmysql.Dialector(database.Config{Host: "127.0.0.1", Port: os.Getenv("ADDP_TEST_MYSQL_PORT"), DBName: owner,
				User: os.Getenv("ADDP_DATABASE_RUNTIME_USER"), Password: os.Getenv("ADDP_DATABASE_RUNTIME_PASSWORD")})
			if err != nil {
				t.Fatal(err)
			}
			db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			err = schema.Require(db, "common", schema.CommonVersion)
			switch state {
			case "ready":
				if err != nil {
					t.Fatal(err)
				}
			case "pending":
				if err == nil || !strings.Contains(err.Error(), "unfinished MySQL migration") {
					t.Fatalf("pending revision: %v", err)
				}
			case "missing", "mismatch":
				if err == nil || !strings.Contains(err.Error(), "version mismatch") {
					t.Fatalf("%s revision: %v", state, err)
				}
			default:
				t.Fatalf("unexpected fixture state %q", state)
			}
		})
	}
}
