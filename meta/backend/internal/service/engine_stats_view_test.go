package service

import (
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
)

func TestBuildResourceWithStatsProjectsScanStats(t *testing.T) {
	lastScan := time.Date(2026, 6, 6, 8, 30, 0, 0, time.UTC)
	lastCheck := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	tenantID := uint(1)
	resource := &commonModels.Engine{
		ID:               9,
		TenantID:         &tenantID,
		Name:             "Business MinIO",
		EngineType:       "s3",
		ConnectionStatus: "healthy",
		LastCheckAt:      &lastCheck,
		CheckMessage:     "ok",
	}
	stats := &engineScanStats{
		totalCount:   map[uint]int64{9: 12},
		scannedCount: map[uint]int64{9: 7},
		lastScanByID: map[uint]*time.Time{9: &lastScan},
	}

	view := buildResourceWithStats(resource, stats)
	if view.EngineID != resource.ID || view.ResourceName != resource.Name || view.ResourceType != resource.EngineType {
		t.Fatalf("identity fields = %#v", view)
	}
	if view.TotalCatalogNodes != 12 || view.ScannedCatalogNodes != 7 || view.UnscannedCatalogNodes != 5 {
		t.Fatalf("scan counts = total:%d scanned:%d unscanned:%d", view.TotalCatalogNodes, view.ScannedCatalogNodes, view.UnscannedCatalogNodes)
	}
	if view.ScannedAt != "2026-06-06 08:30:00" {
		t.Fatalf("ScannedAt = %q", view.ScannedAt)
	}
	if view.LastCheckAt != "2026-06-06 09:00:00" || view.ConnectionStatus != "healthy" || view.CheckMessage != "ok" {
		t.Fatalf("connection fields = %#v", view)
	}
	if view.EngineFamily == "" || view.CatalogRootTerm == "" || view.CatalogLeafTerm == "" {
		t.Fatalf("catalog terms not projected: %#v", view)
	}
}
