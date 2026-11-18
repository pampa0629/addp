package repository

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresManagerPreviewStateUserMigration(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Manager PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })

	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing_table_%t", existing), func(t *testing.T) {
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS manager; DROP TABLE IF EXISTS manager.preview_state").Error; err != nil {
				t.Fatal(err)
			}
			if existing {
				if err := tx.Exec(`CREATE TABLE manager.preview_state (
					id bigserial PRIMARY KEY, tenant_id bigint NOT NULL, item_fingerprint varchar(64) NOT NULL,
					locator text NOT NULL, preferred_mode varchar(32) DEFAULT 'basic_preview', view_state jsonb NOT NULL DEFAULT '{}',
					created_at timestamptz, updated_at timestamptz);
					CREATE UNIQUE INDEX idx_preview_state_tenant_fingerprint ON manager.preview_state(tenant_id, item_fingerprint);
					INSERT INTO manager.preview_state(tenant_id, item_fingerprint, locator, view_state)
						VALUES (7, 'parcel', 'addp://parcel', '{"basic_preview":{"map":{"zoom":3}}}')`).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := MigratePreviewStateSchema(tx); err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := tx.Model(&models.PreviewState{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("unowned state survived migration: count=%d err=%v", count, err)
			}
			if err := MigrateCleanupArtifactSchema(tx); err != nil {
				t.Fatal(err)
			}
			repo := NewPreviewStateRepository(tx)
			token := CleanupTargetToken{Key: CleanupTargetKey{TenantID: 7, Kind: CleanupTargetPreviewState, Identity: "parcel"}}
			view := commonModels.JSONMap{"basic_preview": map[string]any{"map": map[string]any{"zoom": 9}}}
			if err := repo.UpdateViewState(t.Context(), token, 7, 31, "parcel", "addp://parcel", models.PreviewModeBasicPreview, view); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.GetByIdentity(t.Context(), 7, 32, "parcel"); !errors.Is(err, commonapi.ErrNotFound) {
				t.Fatalf("second User inherited an existing preference: %v", err)
			}
			if err := repo.UpdatePreferredMode(t.Context(), token, 7, 32, "parcel", "addp://parcel", models.PreviewModeMapQuickView); err != nil {
				t.Fatal(err)
			}
			if err := MigratePreviewStateSchema(tx); err != nil {
				t.Fatal(err)
			}
			first, err := repo.GetByIdentity(t.Context(), 7, 31, "parcel")
			if err != nil || first.PreferredMode != models.PreviewModeBasicPreview || len(first.ViewState) != 1 {
				t.Fatalf("first User's preferences changed: %#v %v", first, err)
			}
			second, err := repo.GetByIdentity(t.Context(), 7, 32, "parcel")
			if err != nil || second.PreferredMode != models.PreviewModeMapQuickView || len(second.ViewState) != 0 {
				t.Fatalf("second User's preferences changed: %#v %v", second, err)
			}
			if err := repo.DeleteByTenantAndFingerprint(t.Context(), 7, "parcel"); err != nil {
				t.Fatal(err)
			}
			if err := tx.Model(&models.PreviewState{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("item cleanup left user state: count=%d err=%v", count, err)
			}
		})
	}
}

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
 id bigserial PRIMARY KEY, tenant_id integer NOT NULL, user_id bigint NOT NULL, item_fingerprint varchar(64) NOT NULL,
 locator text NOT NULL, preferred_mode varchar(32) DEFAULT 'basic_preview', view_state jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz, updated_at timestamptz, UNIQUE(tenant_id, item_fingerprint, user_id))`).Error; err != nil {
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
	if err := repo.UpdateViewState(t.Context(), CleanupTargetToken{Key: CleanupTargetKey{TenantID: 7, Kind: CleanupTargetPreviewState, Identity: "default-quick"}}, 7, 31, "default-quick", "addp://default", models.PreviewModeMapQuickView, view); err != nil {
		t.Fatal(err)
	}
	state, err := repo.GetByIdentity(t.Context(), 7, 31, "default-quick")
	if err != nil || state.PreferredMode != models.PreviewModeMapQuickView {
		t.Fatalf("initial default lost: %#v %v", state, err)
	}
	createdAt := state.CreatedAt
	if err := repo.UpdatePreferredMode(t.Context(), CleanupTargetToken{Key: CleanupTargetKey{TenantID: 7, Kind: CleanupTargetPreviewState, Identity: "default-quick"}}, 7, 31, "default-quick", "addp://default", models.PreviewModeBasicPreview); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateViewState(t.Context(), CleanupTargetToken{Key: CleanupTargetKey{TenantID: 7, Kind: CleanupTargetPreviewState, Identity: "default-quick"}}, 7, 31, "default-quick", "addp://updated-locator", models.PreviewModeMapQuickView, view); err != nil {
		t.Fatal(err)
	}
	state, err = repo.GetByIdentity(t.Context(), 7, 31, "default-quick")
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
			errors <- repo.UpdateViewState(t.Context(), CleanupTargetToken{Key: CleanupTargetKey{TenantID: 7, Kind: CleanupTargetPreviewState, Identity: fingerprint}}, 7, 31, fingerprint, "addp://race", models.PreviewModeMapQuickView, view)
		}()
		go func() {
			<-start
			errors <- repo.UpdatePreferredMode(t.Context(), CleanupTargetToken{Key: CleanupTargetKey{TenantID: 7, Kind: CleanupTargetPreviewState, Identity: fingerprint}}, 7, 31, fingerprint, "addp://race", models.PreviewModeBasicPreview)
		}()
		close(start)
		for i := 0; i < 2; i++ {
			if err := <-errors; err != nil {
				t.Fatal(err)
			}
		}
		state, err := repo.GetByIdentity(t.Context(), 7, 31, fingerprint)
		if err != nil || state.PreferredMode != models.PreviewModeBasicPreview || len(state.ViewState) != 1 {
			t.Fatalf("concurrent writes lost fields: %#v %v", state, err)
		}
	}
	if err := repo.UpdateViewState(t.Context(), CleanupTargetToken{Key: CleanupTargetKey{TenantID: 8, Kind: CleanupTargetPreviewState, Identity: "default-quick"}}, 8, 31, "default-quick", "addp://other-tenant", models.PreviewModeMapQuickView, nil); err != nil {
		t.Fatal(err)
	}
	state, err = repo.GetByIdentity(t.Context(), 7, 31, "default-quick")
	if err != nil || state.PreferredMode != models.PreviewModeBasicPreview {
		t.Fatalf("tenant isolation lost: %#v %v", state, err)
	}
}
