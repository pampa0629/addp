package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRasterMosaicWholeResultLifecycle(t *testing.T) {
	tx := newRasterMosaicTaskServiceTestDB(t).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	testRasterMosaicWholeLifecycle(t, tx)
}

func TestIntegrationPostgresManagerMosaicWholeResultLifecycle(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Manager PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(os.Getenv("MANAGER_POSTGRES_TEST_DSN")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	for _, schema := range []string{"common", "manager"} {
		if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS " + schema).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.AutoMigrate(&execution.TaskExecution{}, &models.TaskDefinition{}, &models.TaskResourceBinding{}); err != nil {
		t.Fatal(err)
	}
	testRasterMosaicWholeLifecycle(t, tx)
}

func testRasterMosaicWholeLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, scenario := range []string{"wait_success", "scan_failed", "scan_timeout", "scan_cancelled", "deadline", "read_denied", "manifest_identity", "wrong_engine", "missing_item", "missing_fingerprint", "stale_lease"} {
		t.Run(scenario, func(t *testing.T) {
			tx := db
			if err := tx.SavePoint("mosaic_case").Error; err != nil {
				t.Fatal(err)
			}
			defer tx.RollbackTo("mosaic_case")
			tenantID := uint(710000098)
			repo := repository.NewRasterMosaicRepository(tx)
			store := execution.NewTaskExecutionRepository(tx)
			svc := NewRasterMosaicTaskService(repo, store)
			task := &models.RasterMosaicTask{TenantID: tenantID, Name: "mosaic " + uuid.NewString(), Enabled: true, Config: commonModels.JSONMap{
				"source":    commonModels.JSONMap{"node_locator": "addp://engine/26/path/rasters?type=prefix&node_id=11", "source_engine_id": 26},
				"placement": commonModels.JSONMap{"mode": "detached"},
				"target":    commonModels.JSONMap{"storage_locator": "addp://engine/27/path/mosaics?type=bucket&node_id=12", "target_engine_id": 27, "dataset_name": "srtm_mosaic"},
			}}
			if err := svc.Create(t.Context(), task); err != nil {
				t.Fatal(err)
			}
			parentID, scanID := uuid.NewString(), uuid.NewString()
			started := time.Now().UTC().Add(-time.Minute)
			ctx := startRasterMosaicTestExecution(t, tx, repo, task, parentID, started)
			deadlineCtx, cancel := context.WithCancelCause(ctx)
			defer cancel(nil)
			svc.SetExecutor(fakeRasterMosaicExecutor{result: &RasterMosaicExecutionResult{ManifestLocator: "/vsis3/mosaics/srtm_mosaic/mosaic.addp.json", ManifestRef: "mosaic.addp.json", LeafCount: 4}})
			calls := 0
			scan := &fakeRasterMosaicMetaScanSubmitter{run: &execution.TaskExecution{ExecutionID: scanID, Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, Status: execution.ExecutionStatusPending}}
			scan.read = func(readCtx context.Context) (*commonClient.MetaMosaicScanResult, error) {
				calls++
				stored, err := store.GetByExecutionID(t.Context(), parentID, int(tenantID))
				if err != nil {
					t.Fatal(err)
				}
				var binding struct {
					ExecutionID string `json:"execution_id"`
				}
				data, _ := json.Marshal(stored.Metadata["meta_scan"])
				_ = json.Unmarshal(data, &binding)
				if stored.Status != execution.ExecutionStatusRunning || stored.CompletedAt != nil || binding.ExecutionID != scanID || stored.Metadata["lineage_facts"] != nil || stored.Progress != 99 {
					t.Fatalf("parent ended before registration: %+v", stored)
				}
				result := &commonClient.MetaMosaicScanResult{ExecutionID: scanID, ParentExecutionID: parentID, Status: execution.ExecutionStatusSuccess, Output: &commonClient.MetaMosaicOutput{Locator: "addp://engine/27/path/mosaics/srtm_mosaic?type=object&item_id=42", ItemID: 42, Fingerprint: "whole-fingerprint"}}
				switch scenario {
				case "wait_success":
					if calls == 1 {
						result.Status = execution.ExecutionStatusRunning
						result.Output = nil
					}
				case "scan_failed":
					result.Status = execution.ExecutionStatusFailed
					result.Output = nil
				case "scan_timeout":
					result.Status = execution.ExecutionStatusTimeout
					result.Output = nil
				case "scan_cancelled":
					result.Status = execution.ExecutionStatusCancelled
					result.Output = nil
				case "deadline":
					cancel(context.DeadlineExceeded)
					return nil, context.DeadlineExceeded
				case "read_denied":
					return nil, errors.New("Meta result dependency status 403")
				case "manifest_identity":
					result.Output.Locator = "addp://engine/27/path/mosaics/srtm_mosaic/mosaic.addp.json?type=object&item_id=42"
				case "wrong_engine":
					result.Output.Locator = "addp://engine/28/path/mosaics/srtm_mosaic?type=object&item_id=42"
				case "missing_item":
					result.Output.ItemID = 0
				case "missing_fingerprint":
					result.Output.Fingerprint = ""
				case "stale_lease":
					if err := tx.Model(&execution.TaskExecution{}).Where("execution_id = ?", parentID).Update("lease_token", uuid.NewString()).Error; err != nil {
						t.Fatal(err)
					}
				}
				return result, nil
			}
			svc.SetMetaScanSubmitter(scan)
			svc.executeRasterMosaicGeneration(deadlineCtx, task, parentID, started)
			got, err := store.GetByExecutionID(t.Context(), parentID, int(tenantID))
			if err != nil {
				t.Fatal(err)
			}
			want := execution.ExecutionStatusFailed
			if scenario == "wait_success" {
				want = execution.ExecutionStatusSuccess
			}
			if scenario == "deadline" || scenario == "scan_timeout" {
				want = execution.ExecutionStatusTimeout
			}
			if scenario == "stale_lease" {
				want = execution.ExecutionStatusRunning
			}
			if got.Status != want {
				t.Fatalf("status=%s want=%s", got.Status, want)
			}
			if got.Metadata["produced_target"] == nil || got.Metadata["meta_scan"] == nil || got.Metadata["manifest_locator"] != "/vsis3/mosaics/srtm_mosaic/mosaic.addp.json" {
				t.Fatalf("lost retained output/scan binding: %+v", got.Metadata)
			}
			if scenario == "wait_success" {
				if calls != 2 || got.Metadata["lineage_facts"] == nil {
					t.Fatal("did not wait for formal output")
				}
			} else if got.Metadata["lineage_facts"] != nil {
				t.Fatal("failed or stale execution published lineage")
			}
		})
	}
}
