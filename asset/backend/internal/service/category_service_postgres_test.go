package service

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/addp/asset/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCategoryServiceSubtreeAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ASSET_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ASSET_POSTGRES_TEST_DSN to addp_test or an isolated disposable database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS asset CASCADE").Error; err != nil {
			t.Errorf("clean asset test schema: %v", err)
		}
	})
	if err := db.Exec("DROP SCHEMA IF EXISTS asset CASCADE; CREATE SCHEMA asset").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AssetCategory{}); err != nil {
		t.Fatal(err)
	}

	service := NewCategoryService(db)
	root, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Government"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Education", ParentID: &root.ID})
	if err != nil {
		t.Fatal(err)
	}
	grandchild, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Schools", ParentID: &child.ID})
	if err != nil {
		t.Fatal(err)
	}
	otherTenant, err := service.Create(8, &CreateAssetCategoryRequest{Name: "Other tenant"})
	if err != nil {
		t.Fatal(err)
	}

	ids, err := service.SubtreeIDs(7, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{root.ID, child.ID, grandchild.ID}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("SubtreeIDs() = %v, want %v", ids, want)
	}
	if _, err := service.SubtreeIDs(7, otherTenant.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-tenant SubtreeIDs() error = %v", err)
	}
	otherRoot, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Public Services"})
	if err != nil {
		t.Fatal(err)
	}
	moved, err := service.Update(7, child.ID, &UpdateAssetCategoryRequest{
		Version: 1, Name: child.Name, ParentID: &otherRoot.ID, Description: "Moved", SortOrder: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if moved.ParentID == nil || *moved.ParentID != otherRoot.ID || moved.Version != 2 {
		t.Fatalf("moved category = %#v", moved)
	}
	if _, err := service.Update(7, otherRoot.ID, &UpdateAssetCategoryRequest{
		Version: 1, Name: otherRoot.Name, ParentID: &grandchild.ID,
	}); !errors.Is(err, ErrAssetCategoryInvalidParent) {
		t.Fatalf("cycle move error = %v", err)
	}
}
