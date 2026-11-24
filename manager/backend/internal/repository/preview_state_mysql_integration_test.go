package repository

import (
	"errors"
	"testing"

	commonapi "github.com/addp/common/api"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestIntegrationMySQLManagerPreviewState(t *testing.T) {
	db := managerMySQLStoreDB(t, "manager", "ADDP_TEST_MYSQL_MANAGER_STORAGE")
	if err := MigratePreviewStateSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := MigrateCleanupArtifactSchema(db); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	repo := NewPreviewStateRepository(db)
	const tenant, first, second = 83, 11, 12
	item := uuid.NewString()
	token, err := ObserveCleanupTarget(ctx, db, CleanupTargetKey{TenantID: tenant, Kind: CleanupTargetPreviewState, Identity: item})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateViewState(ctx, token, tenant, first, item, "addp://orders", models.PreviewModeBasicPreview, commonModels.JSONMap{"zoom": 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetByIdentity(ctx, tenant, second, item); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("other user read preview preference: %v", err)
	}
	if err := repo.UpdatePreferredMode(ctx, token, tenant, first, item, "addp://orders", models.PreviewModeMapQuickView); err != nil {
		t.Fatal(err)
	}
	state, err := repo.GetByIdentity(ctx, tenant, first, item)
	if err != nil || state.PreferredMode != models.PreviewModeMapQuickView || state.ViewState["zoom"] != float64(3) {
		t.Fatalf("independent preference writes: %+v, %v", state, err)
	}
	tokens := []CleanupTargetToken{token}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := HoldCleanupTargets(ctx, tx, tokens, "cleanup"); err != nil {
			return err
		}
		if err := NewPreviewStateRepository(tx).DeleteByTenantAndFingerprint(ctx, tenant, item); err != nil {
			return err
		}
		return FinishCleanupTargets(ctx, tx, tokens, "cleanup", true)
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdatePreferredMode(ctx, token, tenant, first, item, "addp://orders", models.PreviewModeBasicPreview); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("stale preference write: %v", err)
	}
	if _, err := repo.GetByIdentity(ctx, tenant, first, item); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("preference survived cleanup: %v", err)
	}
}
