package service

import (
	"errors"
	"testing"
	"time"

	"github.com/addp/asset/internal/models"
	"gorm.io/gorm"
)

func TestDashboardStatsScopesAssetOwnedOperationalFacts(t *testing.T) {
	db := openAssetAggregateTestDB(t)
	for _, statement := range []string{
		`CREATE TABLE asset.applications (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, asset_id INTEGER NOT NULL,
			applicant_id INTEGER NOT NULL, reason TEXT, duration_day INTEGER, status TEXT,
			reviewer_id INTEGER, review_note TEXT, reviewed_at DATETIME, expires_at DATETIME,
			created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE asset.authorizations (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, asset_id INTEGER NOT NULL,
			application_id INTEGER, user_id INTEGER NOT NULL, status TEXT, target_module TEXT,
			target_resource_type TEXT, target_resource_id TEXT, expires_at DATETIME,
			fulfillment_attempt INTEGER, fulfillment_last_error TEXT, next_attempt_at DATETIME,
			fulfilled_at DATETIME, revoked_at DATETIME, revoked_by INTEGER,
			created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE asset.ratings (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, asset_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL, score REAL NOT NULL, comment TEXT, tags TEXT, is_handled BOOLEAN,
			created_at DATETIME, updated_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}

	applicationType := models.TypeDefinition{TenantID: 0, Name: "Data application", Code: "application", Enabled: true}
	datasetType := models.TypeDefinition{TenantID: 0, Name: "Dataset", Code: "dataset", Enabled: true}
	for _, value := range []any{&applicationType, &datasetType} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	yesterday := now.Add(-24 * time.Hour)
	applicationPublished := models.Asset{TenantID: 7, Name: "Orders app", TypeID: applicationType.ID, Status: "published", OwnerID: 11, CreatedBy: 11, PublishedAt: &yesterday}
	applicationDraft := models.Asset{TenantID: 7, Name: "Draft app", TypeID: applicationType.ID, Status: "draft", OwnerID: 11, CreatedBy: 11}
	dataset := models.Asset{TenantID: 7, Name: "Orders dataset", TypeID: datasetType.ID, Status: "published", OwnerID: 11, CreatedBy: 11, PublishedAt: &yesterday}
	foreignApplication := models.Asset{TenantID: 8, Name: "Foreign app", TypeID: applicationType.ID, Status: "published", OwnerID: 21, CreatedBy: 21, PublishedAt: &yesterday}
	for _, value := range []any{&applicationPublished, &applicationDraft, &dataset, &foreignApplication} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}

	applications := []*models.Application{
		{TenantID: 7, AssetID: applicationPublished.ID, ApplicantID: 101, Status: "pending", CreatedAt: yesterday},
		{TenantID: 7, AssetID: applicationPublished.ID, ApplicantID: 102, Status: "approved", CreatedAt: yesterday},
		{TenantID: 7, AssetID: applicationPublished.ID, ApplicantID: 103, Status: "rejected", CreatedAt: yesterday},
		{TenantID: 7, AssetID: applicationDraft.ID, ApplicantID: 101, Status: "approved", CreatedAt: yesterday},
		{TenantID: 7, AssetID: dataset.ID, ApplicantID: 201, Status: "pending", CreatedAt: yesterday},
		{TenantID: 8, AssetID: foreignApplication.ID, ApplicantID: 301, Status: "approved", CreatedAt: yesterday},
	}
	for _, application := range applications {
		if err := db.Create(application).Error; err != nil {
			t.Fatal(err)
		}
	}

	nextWeek := now.Add(7 * 24 * time.Hour)
	lastWeek := now.Add(-7 * 24 * time.Hour)
	authorizations := []*models.Authorization{
		{TenantID: 7, AssetID: applicationPublished.ID, ApplicationID: &applications[1].ID, UserID: 501, Status: models.AuthorizationStatusEffective, ExpiresAt: &nextWeek},
		{TenantID: 7, AssetID: applicationDraft.ID, ApplicationID: &applications[3].ID, UserID: 501, Status: models.AuthorizationStatusEffective, ExpiresAt: &nextWeek},
		{TenantID: 7, AssetID: applicationPublished.ID, ApplicationID: &applications[0].ID, UserID: 502, Status: models.AuthorizationStatusEffective, ExpiresAt: &lastWeek},
		{TenantID: 8, AssetID: foreignApplication.ID, ApplicationID: &applications[5].ID, UserID: 701, Status: models.AuthorizationStatusEffective, ExpiresAt: &nextWeek},
	}
	for _, authorization := range authorizations {
		if err := db.Create(authorization).Error; err != nil {
			t.Fatal(err)
		}
	}

	ratings := []*models.Rating{
		{TenantID: 7, AssetID: applicationPublished.ID, UserID: 801, Score: 4},
		{TenantID: 7, AssetID: applicationPublished.ID, UserID: 802, Score: 2},
		{TenantID: 7, AssetID: applicationDraft.ID, UserID: 803, Score: 5},
		{TenantID: 7, AssetID: dataset.ID, UserID: 804, Score: 1},
		{TenantID: 8, AssetID: foreignApplication.ID, UserID: 805, Score: 5},
	}
	for _, rating := range ratings {
		if err := db.Create(rating).Error; err != nil {
			t.Fatal(err)
		}
	}

	assetService := NewAssetService(db, nil, nil)
	applicationStats, err := assetService.GetDashboardStats(7, DashboardStatsFilter{TypeCode: "application"})
	if err != nil {
		t.Fatal(err)
	}
	if applicationStats.AssetTotal != 2 || applicationStats.AssetPublished != 1 || applicationStats.AssetDraft != 1 {
		t.Fatalf("application asset status = %#v", applicationStats)
	}
	if applicationStats.ApplicationTotal != 4 || applicationStats.ApplicationPending != 1 || applicationStats.ApplicationApproved != 2 || applicationStats.ApplicationRejected != 1 {
		t.Fatalf("application result stats = %#v", applicationStats)
	}
	if applicationStats.EffectiveAuthorizedUsers != 1 {
		t.Fatalf("effective authorized users = %d, want 1", applicationStats.EffectiveAuthorizedUsers)
	}
	if applicationStats.RatingCount != 3 || applicationStats.RatingAvgScore != 11.0/3.0 {
		t.Fatalf("application rating stats = %#v", applicationStats)
	}
	if len(applicationStats.PublishTrend) != 1 || applicationStats.PublishTrend[0].Count != 1 || len(applicationStats.ApplicationTrend) != 1 || applicationStats.ApplicationTrend[0].Count != 4 {
		t.Fatalf("application trends = publish %#v application %#v", applicationStats.PublishTrend, applicationStats.ApplicationTrend)
	}

	assetStats, err := assetService.GetDashboardStats(7, DashboardStatsFilter{TypeCode: "application", AssetID: applicationPublished.ID})
	if err != nil {
		t.Fatal(err)
	}
	if assetStats.AssetTotal != 1 || assetStats.ApplicationTotal != 3 || assetStats.EffectiveAuthorizedUsers != 1 || assetStats.RatingCount != 2 || assetStats.RatingAvgScore != 3 {
		t.Fatalf("single asset stats = %#v", assetStats)
	}

	allStats, err := assetService.GetDashboardStats(7, DashboardStatsFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if allStats.AssetTotal != 3 || allStats.ApplicationTotal != 5 || allStats.EffectiveAuthorizedUsers != 1 || allStats.RatingCount != 4 {
		t.Fatalf("all asset stats = %#v", allStats)
	}

	_, err = assetService.GetDashboardStats(7, DashboardStatsFilter{AssetID: foreignApplication.ID})
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("foreign asset scope error = %v", err)
	}
}
