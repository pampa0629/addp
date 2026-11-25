package repository

import (
	"os"
	"reflect"
	"testing"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/common/infra/database/dbtypes"
	"gorm.io/gorm"
)

func TestInstalledServiceCatalog(t *testing.T) {
	if os.Getenv("ADDP_TEST_SERVICE_DEPLOYMENT") != "1" {
		t.Skip("requires isolated Service deployment")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{Host: os.Getenv("POSTGRES_HOST"), Port: os.Getenv("POSTGRES_PORT"), DBName: os.Getenv("POSTGRES_DB"), Schema: "service"}, "service", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	checkConsumerCatalog(t, db)
	repo := NewQueryServiceRepository(db)
	item := consumerCatalogService("roundtrip", "Query 服务", 17, "active", true, false)
	item.Keywords = dbtypes.StringArray{"中文", "逗,号", "双\"引号"}
	if err := repo.Create(&item); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetConsumerServiceByID(17, item.ID)
	if err != nil || got.Title != item.Title || !reflect.DeepEqual(item.Keywords, got.Keywords) {
		t.Fatalf("consumer readback: %+v %v", got, err)
	}
	var count int64
	if err := db.Table("service.catalog_resource_changes").Where("tenant_id=? AND source_identity=? AND operation=?", 17, item.ID, "upsert").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("catalog projection count=%d err=%v", count, err)
	}
	t.Logf("service=%d name=%s tenant=%d keywords round-tripped; Catalog upsert=%d", got.ID, got.ServiceName, got.TenantID, count)
}
