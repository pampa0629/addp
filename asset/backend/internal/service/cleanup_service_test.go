package service

import (
	"context"
	"testing"

	"github.com/addp/asset/internal/models"
	"github.com/addp/common/events"
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
			source_module TEXT,
			auth_handler TEXT,
			entry_type TEXT,
			discovery_path TEXT,
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
			source_module TEXT,
			source_reference TEXT,
			fingerprint TEXT,
			source_available BOOLEAN NOT NULL DEFAULT TRUE,
			published_at DATETIME,
			created_by INTEGER NOT NULL,
			updated_by INTEGER,
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
			application_id INTEGER,
			user_id INTEGER NOT NULL,
			credential TEXT,
			expires_at DATETIME,
			is_active BOOLEAN DEFAULT TRUE,
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
	stats, err := svc.ScanGarbage(context.Background(), 1, nil)
	if err != nil {
		t.Fatalf("ScanGarbage: %v", err)
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
	if asset.Status != "offline" || asset.SourceAvailable {
		t.Fatalf("expected asset offline and source unavailable, got status=%s source_available=%v", asset.Status, asset.SourceAvailable)
	}
	var auth models.Authorization
	if err := db.First(&auth, authID).Error; err != nil {
		t.Fatalf("load auth: %v", err)
	}
	if auth.IsActive || auth.RevokedAt == nil {
		t.Fatalf("expected authorization revoked, got active=%v revoked_at=%v", auth.IsActive, auth.RevokedAt)
	}
}

func TestAssetCleanupTenantDeletedPhysicalDeletesOwnedState(t *testing.T) {
	db := setupAssetCleanupTestDB(t)
	seedAssetCleanupTenantState(t, db, 1)
	seedAssetCleanupTenantState(t, db, 2)

	svc := NewCleanupService(db, nil, nil)
	stats, err := svc.ScanGarbage(context.Background(), 1, map[string]interface{}{"tenant_id": 1})
	if err != nil {
		t.Fatalf("ScanGarbage: %v", err)
	}
	if stats.TypeDefinitions != 1 || stats.TypeFieldSchemas != 1 || stats.Catalogs != 1 || stats.Assets != 1 ||
		stats.AssetExtFields != 1 || stats.Applications != 1 || stats.Authorizations != 1 || stats.Ratings != 1 {
		t.Fatalf("unexpected tenant scan stats: %+v", stats)
	}

	stats, err = svc.ExecuteCleanup(context.Background(), 1, events.CleanupModePhysical, map[string]interface{}{"tenant_id": 1})
	if err != nil {
		t.Fatalf("ExecuteCleanup: %v", err)
	}
	if stats.DeletedRecords != 8 {
		t.Fatalf("expected 8 deleted records, got %+v", stats)
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
		TenantID:        tenantID,
		Name:            "Asset",
		TypeID:          typeDef.ID,
		CatalogID:       &catalog.ID,
		Status:          "published",
		OwnerID:         1,
		SourceModule:    "meta",
		SourceReference: "item-1",
		SourceAvailable: true,
		CreatedBy:       1,
	}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatalf("create asset: %v", err)
	}
	ext := models.AssetExtField{AssetID: asset.ID, FieldKey: "path", Value: models.JSONBMap{"value": "table"}}
	if err := db.Create(&ext).Error; err != nil {
		t.Fatalf("create ext field: %v", err)
	}
	app := models.Application{TenantID: tenantID, AssetID: asset.ID, ApplicantID: 2, Reason: "need", Status: "approved"}
	if err := db.Create(&app).Error; err != nil {
		t.Fatalf("create application: %v", err)
	}
	auth := models.Authorization{TenantID: tenantID, AssetID: asset.ID, ApplicationID: &app.ID, UserID: 2, IsActive: true}
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
