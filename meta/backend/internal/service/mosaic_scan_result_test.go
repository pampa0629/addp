package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestMosaicScanWholeResultCompletion(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	testMosaicScanWholeCompletion(t, tx)
}

func TestMosaicScanWholeResultAgainstPostgres(t *testing.T) {
	tx := openLineageCollectorPostgresDB(t)
	if err := tx.AutoMigrate(&execution.Event{}); err != nil {
		t.Fatal(err)
	}
	testMosaicScanWholeCompletion(t, tx)
}

func testMosaicScanWholeCompletion(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, scenario := range []string{"s3", "nfs", "missing_whole", "stale_item", "single_manifest", "basic_only", "wrong_format", "stale_lease"} {
		t.Run(scenario, func(t *testing.T) {
			if err := db.SavePoint("mosaic_completion").Error; err != nil {
				t.Fatal(err)
			}
			defer db.RollbackTo("mosaic_completion")
			tenantID := uint(710000099)
			now := time.Now().UTC()
			catalogPath := "mosaics/result"
			run := scantask.NewManualExecution(tenantID, 31, 9, 0, scenario, nil, []string{catalogPath}, nil, "deep", true, commonclient.MetaScanSourceManagerMosaicProducedTarget, now)
			parentID := uuid.NewString()
			run.ParentExecutionID = &parentID
			run.Metadata = commonmodels.JSONMap{"produced_target": commonclient.MetaScanProducedTarget{EngineID: 9, CatalogPath: catalogPath, ManifestRef: "mosaic.addp.json", Attempt: 1}}
			if err := db.Create(run).Error; err != nil {
				t.Fatal(err)
			}
			engines := NewEngineService(db, nil)
			engineType := "s3"
			if scenario == "nfs" {
				engineType = "nfs"
			}
			engines.cacheEngine(tenantID, &commonmodels.Engine{ID: 9, TenantID: &tenantID, EngineType: engineType})
			svc := NewScanExecutionService(db, nil, engines, nil)
			claimed, lease, err := svc.ClaimNextBoundedExecution(t.Context(), "meta-mosaic-result-test", now, time.Hour)
			if err != nil || lease == nil || claimed.ExecutionID != run.ExecutionID {
				t.Fatalf("claim: %+v %v", claimed, err)
			}
			svc.BindBoundedLease(run.ExecutionID, *lease)
			defer svc.UnbindBoundedLease(run.ExecutionID)
			reporter := scantask.NewExecProgressReporter(svc, run.ExecutionID, int(tenantID))
			reporter.Report(models.ScanProgress{Phase: "scanning", FilesDiscovered: 7, ItemsPlanned: 1})
			reporter.Close()
			during, err := svc.GetExecution(t.Context(), run.ExecutionID, int(tenantID))
			if err != nil || during.Metadata["produced_target"] == nil || during.Metadata["scan_progress"] == nil {
				t.Fatalf("progress lost immutable scan provenance: %+v err=%v", during, err)
			}
			scannedAt := now.Add(time.Second)
			item := models.MetaItem{TenantID: tenantID, EngineID: 9, NodeID: 1, ItemType: "object", Name: "result", FullName: catalogPath, Fingerprint: uuid.NewString(), ScannedAt: &scannedAt, ScannedDepth: "deep",
				Attributes: models.JSONMap{"item": map[string]interface{}{"layout": "whole", "format": "raster_mosaic"}}}
			if engineType == "nfs" {
				item.ItemType = "file"
			}
			switch scenario {
			case "stale_item":
				old := now.Add(-time.Hour)
				item.ScannedAt = &old
			case "single_manifest":
				item.FullName += "/mosaic.addp.json"
				item.Attributes["item"] = map[string]interface{}{"layout": "single", "format": "json"}
			case "basic_only":
				item.ScannedDepth = "basic"
			case "wrong_format":
				item.Attributes["item"] = map[string]interface{}{"layout": "whole", "format": "other"}
			}
			if scenario != "missing_whole" {
				if err := db.Create(&item).Error; err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "stale_lease" {
				if err := db.Model(&execution.TaskExecution{}).Where("execution_id = ?", run.ExecutionID).Update("lease_token", uuid.NewString()).Error; err != nil {
					t.Fatal(err)
				}
			}
			err = svc.completeExecutionWithSuccess(context.Background(), claimed, &models.ScanResponse{ItemsScanned: 1}, engineType, now.Add(2*time.Second), 2000)
			if scenario == "stale_lease" {
				if !errors.Is(err, commonapi.ErrConflict) {
					t.Fatalf("stale completion: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			got, err := svc.GetExecution(t.Context(), run.ExecutionID, int(tenantID))
			if err != nil {
				t.Fatal(err)
			}
			want := execution.ExecutionStatusFailed
			if scenario == "s3" || scenario == "nfs" {
				want = execution.ExecutionStatusSuccess
			}
			if scenario == "stale_lease" {
				want = execution.ExecutionStatusRunning
			}
			if got.Status != want {
				t.Fatalf("status=%s want=%s", got.Status, want)
			}
			if got.Metadata["produced_target"] == nil {
				t.Fatal("lost durable target receipt")
			}
			if want == execution.ExecutionStatusSuccess {
				var output commonclient.MetaMosaicOutput
				encoded, _ := json.Marshal(got.Metadata["mosaic_output"])
				if err := json.Unmarshal(encoded, &output); err != nil {
					t.Fatal(err)
				}
				loc, err := resourcetree.ParseURI(output.Locator)
				if err != nil || loc.ItemID == nil || *loc.ItemID != item.ID || loc.FullName() != catalogPath || output.ItemID != item.ID || output.Fingerprint != item.Fingerprint {
					t.Fatalf("not a real whole identity: %+v", output)
				}
			} else if got.Metadata["mosaic_output"] != nil {
				t.Fatal("failed/stale scan published a formal identity")
			}
		})
	}
}
