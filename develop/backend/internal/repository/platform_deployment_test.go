package repository

import (
	"os"
	"testing"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
)

func TestInstalledDevelopTask(t *testing.T) {
	if os.Getenv("ADDP_TEST_DEVELOP_DEPLOYMENT") != "1" {
		t.Skip("requires isolated Develop deployment")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{Host: os.Getenv("POSTGRES_HOST"), Port: os.Getenv("POSTGRES_PORT"), DBName: os.Getenv("POSTGRES_DB"), Schema: "develop,public"}, "develop", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := NewDevTaskRepository(db)
	task := models.DevTask{TenantID: 7, Name: "OrderReport", DisplayName: "订单", DevType: "query", Content: models.DevTaskContent{"query_type": "sql"}, Status: "active", Tags: []string{"统计", "季度,报告"}}
	if err := repo.Create(&task); err != nil {
		t.Fatal(err)
	}
	filter := &models.ListDevTasksRequest{Keyword: "orderreport", Tag: "季度,报告", Page: 1, PageSize: 10}
	rows, total, err := repo.List(filter, 7)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != task.ID || len(rows[0].Tags) != 2 {
		t.Fatalf("task list: %v %d %v", rows, total, err)
	}
	_, total, err = repo.List(filter, 8)
	if err != nil || total != 0 {
		t.Fatalf("other tenant count: %d %v", total, err)
	}
	var event models.CatalogResourceChangeRow
	if err := db.Where("tenant_id=? AND source_identity=?", 7, task.ID).First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Operation != "upsert" {
		t.Fatalf("catalog operation: %s", event.Operation)
	}
	t.Logf("task %d: tags and query definition read in tenant; catalog upsert observed", task.ID)
}
