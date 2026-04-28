//go:build integration

package service

import (
	"context"
	"fmt"
	"os"
	"testing"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

func TestObjectStoragePreviewProvider_ManagedMinIORealShapefile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	commonConfig.LoadEnv()
	cfg := config.Load()

	db, err := repository.InitDatabase(cfg)
	if err != nil {
		t.Fatalf("init database failed: %v", err)
	}

	systemClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
	metaClient := commonClient.NewMetaClientWithInternalKey(cfg.MetaServiceURL, cfg.InternalAPIKey)
	metaClient.SetTenantID(uintPtr(1))

	engine, err := systemClient.GetEngine(9)
	if err != nil {
		t.Fatalf("get engine 9 failed: %v", err)
	}

	metadataRepo := repository.NewMetadataRepository(db, cfg.EncryptionKey)
	contentRegistry := NewObjectContentRegistry()
	contentRegistry.Register(&shapefileContentHandler{
		baseContentHandler: baseContentHandler{
			name:     "builtin:shapefile",
			priority: 90,
			matcher: newObjectContentMatcher(
				[]string{".shp"},
				[]string{"application/x-esri-shapefile", "application/octet-stream", "shp"},
			),
		},
		maxFeatures: 20,
	})

	provider := &objectStoragePreviewProvider{
		metadataRepo:   metadataRepo,
		metaClient:     metaClient,
		metaServiceURL: cfg.MetaServiceURL,
		content:        contentRegistry,
		priority:       95,
	}

	tenantID := uint(1)
	req := &PreviewRequest{
		Engine: &models.Engine{
			ID:             engine.ID,
			Name:           engine.Name,
			EngineType:     engine.EngineType,
			ConnectionInfo: models.ConnectionInfo(engine.ConnectionInfo),
			TenantID:       engine.TenantID,
			IsActive:       engine.IsActive,
		},
		Schema:   "gischain",
		Table:    "data/farmland.shp",
		Page:     1,
		PageSize: 20,
		TenantID: &tenantID,
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}
	if preview == nil || preview.Object == nil || preview.Object.Content == nil {
		t.Fatalf("expected object preview content")
	}

	content := preview.Object.Content
	if content.Kind != "shapefile" {
		t.Fatalf("expected shapefile content, got %s", content.Kind)
	}

	sourceSRID := fmt.Sprint(content.Metadata["source_srid"])
	if sourceSRID != "32650" {
		t.Fatalf("expected source_srid 32650, got %s", sourceSRID)
	}
	if got := fmt.Sprint(content.Metadata["transform_status"]); got != "transformed" {
		t.Fatalf("expected transformed status, got %s", got)
	}
	if got := fmt.Sprint(content.Metadata["render_srid"]); got != "4326" {
		t.Fatalf("expected render_srid 4326, got %s", got)
	}
	if len(preview.GeometryColumns) == 0 {
		t.Fatalf("expected geometry columns")
	}
	if len(preview.RenderGeometryColumns) == 0 {
		t.Fatalf("expected render geometry columns")
	}
	if preview.SRID != 32650 {
		t.Fatalf("expected preview srid 32650, got %d", preview.SRID)
	}

	t.Logf("engine=%d bucket=%s object=%s source_srid=%v transform_status=%v render_srid=%v rows=%d",
		req.Engine.ID,
		req.Schema,
		req.Table,
		content.Metadata["source_srid"],
		content.Metadata["transform_status"],
		content.Metadata["render_srid"],
		len(preview.Rows),
	)
}

func uintPtr(v uint) *uint {
	return &v
}

func init() {
	_ = os.Setenv("ENABLE_SERVICE_INTEGRATION", "true")
	_ = os.Setenv("ENABLE_META_INTEGRATION", "true")
}
