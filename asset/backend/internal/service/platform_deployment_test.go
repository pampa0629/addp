package service

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/asset/internal/models"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/google/uuid"
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

	t.Run("ApplicationOpenPath", func(t *testing.T) {
		var typ models.TypeDefinition
		if err := db.Where("code = ?", "application").First(&typ).Error; err != nil {
			t.Fatal(err)
		}
		asset := models.Asset{TenantID: 77, TypeID: typ.ID, Name: "数据应用", Status: "published", OwnerID: 11, CreatedBy: 11}
		if err := db.Create(&asset).Error; err != nil {
			t.Fatal(err)
		}
		apps := NewApplicationService(db, NewAuthorizationService(db))
		app, err := apps.Create(77, 12, &CreateApplicationReq{AssetID: asset.ID, Reason: "使用工作台应用"})
		if err != nil {
			t.Fatal(err)
		}
		if err := apps.Approve(77, 11, app.ID, &ApproveApplicationReq{}); err != nil {
			t.Fatal(err)
		}
		check := func(want string) {
			t.Helper()
			params := ApplicationListParams{AssetID: asset.ID}
			rows, total, err := apps.ListForApplicant(77, 12, params)
			if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != app.ID || rows[0].OpenPath != want {
				t.Fatalf("applicant list: rows=%+v total=%d err=%v, want path %q", rows, total, err, want)
			}
			detail, err := apps.Get(77, app.ID)
			if err != nil || detail.OpenPath != want {
				t.Fatalf("application detail: %+v err=%v, want path %q", detail, err, want)
			}
		}
		check("")
		resourceID := uuid.NewString()
		if err := db.Model(&models.Authorization{}).Where("application_id=?", app.ID).Updates(map[string]any{
			"status": models.AuthorizationStatusEffective, "fulfilled_at": time.Now().UTC(),
			"target_module": grantTargetModuleWorkbench, "target_resource_type": grantTargetResourceDataApplication, "target_resource_id": resourceID,
		}).Error; err != nil {
			t.Fatal(err)
		}
		check("/data-apps/" + resourceID)
		ratings := NewRatingService(db)
		t.Run("RatingCreateOnce", func(t *testing.T) {
			first, err := ratings.Create(77, 12, asset.ID, &RatingWriteReq{Score: 4, Tags: []string{"content"}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = ratings.Create(77, 12, asset.ID, &RatingWriteReq{Score: 1, Comment: "second submission"})
			if !errors.Is(err, ErrRatingAlreadyExists) {
				t.Fatalf("duplicate creation: got %v, want already exists", err)
			}
			stored, err := ratings.GetByUser(77, 12, asset.ID)
			if err != nil || stored == nil || stored.ID != first.ID || stored.Score != 4 || len(stored.Tags) != 1 || stored.Tags[0] != "content" || stored.Comment != "" {
				t.Fatalf("first rating after duplicate: %+v, %v", stored, err)
			}
			if _, err := ratings.Update(77, 12, asset.ID, &RatingWriteReq{Score: 5}); err != nil {
				t.Fatal(err)
			}
		})
		other, total, err := apps.ListForApplicant(77, 13, ApplicationListParams{AssetID: asset.ID})
		if err != nil || total != 0 || len(other) != 0 {
			t.Fatalf("other applicant: rows=%+v total=%d err=%v", other, total, err)
		}
		if err := apps.RevokeByApplication(77, 11, app.ID); err != nil {
			t.Fatal(err)
		}
		check("")
		if _, err := ratings.Update(77, 12, asset.ID, &RatingWriteReq{Score: 2}); !errors.Is(err, ErrRatingAccessDenied) {
			t.Fatalf("rating after authorization revocation: got %v", err)
		}
	})
}
