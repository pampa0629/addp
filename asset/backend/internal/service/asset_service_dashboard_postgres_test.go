package service

import (
	"os"
	"testing"
	"time"

	"github.com/addp/asset/internal/models"
	"github.com/addp/asset/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDashboardStatsAgainstPostgres(t *testing.T) {
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
	if err := db.Exec("DROP SCHEMA IF EXISTS asset CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.Migrate(db); err != nil {
		t.Fatal(err)
	}

	typeDefinition := models.TypeDefinition{TenantID: 0, Name: "Data application", Code: "application", Enabled: true}
	if err := db.Create(&typeDefinition).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	publishedAt := now.Add(-24 * time.Hour)
	asset := models.Asset{
		TenantID: 7, Name: "Orders app", TypeID: typeDefinition.ID, Status: "published",
		OwnerID: 11, CreatedBy: 11, PublishedAt: &publishedAt,
	}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	application := models.Application{
		TenantID: 7, AssetID: asset.ID, ApplicantID: 91, Status: "approved", CreatedAt: publishedAt,
	}
	if err := db.Create(&application).Error; err != nil {
		t.Fatal(err)
	}
	expiresAt := now.Add(7 * 24 * time.Hour)
	authorization := models.Authorization{
		TenantID: 7, AssetID: asset.ID, ApplicationID: &application.ID, UserID: 91,
		Status: models.AuthorizationStatusEffective, ExpiresAt: &expiresAt,
		TargetModule: "workbench", TargetResourceType: "data_application",
		TargetResourceID: uuid.NewString(), FulfilledAt: &now,
	}
	if err := db.Create(&authorization).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Rating{TenantID: 7, AssetID: asset.ID, UserID: 91, Score: 4}).Error; err != nil {
		t.Fatal(err)
	}

	stats, err := NewAssetService(db, nil, nil).GetDashboardStats(7, DashboardStatsFilter{
		TypeCode: "application",
		AssetID:  asset.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.AssetPublished != 1 || stats.ApplicationApproved != 1 || stats.EffectiveAuthorizedUsers != 1 || stats.RatingAvgScore != 4 {
		t.Fatalf("dashboard stats = %#v", stats)
	}
	if len(stats.PublishTrend) != 1 || stats.PublishTrend[0].Count != 1 || len(stats.ApplicationTrend) != 1 || stats.ApplicationTrend[0].Count != 1 {
		t.Fatalf("dashboard trends = publish %#v application %#v", stats.PublishTrend, stats.ApplicationTrend)
	}
}
