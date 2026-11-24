package repository

import (
	"errors"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"testing"

	"github.com/addp/manager/internal/models"
	"github.com/go-sql-driver/mysql"
)

// The active-result key is released by soft deletion. Test each persisted
// artifact with its production GORM model, including every native JSON column.
func TestIntegrationMySQLManagerPreviewArtifactIdentity(t *testing.T) {
	db := managerMySQLStoreDB(t, "manager", "ADDP_TEST_MYSQL_MANAGER_STORAGE")
	if err := preparePreviewArtifacts(db); err != nil {
		t.Fatal(err)
	}
	for name, newResult := range map[string]func() any{
		"tile-cache": func() any { return &models.TileCache{TenantID: 8, ItemFingerprint: "table", Status: "ready"} },
		"materialized-view": func() any {
			return &models.VectorMaterializedView{TenantID: 8, ItemFingerprint: "roads", Status: "ready"}
		},
		"raster-cog":     func() any { return &models.RasterCOG{TenantID: 8, ItemFingerprint: "raster", Status: "ready"} },
		"model3d-tiles":  func() any { return &models.Model3DTiles{TenantID: 8, ItemFingerprint: "scene", Status: "ready"} },
		"model3d-glb":    func() any { return &models.Model3DGLB{TenantID: 8, ItemFingerprint: "scene", Status: "ready"} },
		"gaussian-splat": func() any { return &models.GaussianSplatKSplat{TenantID: 8, ItemFingerprint: "splat", Status: "ready"} },
		"point-cloud":    func() any { return &models.PointCloudCOPC{TenantID: 8, ItemFingerprint: "cloud", Status: "ready"} },
		"pptx-pdf":       func() any { return &models.PPTXPDF{TenantID: 8, ItemFingerprint: "slides", Status: "ready"} },
	} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			tx := db.WithContext(ctx).Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			t.Cleanup(func() {
				if err := tx.Rollback().Error; err != nil {
					t.Error(err)
				}
			})
			first := newResult()
			if err := tx.Create(first).Error; err != nil {
				t.Fatal(err)
			}
			var dup *mysql.MySQLError
			if err := tx.Create(newResult()).Error; !errors.As(err, &dup) || dup.Number != 1062 {
				t.Fatalf("duplicate current result: %v", err)
			}
			if err := tx.Delete(first).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Create(newResult()).Error; err != nil {
				t.Fatalf("reuse after deletion: %v", err)
			}
			var total int64
			if err := tx.Model(newResult()).Where("tenant_id = ?", 8).Count(&total).Error; err != nil || total != 1 {
				t.Fatalf("current count=%d, %v", total, err)
			}
		})
	}

	if err := ensureTaskDefinitionSchema(db); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	raster := NewRasterCOGRepository(db)
	first := models.RasterCOG{TenantID: 9, ItemFingerprint: "forecast", Status: models.RasterCOGStatusReady, FileName: "FORECAST.grib"}
	second := first
	first.Metadata = commonModels.JSONMap{"grib_map": commonModels.JSONMap{"message": "message-1"}}
	second.Metadata = commonModels.JSONMap{"grib_map": commonModels.JSONMap{"message": "message-2"}}
	for _, row := range []*models.RasterCOG{&first, &second} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	read, err := raster.GetLatestReadyByFingerprint(ctx, 9, "forecast", "message-2")
	if err != nil || read == nil || read.ID != second.ID {
		t.Fatalf("raster message result: %+v %v", read, err)
	}
	missing, err := raster.GetCurrentByFingerprint(ctx, 9, "forecast", "MESSAGE-2")
	if err != nil || missing != nil {
		t.Fatalf("incorrect case matched raster message: %+v %v", missing, err)
	}
	items, total, err := raster.List(ctx, RasterCOGFilter{TenantID: 9, Q: "foreCAST.gRiB"})
	if err != nil || len(items) != 2 || total != 2 {
		t.Fatalf("case-insensitive result list: %d/%d %v", len(items), total, err)
	}
	task := models.TaskDefinition{TenantID: 9, Name: "forecast", Enabled: true, Config: commonModels.JSONMap{
		"target": commonModels.JSONMap{"item_fingerprint": "forecast"}, "raster": commonModels.JSONMap{"message": "message-2"},
	}}
	if err := createTaskDefinition(ctx, db, commonExecution.TaskTypeRasterCOGGeneration, &task); err != nil {
		t.Fatal(err)
	}
	found, err := raster.GetTaskByItemFingerprint(ctx, 9, "forecast", "message-2")
	if err != nil || found == nil || found.ID != task.ID {
		t.Fatalf("raster task identity: %+v %v", found, err)
	}
}
