package service

import (
	"context"
	"errors"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

func TestPreviewStateInitialModeFollowsCapability(t *testing.T) {
	for _, mode := range []string{models.PreviewModeMapQuickView, models.PreviewModeBasicPreview} {
		t.Run(mode, func(t *testing.T) {
			db := newTileCacheTaskServiceTestDB(t)
			svc := NewQuickViewService(db, nil)
			identity := QuickViewIdentity{TenantID: 7, ItemFingerprint: "initial-mode", Locator: "addp://engine/14/path/test.las?type=file"}
			view := commonModels.JSONMap{"quick_view": map[string]interface{}{"scene_3d": map[string]interface{}{"color_mode": "rgb16"}}}
			err := svc.UpdateViewStateByIdentity(context.Background(), identity, view, func(context.Context) (*QuickViewCapability, error) {
				return &QuickViewCapability{PreferredMode: mode}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			stored, err := svc.repo.GetByIdentity(7, identity.ItemFingerprint, identity.Locator)
			if err != nil || stored.PreferredMode != mode || len(stored.ViewState) != 1 {
				t.Fatalf("first state = %#v, err=%v, want mode %s with view state", stored, err, mode)
			}
		})
	}
}

func TestPreviewStatePointCloudDefaultSurvivesCameraSave(t *testing.T) {
	for _, item := range []struct{ format, url, mode string }{
		{"las", "", models.PreviewModeMapQuickView},
		{"copc", "/existing-content", models.PreviewModeBasicPreview},
	} {
		t.Run(item.format, func(t *testing.T) {
			db := newTileCacheTaskServiceTestDB(t)
			if err := db.Exec(`CREATE TABLE manager.point_cloud_copc (id INTEGER PRIMARY KEY, tenant_id INTEGER, item_fingerprint TEXT, status TEXT, updated_at DATETIME, deleted_at DATETIME)`).Error; err != nil {
				t.Fatal(err)
			}
			svc := NewQuickViewService(db, nil)
			source := QuickViewSource{
				Identity:   QuickViewIdentity{TenantID: 7, ItemFingerprint: "point-default", Locator: "addp://engine/14/path/test." + item.format + "?type=file"},
				EngineID:   14,
				PointCloud: &PointCloudCOPCSource{Format: item.format, Layout: "single", PreviewURL: item.url},
			}
			loader := func(ctx context.Context) (*QuickViewCapability, error) {
				return svc.BuildCapabilityFromSource(ctx, source)
			}
			before, err := loader(context.Background())
			if err != nil || before.PreferredMode != item.mode {
				t.Fatalf("default before save: %#v %v", before, err)
			}
			view := commonModels.JSONMap{"quick_view": map[string]interface{}{"scene_3d": map[string]interface{}{"position": []float64{1, 2, 3}, "color_mode": "rgb16"}}}
			if err := svc.UpdateViewStateByIdentity(context.Background(), source.Identity, view, loader); err != nil {
				t.Fatal(err)
			}
			after, err := loader(context.Background())
			if err != nil || after.PreferredMode != before.PreferredMode {
				t.Fatalf("camera save changed default mode: %#v %v", after, err)
			}
		})
	}
}

func TestPreviewStatePreservesExplicitModeDuringFirstStateWrite(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	identity := QuickViewIdentity{TenantID: 7, ItemFingerprint: "concurrent-mode", Locator: "addp://engine/14/path/test.las?type=file"}
	view := commonModels.JSONMap{"quick_view": map[string]interface{}{"scene_3d": map[string]interface{}{"color_mode": "rgb8"}}}
	err := svc.UpdateViewStateByIdentity(context.Background(), identity, view, func(context.Context) (*QuickViewCapability, error) {
		// A user's explicit choice arrives after the missing-state lookup, before insertion.
		if err := svc.repo.UpdatePreferredMode(t.Context(), repository.CleanupTargetToken{Key: repository.CleanupTargetKey{TenantID: 7, Kind: repository.CleanupTargetPreviewState, Identity: identity.ItemFingerprint}}, 7, identity.ItemFingerprint, identity.Locator, models.PreviewModeBasicPreview); err != nil {
			return nil, err
		}
		return &QuickViewCapability{PreferredMode: models.PreviewModeMapQuickView}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := svc.repo.GetByIdentity(7, identity.ItemFingerprint, identity.Locator)
	if err != nil || stored.PreferredMode != models.PreviewModeBasicPreview || len(stored.ViewState) != 1 {
		t.Fatalf("concurrent explicit choice lost: %#v %v", stored, err)
	}
	if err := svc.UpdateViewStateByIdentity(context.Background(), identity, view, func(context.Context) (*QuickViewCapability, error) {
		t.Fatal("existing preference must not need capability discovery")
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.UpdatePreferredMode(t.Context(), repository.CleanupTargetToken{Key: repository.CleanupTargetKey{TenantID: 7, Kind: repository.CleanupTargetPreviewState, Identity: identity.ItemFingerprint}}, 7, identity.ItemFingerprint, identity.Locator, models.PreviewModeMapQuickView); err != nil {
		t.Fatal(err)
	}
	stored, err = svc.repo.GetByIdentity(7, identity.ItemFingerprint, identity.Locator)
	if err != nil || stored.PreferredMode != models.PreviewModeMapQuickView || len(stored.ViewState) != 1 {
		t.Fatalf("mode write erased camera/color: %#v %v", stored, err)
	}
}

func TestPreviewStateFailedCapabilityDoesNotCreatePreference(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	identity := QuickViewIdentity{TenantID: 7, ItemFingerprint: "failed-mode", Locator: "addp://engine/14/path/test.las?type=file"}
	want := errors.New("capability unavailable")
	if err := svc.UpdateViewStateByIdentity(context.Background(), identity, nil, func(context.Context) (*QuickViewCapability, error) { return nil, want }); !errors.Is(err, want) {
		t.Fatalf("error=%v want %v", err, want)
	}
	var count int64
	if err := db.Model(&models.PreviewState{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed initialization persisted %d states: %v", count, err)
	}
	if err := svc.UpdateViewStateByIdentity(context.Background(), identity, nil, func(context.Context) (*QuickViewCapability, error) {
		return &QuickViewCapability{PreferredMode: "guess"}, nil
	}); !errors.Is(err, ErrQuickViewInvalidPreferredMode) {
		t.Fatalf("invalid initial mode: %v", err)
	}
}
