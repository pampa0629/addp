package service

import (
	"errors"
	"testing"

	"github.com/addp/asset/internal/models"
)

func TestBatchCategoryEnforcesTenantOwnedCategory(t *testing.T) {
	db := openAssetAggregateTestDB(t)
	typeDefinition := models.TypeDefinition{TenantID: 0, Name: "Dataset", Code: "dataset", Enabled: true}
	if err := db.Create(&typeDefinition).Error; err != nil {
		t.Fatal(err)
	}
	asset := models.Asset{TenantID: 7, Name: "Orders", TypeID: typeDefinition.ID, Status: "draft", OwnerID: 11, CreatedBy: 11}
	owned := models.AssetCategory{TenantID: 7, Name: "Education"}
	foreign := models.AssetCategory{TenantID: 8, Name: "Finance"}
	for _, value := range []any{&asset, &owned, &foreign} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}

	service := NewAssetService(db, nil, nil)
	if _, err := service.BatchCategory(7, []int64{asset.ID}, &foreign.ID); !errors.Is(err, ErrInvalidAssetAggregate) {
		t.Fatalf("foreign BatchCategory() error = %v", err)
	}
	if _, err := service.BatchCategory(7, []int64{asset.ID, asset.ID}, &owned.ID); !errors.Is(err, ErrInvalidAssetAggregate) {
		t.Fatalf("duplicate BatchCategory() error = %v", err)
	}
	affected, err := service.BatchCategory(7, []int64{asset.ID}, &owned.ID)
	if err != nil || affected != 1 {
		t.Fatalf("BatchCategory() affected=%d error=%v", affected, err)
	}
	if err := db.First(&asset, asset.ID).Error; err != nil {
		t.Fatal(err)
	}
	if asset.CategoryID == nil || *asset.CategoryID != owned.ID {
		t.Fatalf("categorized asset = %#v", asset)
	}
}
