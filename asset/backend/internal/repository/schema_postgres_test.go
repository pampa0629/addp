package repository

import (
	"os"
	"testing"
	"time"

	"github.com/addp/asset/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestAssetSchemaMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ASSET_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ASSET_POSTGRES_TEST_DSN to addp_test or an isolated disposable database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP SCHEMA IF EXISTS asset CASCADE; CREATE SCHEMA asset").Error; err != nil {
		t.Fatalf("reset asset schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE asset.authorizations (
		id bigserial PRIMARY KEY, tenant_id bigint NOT NULL, asset_id bigint NOT NULL,
		application_id bigint, user_id bigint NOT NULL, credential varchar(2000),
		expires_at timestamptz, is_active boolean DEFAULT true, revoked_at timestamptz,
		revoked_by bigint, created_at timestamptz, updated_at timestamptz
	); INSERT INTO asset.authorizations (tenant_id, asset_id, user_id, credential) VALUES (7, 9, 91, 'legacy-token')`).Error; err != nil {
		t.Fatalf("seed legacy authorization: %v", err)
	}
	if err := db.Exec(`CREATE TABLE asset.catalogs (
		id bigserial PRIMARY KEY, tenant_id bigint NOT NULL, name varchar(200) NOT NULL,
		parent_id bigint, sort_order bigint DEFAULT 0, description varchar(500),
		created_at timestamptz, updated_at timestamptz
	); CREATE INDEX idx_asset_catalogs_tenant_id ON asset.catalogs (tenant_id);
	CREATE INDEX idx_asset_catalogs_parent_id ON asset.catalogs (parent_id);
	CREATE TABLE asset.assets (
		id bigserial PRIMARY KEY, tenant_id bigint NOT NULL, name varchar(500) NOT NULL,
		description varchar(2000), type_id bigint NOT NULL, catalog_id bigint,
		tags jsonb DEFAULT '[]', status varchar(50) NOT NULL DEFAULT 'draft',
		owner_id bigint NOT NULL, version bigint NOT NULL DEFAULT 1, published_at timestamptz,
		created_by bigint NOT NULL, updated_by bigint, created_at timestamptz, updated_at timestamptz
	); CREATE INDEX idx_asset_assets_catalog_id ON asset.assets (catalog_id);
	INSERT INTO asset.catalogs (tenant_id, name) VALUES (7, 'Education');
	INSERT INTO asset.assets (tenant_id, name, type_id, catalog_id, owner_id, created_by)
	VALUES (7, 'Legacy categorized asset', 1, 1, 11, 11)`).Error; err != nil {
		t.Fatalf("seed legacy asset category storage: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	var legacyColumnCount, authorizationCount, migrationCount int64
	if err := db.Raw(`SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'asset' AND table_name = 'authorizations' AND column_name IN ('credential', 'is_active')`).Scan(&legacyColumnCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Authorization{}).Count(&authorizationCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("asset.data_migrations").Where("version = ?", 2026082701).Count(&migrationCount).Error; err != nil {
		t.Fatal(err)
	}
	if legacyColumnCount != 0 || authorizationCount != 0 || migrationCount != 1 {
		t.Fatalf("legacy_columns=%d authorizations=%d migration=%d", legacyColumnCount, authorizationCount, migrationCount)
	}
	var category models.AssetCategory
	if err := db.Where("tenant_id = ? AND name = ?", 7, "Education").First(&category).Error; err != nil {
		t.Fatalf("renamed AssetCategory was not preserved: %v", err)
	}
	var categorizedAsset models.Asset
	if err := db.Where("tenant_id = ? AND name = ?", 7, "Legacy categorized asset").First(&categorizedAsset).Error; err != nil {
		t.Fatalf("categorized Asset was not preserved: %v", err)
	}
	if categorizedAsset.CategoryID == nil || *categorizedAsset.CategoryID != category.ID || category.Version != 1 {
		t.Fatalf("renamed category storage category=%#v asset=%#v", category, categorizedAsset)
	}
	var legacyCategoryObjectCount, currentCategoryObjectCount int64
	if err := db.Raw(`SELECT count(*) FROM pg_class object
		JOIN pg_namespace namespace ON namespace.oid = object.relnamespace
		WHERE namespace.nspname = 'asset' AND object.relname IN (
		  'catalogs_id_seq', 'catalogs_pkey', 'idx_asset_catalogs_tenant_id',
		  'idx_asset_catalogs_parent_id', 'idx_asset_assets_catalog_id'
		)`).Scan(&legacyCategoryObjectCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`SELECT count(*) FROM pg_class object
		JOIN pg_namespace namespace ON namespace.oid = object.relnamespace
		WHERE namespace.nspname = 'asset' AND object.relname IN (
		  'categories_id_seq', 'categories_pkey', 'idx_asset_categories_tenant_id',
		  'idx_asset_categories_parent_id', 'idx_asset_assets_category_id'
		)`).Scan(&currentCategoryObjectCount).Error; err != nil {
		t.Fatal(err)
	}
	if legacyCategoryObjectCount != 0 || currentCategoryObjectCount != 5 {
		t.Fatalf("legacy category objects=%d current category objects=%d", legacyCategoryObjectCount, currentCategoryObjectCount)
	}

	typeDefinition := models.TypeDefinition{TenantID: 0, Name: "Data application", Code: "application", Enabled: true}
	if err := db.Create(&typeDefinition).Error; err != nil {
		t.Fatal(err)
	}
	asset := models.Asset{TenantID: 7, Name: "Orders app", TypeID: typeDefinition.ID, Status: "published", OwnerID: 11, CreatedBy: 11}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	application := models.Application{TenantID: 7, AssetID: asset.ID, ApplicantID: 91, Status: "approved"}
	if err := db.Create(&application).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	authorization := models.Authorization{
		TenantID: 7, AssetID: asset.ID, ApplicationID: &application.ID, UserID: 91,
		Status: models.AuthorizationStatusPending, NextAttemptAt: &now,
	}
	if err := db.Create(&authorization).Error; err != nil {
		t.Fatalf("create pending authorization: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("idempotent Migrate() error = %v", err)
	}
	if err := db.First(&authorization, authorization.ID).Error; err != nil {
		t.Fatalf("idempotent migration removed current authorization: %v", err)
	}
	invalid := models.Authorization{
		TenantID: 7, AssetID: asset.ID, ApplicationID: pointerInt64(application.ID + 1), UserID: 92,
		Status: models.AuthorizationStatusEffective,
	}
	if err := db.Create(&invalid).Error; err == nil {
		t.Fatal("effective authorization without owner target was accepted")
	}
	fulfilledAt := now
	valid := models.Authorization{
		TenantID: 7, AssetID: asset.ID, ApplicationID: pointerInt64(application.ID + 2), UserID: 93,
		Status: models.AuthorizationStatusEffective, TargetModule: "workbench",
		TargetResourceType: "data_application", TargetResourceID: uuid.NewString(), FulfilledAt: &fulfilledAt,
	}
	if err := db.Create(&valid).Error; err != nil {
		t.Fatalf("valid effective authorization rejected: %v", err)
	}
}

func pointerInt64(value int64) *int64 { return &value }
