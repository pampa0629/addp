package service

import (
	"context"
	"testing"

	"github.com/addp/asset/internal/models"
	"github.com/addp/common/events"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAssetCleanupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS asset").Error; err != nil {
		t.Fatalf("attach asset schema: %v", err)
	}
	for _, ddl := range []string{
		`CREATE TABLE asset.type_definitions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			code TEXT NOT NULL,
			icon_url TEXT,
			description TEXT,
			enabled BOOLEAN DEFAULT TRUE,
			sort_order INTEGER DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE asset.type_field_schemas (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type_id INTEGER NOT NULL,
			field_key TEXT NOT NULL,
			field_name TEXT NOT NULL,
			field_type TEXT NOT NULL,
			required BOOLEAN DEFAULT FALSE,
			schema TEXT,
			sort_order INTEGER DEFAULT 0,
			created_at DATETIME
		)`,
		`CREATE TABLE asset.catalogs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			parent_id INTEGER,
			sort_order INTEGER DEFAULT 0,
			description TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE asset.assets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			type_id INTEGER NOT NULL,
			catalog_id INTEGER,
			tags TEXT,
			status TEXT NOT NULL DEFAULT 'draft',
			owner_id INTEGER NOT NULL,
			version INTEGER NOT NULL DEFAULT 1,
			published_at DATETIME,
			created_by INTEGER NOT NULL,
			updated_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE asset.asset_components (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			asset_id INTEGER NOT NULL,
			catalog_entry_id TEXT NOT NULL,
			role TEXT NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE asset.asset_ext_fields (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			asset_id INTEGER NOT NULL,
			field_key TEXT NOT NULL,
			value TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE asset.applications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			asset_id INTEGER NOT NULL,
			applicant_id INTEGER NOT NULL,
			reason TEXT,
			duration_day INTEGER DEFAULT 30,
			status TEXT NOT NULL DEFAULT 'pending',
			reviewer_id INTEGER,
			review_note TEXT,
			reviewed_at DATETIME,
			expires_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE asset.authorizations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			asset_id INTEGER NOT NULL,
			application_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			target_module TEXT NOT NULL DEFAULT '',
			target_resource_type TEXT NOT NULL DEFAULT '',
			target_resource_id TEXT NOT NULL DEFAULT '',
			expires_at DATETIME,
			fulfillment_attempt INTEGER NOT NULL DEFAULT 0,
			fulfillment_last_error TEXT NOT NULL DEFAULT '',
			next_attempt_at DATETIME,
			fulfilled_at DATETIME,
			revoked_at DATETIME,
			revoked_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE asset.ratings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			asset_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			score REAL NOT NULL,
			comment TEXT,
			tags TEXT,
			is_handled BOOLEAN DEFAULT FALSE,
			created_at DATETIME,
			updated_at DATETIME
		)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create cleanup test table: %v", err)
		}
	}
	return db
}

func TestAssetCleanupScanWithoutTenantLifecycleContextReturnsNoCandidates(t *testing.T) {
	db := setupAssetCleanupTestDB(t)
	seedAssetCleanupTenantState(t, db, 1)

	svc := NewCleanupService(db, nil, nil)
	stats, err := svc.ScanReclaimCandidates(context.Background(), 1, nil)
	if err != nil {
		t.Fatalf("ScanReclaimCandidates: %v", err)
	}
	if assetCandidateRecordCount(stats) != 0 {
		t.Fatalf("expected no candidates without tenant lifecycle context, got %+v", stats)
	}
}

func TestAssetCleanupTenantDeletedLogicalOfflinesAssetsAndRevokesAuthorizations(t *testing.T) {
	db := setupAssetCleanupTestDB(t)
	assetID, authID := seedAssetCleanupTenantState(t, db, 1)

	svc := NewCleanupService(db, nil, nil)
	stats, err := svc.ExecuteCleanup(context.Background(), 1, events.CleanupModeLogical, map[string]interface{}{"tenant_id": 1})
	if err != nil {
		t.Fatalf("ExecuteCleanup: %v", err)
	}
	if stats.OfflineAssets != 1 || stats.RevokedAuthorizations != 1 {
		t.Fatalf("unexpected logical cleanup stats: %+v", stats)
	}

	var asset models.Asset
	if err := db.First(&asset, assetID).Error; err != nil {
		t.Fatalf("load asset: %v", err)
	}
	if asset.Status != "offline" {
		t.Fatalf("expected asset offline, got status=%s", asset.Status)
	}
	var auth models.Authorization
	if err := db.First(&auth, authID).Error; err != nil {
		t.Fatalf("load auth: %v", err)
	}
	if auth.Status != models.AuthorizationStatusRevocationPending || auth.NextAttemptAt == nil || auth.RevokedAt != nil {
		t.Fatalf("expected authorization revocation pending, got status=%s next_attempt_at=%v revoked_at=%v", auth.Status, auth.NextAttemptAt, auth.RevokedAt)
	}
}

func TestAssetCleanupTenantDeletedPhysicalDeletesOwnedState(t *testing.T) {
	db := setupAssetCleanupTestDB(t)
	seedAssetCleanupTenantState(t, db, 1)
	seedAssetCleanupTenantState(t, db, 2)

	svc := NewCleanupService(db, nil, nil)
	stats, err := svc.ScanReclaimCandidates(context.Background(), 1, map[string]interface{}{"tenant_id": 1})
	if err != nil {
		t.Fatalf("ScanReclaimCandidates: %v", err)
	}
	if stats.TypeDefinitions != 1 || stats.TypeFieldSchemas != 1 || stats.Catalogs != 1 || stats.Assets != 1 ||
		stats.AssetComponents != 1 || stats.AssetExtFields != 1 || stats.Applications != 1 || stats.Authorizations != 1 || stats.Ratings != 1 {
		t.Fatalf("unexpected tenant scan stats: %+v", stats)
	}

	stats, err = svc.ExecuteCleanup(context.Background(), 1, events.CleanupModePhysical, map[string]interface{}{"tenant_id": 1})
	if err != nil {
		t.Fatalf("ExecuteCleanup: %v", err)
	}
	if stats.DeletedRecords != 9 {
		t.Fatalf("expected 9 deleted records, got %+v", stats)
	}
	assertAssetCleanupCount(t, db, 1, 0)
	assertAssetCleanupCount(t, db, 2, 1)
}

func seedAssetCleanupTenantState(t *testing.T, db *gorm.DB, tenantID int64) (int64, int64) {
	t.Helper()
	typeDef := models.TypeDefinition{TenantID: tenantID, Name: "Dataset", Code: "dataset"}
	if err := db.Create(&typeDef).Error; err != nil {
		t.Fatalf("create type definition: %v", err)
	}
	fieldSchema := models.TypeFieldSchema{TypeID: typeDef.ID, FieldKey: "path", FieldName: "Path", FieldType: "string"}
	if err := db.Create(&fieldSchema).Error; err != nil {
		t.Fatalf("create field schema: %v", err)
	}
	catalog := models.Catalog{TenantID: tenantID, Name: "Catalog"}
	if err := db.Create(&catalog).Error; err != nil {
		t.Fatalf("create catalog: %v", err)
	}
	asset := models.Asset{
		TenantID:  tenantID,
		Name:      "Asset",
		TypeID:    typeDef.ID,
		CatalogID: &catalog.ID,
		Status:    "published",
		OwnerID:   1,
		Version:   1,
		CreatedBy: 1,
	}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatalf("create asset: %v", err)
	}
	component := models.AssetComponent{
		TenantID: tenantID, AssetID: asset.ID, CatalogEntryID: uuid.New(),
		Role: models.AssetComponentRolePrimary, SortOrder: 0,
	}
	if err := db.Create(&component).Error; err != nil {
		t.Fatalf("create asset component: %v", err)
	}
	ext := models.AssetExtField{AssetID: asset.ID, FieldKey: "path", Value: models.JSONBMap{"value": "table"}}
	if err := db.Create(&ext).Error; err != nil {
		t.Fatalf("create ext field: %v", err)
	}
	app := models.Application{TenantID: tenantID, AssetID: asset.ID, ApplicantID: 2, Reason: "need", Status: "approved"}
	if err := db.Create(&app).Error; err != nil {
		t.Fatalf("create application: %v", err)
	}
	auth := models.Authorization{
		TenantID: tenantID, AssetID: asset.ID, ApplicationID: &app.ID, UserID: 2,
		Status: models.AuthorizationStatusEffective, TargetModule: "workbench",
		TargetResourceType: "data_application", TargetResourceID: uuid.NewString(),
	}
	if err := db.Create(&auth).Error; err != nil {
		t.Fatalf("create authorization: %v", err)
	}
	rating := models.Rating{TenantID: tenantID, AssetID: asset.ID, UserID: 2, Score: 5}
	if err := db.Create(&rating).Error; err != nil {
		t.Fatalf("create rating: %v", err)
	}
	return asset.ID, auth.ID
}

func assertAssetCleanupCount(t *testing.T, db *gorm.DB, tenantID int64, expected int64) {
	t.Helper()
	for _, item := range []struct {
		name  string
		model interface{}
		where string
		args  []interface{}
	}{
		{name: "type_definitions", model: &models.TypeDefinition{}, where: "tenant_id = ?", args: []interface{}{tenantID}},
		{name: "catalogs", model: &models.Catalog{}, where: "tenant_id = ?", args: []interface{}{tenantID}},
		{name: "assets", model: &models.Asset{}, where: "tenant_id = ?", args: []interface{}{tenantID}},
		{name: "applications", model: &models.Application{}, where: "tenant_id = ?", args: []interface{}{tenantID}},
		{name: "authorizations", model: &models.Authorization{}, where: "tenant_id = ?", args: []interface{}{tenantID}},
		{name: "ratings", model: &models.Rating{}, where: "tenant_id = ?", args: []interface{}{tenantID}},
	} {
		var count int64
		if err := db.Model(item.model).Where(item.where, item.args...).Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", item.name, err)
		}
		if count != expected {
			t.Fatalf("expected tenant %d %s count %d, got %d", tenantID, item.name, expected, count)
		}
	}
	var typeDefs []models.TypeDefinition
	if err := db.Where("tenant_id = ?", tenantID).Find(&typeDefs).Error; err != nil {
		t.Fatalf("load type definitions: %v", err)
	}
	typeIDs := make([]int64, 0, len(typeDefs))
	for _, item := range typeDefs {
		typeIDs = append(typeIDs, item.ID)
	}
	var typeFieldCount int64
	if len(typeIDs) > 0 {
		if err := db.Model(&models.TypeFieldSchema{}).Where("type_id IN ?", typeIDs).Count(&typeFieldCount).Error; err != nil {
			t.Fatalf("count type field schemas: %v", err)
		}
	}
	if typeFieldCount != expected {
		t.Fatalf("expected tenant %d type_field_schemas count %d, got %d", tenantID, expected, typeFieldCount)
	}
	var assets []models.Asset
	if err := db.Where("tenant_id = ?", tenantID).Find(&assets).Error; err != nil {
		t.Fatalf("load assets: %v", err)
	}
	assetIDs := make([]int64, 0, len(assets))
	for _, item := range assets {
		assetIDs = append(assetIDs, item.ID)
	}
	var extCount int64
	if len(assetIDs) > 0 {
		if err := db.Model(&models.AssetExtField{}).Where("asset_id IN ?", assetIDs).Count(&extCount).Error; err != nil {
			t.Fatalf("count ext fields: %v", err)
		}
	}
	if extCount != expected {
		t.Fatalf("expected tenant %d asset_ext_fields count %d, got %d", tenantID, expected, extCount)
	}
}
