package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/addp/asset/internal/models"
	commonClient "github.com/addp/common/client"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestAssetSyncConcurrentAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_ASSET_SYNC_TEST_DSN")
	if dsn == "" {
		t.Skip("ADDP_ASSET_SYNC_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if err := db.Exec("DROP SCHEMA IF EXISTS asset CASCADE").Error; err != nil {
		t.Fatalf("drop asset schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA IF EXISTS asset CASCADE").Error })
	if err := db.Exec("CREATE SCHEMA asset").Error; err != nil {
		t.Fatalf("create asset schema: %v", err)
	}
	if err := db.AutoMigrate(&models.TypeDefinition{}, &models.Asset{}); err != nil {
		t.Fatalf("migrate asset tables: %v", err)
	}
	typeDefinition := models.TypeDefinition{
		TenantID: 0, Name: "数据集", Code: "dataset", SourceModule: "meta",
		DiscoveryPath: "/api/v1/meta/assets/discoverable", Enabled: true,
	}
	if err := db.Create(&typeDefinition).Error; err != nil {
		t.Fatalf("create type definition: %v", err)
	}

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer asset-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode([]commonClient.DiscoverableAsset{{
			SourceReference: "engine:1/table:orders", Name: "orders",
		}})
	}))
	defer source.Close()
	assetService := NewAssetService(
		db, map[string]string{"meta": source.URL},
		commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
			return "asset-token", nil
		}), nil,
	)

	results := make(chan *SyncResult, 2)
	errors := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for range 2 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			result, syncErr := assetService.Sync(context.Background(), 7)
			results <- result
			errors <- syncErr
		}()
	}
	waitGroup.Wait()
	close(results)
	close(errors)

	for syncErr := range errors {
		if syncErr != nil {
			t.Fatalf("Sync() error = %v", syncErr)
		}
	}
	var created, skipped, errorCount int
	for result := range results {
		created += result.Created
		skipped += result.Skipped
		errorCount += result.Errors
	}
	if created != 1 || skipped != 1 || errorCount != 0 {
		t.Fatalf("aggregate result = created:%d skipped:%d errors:%d", created, skipped, errorCount)
	}
	var count int64
	if err := db.Model(&models.Asset{}).Where("tenant_id = ?", 7).Count(&count).Error; err != nil {
		t.Fatalf("count assets: %v", err)
	}
	if count != 1 {
		t.Fatalf("asset count = %d, want 1", count)
	}
}
