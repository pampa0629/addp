package service

import (
	"errors"
	"os"
	"testing"

	"github.com/addp/asset/internal/models"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"gorm.io/gorm"
)

func TestInstalledAssetCategories(t *testing.T) {
	if os.Getenv("ADDP_TEST_ASSET_DEPLOYMENT") != "1" {
		t.Skip("requires isolated Asset deployment")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{Host: os.Getenv("POSTGRES_HOST"), Port: os.Getenv("POSTGRES_PORT"), DBName: os.Getenv("POSTGRES_DB"), Schema: "asset"}, "asset", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	types, err := NewTypeService(db).ListTypes(77)
	if err != nil || len(types) != 6 {
		t.Fatalf("installed asset types=%d err=%v", len(types), err)
	}
	svc := NewCategoryService(db)
	root, err := svc.Create(77, &CreateAssetCategoryRequest{Name: "业务"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.Create(77, &CreateAssetCategoryRequest{Name: "订单", ParentID: &root.ID})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := svc.GetTree(77)
	if err != nil || len(tree) != 1 || len(tree[0].Children) != 1 || tree[0].Children[0].ID != child.ID {
		t.Fatalf("category readback: %+v err=%v", tree, err)
	}
	if _, err := svc.Get(78, child.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-tenant read: %v", err)
	}
	if err := db.Create(&models.AssetCategory{TenantID: 77, Name: root.Name}).Error; err == nil {
		t.Fatal("database accepted duplicate root")
	}
	if _, err := svc.Create(78, &CreateAssetCategoryRequest{Name: root.Name}); err != nil {
		t.Fatal(err)
	}
	t.Logf("seed types=%d root=%d child=%d; same-name roots remain tenant scoped", len(types), root.ID, child.ID)
}
