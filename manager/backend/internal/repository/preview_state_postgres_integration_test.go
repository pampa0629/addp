package repository

import (
	"fmt"
	"os"
	"strings"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresManagerPreviewStateIndependentWrites(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Manager PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	schema := "addp_preview_pref_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Errorf("cleanup: %v", err)
		}
		var count int64
		if err := db.Raw("SELECT count(*) FROM pg_namespace WHERE nspname = ?", schema).Scan(&count).Error; err != nil || count != 0 {
			t.Errorf("schema residue=%d: %v", count, err)
		}
	})
	if err := db.Exec("CREATE TABLE " + schema + `.preview_state (
 id bigserial PRIMARY KEY, tenant_id integer NOT NULL, item_fingerprint varchar(64) NOT NULL,
 locator text NOT NULL, preferred_mode varchar(32) DEFAULT 'basic_preview', view_state jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz, updated_at timestamptz, UNIQUE(tenant_id, item_fingerprint))`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateCleanupArtifactSchema(db); err != nil {
		t.Fatal(err)
	}
	repo := NewPreviewStateRepository(db.Table(schema + ".preview_state").Session(&gorm.Session{}))
	view := commonModels.JSONMap{"quick_view": map[string]interface{}{"scene_3d": map[string]interface{}{"color_mode": "rgb16"}}}
	if err := repo.UpdateViewState(t.Context(), CleanupTargetToken{Key: CleanupTargetKey{TenantID: 7, Kind: CleanupTargetPreviewState, Identity: "default-quick"}}, 7, "default-quick", "addp://default", models.PreviewModeMapQuickView, view); err != nil {
		t.Fatal(err)
	}
	state, err := repo.GetByIdentity(7, "default-quick", "")
	if err != nil || state.PreferredMode != models.PreviewModeMapQuickView {
		t.Fatalf("initial default lost: %#v %v", state, err)
	}
	createdAt := state.CreatedAt
	if err := repo.UpdatePreferredMode(t.Context(), CleanupTargetToken{Key: CleanupTargetKey{TenantID: 7, Kind: CleanupTargetPreviewState, Identity: "default-quick"}}, 7, "default-quick", "addp://default", models.PreviewModeBasicPreview); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateViewState(t.Context(), CleanupTargetToken{Key: CleanupTargetKey{TenantID: 7, Kind: CleanupTargetPreviewState, Identity: "default-quick"}}, 7, "default-quick", "addp://updated-locator", models.PreviewModeMapQuickView, view); err != nil {
		t.Fatal(err)
	}
	state, err = repo.GetByIdentity(7, "default-quick", "")
	if err != nil || state.PreferredMode != models.PreviewModeBasicPreview || len(state.ViewState) != 1 || !state.CreatedAt.Equal(createdAt) || state.Locator != "addp://updated-locator" {
		t.Fatalf("independent fields changed: %#v %v", state, err)
	}

	// Either request may insert first; the explicit mode and view must both survive.
	for n := 0; n < 24; n++ {
		fingerprint := fmt.Sprintf("concurrent-%d", n)
		start := make(chan struct{})
		errors := make(chan error, 2)
		go func() {
			<-start
			errors <- repo.UpdateViewState(t.Context(), CleanupTargetToken{Key: CleanupTargetKey{TenantID: 7, Kind: CleanupTargetPreviewState, Identity: fingerprint}}, 7, fingerprint, "addp://race", models.PreviewModeMapQuickView, view)
		}()
		go func() {
			<-start
			errors <- repo.UpdatePreferredMode(t.Context(), CleanupTargetToken{Key: CleanupTargetKey{TenantID: 7, Kind: CleanupTargetPreviewState, Identity: fingerprint}}, 7, fingerprint, "addp://race", models.PreviewModeBasicPreview)
		}()
		close(start)
		for i := 0; i < 2; i++ {
			if err := <-errors; err != nil {
				t.Fatal(err)
			}
		}
		state, err := repo.GetByIdentity(7, fingerprint, "")
		if err != nil || state.PreferredMode != models.PreviewModeBasicPreview || len(state.ViewState) != 1 {
			t.Fatalf("concurrent writes lost fields: %#v %v", state, err)
		}
	}
	if err := repo.UpdateViewState(t.Context(), CleanupTargetToken{Key: CleanupTargetKey{TenantID: 8, Kind: CleanupTargetPreviewState, Identity: "default-quick"}}, 8, "default-quick", "addp://other-tenant", models.PreviewModeMapQuickView, nil); err != nil {
		t.Fatal(err)
	}
	state, err = repo.GetByIdentity(7, "default-quick", "")
	if err != nil || state.PreferredMode != models.PreviewModeBasicPreview {
		t.Fatalf("tenant isolation lost: %#v %v", state, err)
	}
}
