package api

import (
	"testing"
	"time"

	"github.com/addp/common/authtest"
	metaauthorization "github.com/addp/meta/internal/authorization"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
)

func TestAssetDiscoverableRouteAuthenticationAndTenantContract(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	now := time.Now().UTC()
	for _, item := range []models.MetaItem{
		{TenantID: 7, EngineID: 1, NodeID: 1, ItemType: "table", Name: "tenant-seven", FullName: "public.seven", Fingerprint: "seven", ScannedAt: &now},
		{TenantID: 8, EngineID: 1, NodeID: 1, ItemType: "table", Name: "tenant-eight", FullName: "public.eight", Fingerprint: "eight", ScannedAt: &now},
	} {
		if err := db.Create(&item).Error; err != nil {
			t.Fatalf("create meta item: %v", err)
		}
	}

	systemServer := authtest.NewTenantAuthContextServer(t, "7", metaauthorization.PermissionMetaCatalogRead)
	defer systemServer.Close()
	engineService := service.NewEngineService(db, nil)
	scanService := service.NewScanService(db, engineService)
	cfg := &config.Config{}
	cfg.SystemServiceURL = systemServer.URL
	router := SetupRouter(cfg, db, engineService, scanService, nil, nil, nil, nil)

	authtest.AssertAssetDiscoverableContract(t, router, "/api/v1/meta/assets/discoverable", "tenant-seven")
}
