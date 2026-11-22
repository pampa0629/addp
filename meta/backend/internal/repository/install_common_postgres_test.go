package repository

import (
	"os"
	"testing"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

func TestPrepareCommonForIsolatedMetaInstall(t *testing.T) {
	if os.Getenv("ADDP_TEST_META_COMMON_INSTALL") != "1" {
		t.Skip("requires the isolated Meta installation gate")
	}
	db, err := bootstrap.OpenDatabase(database.Config{
		Host: "127.0.0.1", Port: os.Getenv("POSTGRES_PORT"), User: "postgres",
		Password: os.Getenv("POSTGRES_PASSWORD"), DBName: "addp_test", Schema: "common", SSLMode: "disable",
	}, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := schema.InitializeCommon(db); err != nil {
		t.Fatal(err)
	}
}
