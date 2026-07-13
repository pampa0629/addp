package preview

import (
	"context"
	"testing"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
	"github.com/addp/manager/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestResolveCADPreviewURLUsesTenantFingerprintAndSourceFacts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:cad-preview-artifact?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
		t.Fatalf("attach manager schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.cad_previews (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		item_fingerprint TEXT NOT NULL,
		item_id INTEGER,
		locator TEXT,
		task_id INTEGER,
		last_execution_id TEXT,
		source_engine_id INTEGER NOT NULL,
		source_format TEXT NOT NULL,
		source_size_bytes INTEGER,
		storage_ref TEXT NOT NULL,
		manifest_ref TEXT NOT NULL,
		thumbnail_ref TEXT,
		tile_count INTEGER,
		tile_size INTEGER,
		min_zoom INTEGER,
		max_zoom INTEGER,
		status TEXT NOT NULL,
		bounds TEXT,
		metadata TEXT,
		error_message TEXT,
		created_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create CAD preview table: %v", err)
	}
	result := &models.CADPreview{
		TenantID: 7, ItemFingerprint: "cad-fingerprint", SourceEngineID: 26, SourceFormat: "dwg",
		StorageRef: "s3://manager/cad", ManifestRef: "manifest.json", Status: models.CADPreviewStatusReady,
	}
	if err := db.Create(result).Error; err != nil {
		t.Fatalf("create CAD preview: %v", err)
	}
	tenantID := uint(7)
	url, err := resolveCADPreviewURL(context.Background(), repository.NewCADPreviewRepository(db), &PreviewRequest{
		TenantID: &tenantID, ItemFingerprint: "cad-fingerprint", Engine: &models.Engine{ID: 26},
	}, &objectcontent.ObjectContentRequest{Format: "dwg"})
	if err != nil {
		t.Fatalf("resolveCADPreviewURL() error = %v", err)
	}
	want := "/api/v1/manager/cad-previews/1/manifest"
	if url != want {
		t.Fatalf("url = %q, want %q", url, want)
	}

	otherTenant := uint(8)
	url, err = resolveCADPreviewURL(context.Background(), repository.NewCADPreviewRepository(db), &PreviewRequest{
		TenantID: &otherTenant, ItemFingerprint: "cad-fingerprint", Engine: &models.Engine{ID: 26},
	}, &objectcontent.ObjectContentRequest{Format: "dwg"})
	if err != nil || url != "" {
		t.Fatalf("cross-tenant url = %q, err = %v", url, err)
	}
}
