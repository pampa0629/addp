package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
	commonJSON "github.com/addp/common/jsonmap"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"gorm.io/gorm"
)

// captureMosaicOutput runs only after the exact scan has succeeded. The receipt
// is committed together with its terminal status under the scan's current lease.
func (s *ScanExecutionService) captureMosaicOutput(ctx context.Context, run *execution.TaskExecution) (*commonclient.MetaMosaicOutput, error) {
	cfg := scanflow.ParseExecutionConfig(run.ExecutionConfig)
	var target commonclient.MetaScanProducedTarget
	if !decodeMosaicFact(run.Metadata["produced_target"], &target) || target.EngineID != cfg.EngineID || len(cfg.CatalogPaths) != 1 || target.CatalogPath != cfg.CatalogPaths[0] || run.StartedAt == nil {
		return nil, fmt.Errorf("mosaic scan output provenance unavailable")
	}
	var item models.MetaItem
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND engine_id = ? AND full_name = ?", run.TenantID, target.EngineID, target.CatalogPath).First(&item).Error; err != nil {
		return nil, fmt.Errorf("mosaic scan whole item unavailable")
	}
	if item.ID == 0 || item.Fingerprint == "" || item.ScannedDepth != "deep" || item.ScannedAt == nil || item.ScannedAt.Before(*run.StartedAt) || !isWholeScopeItemAttributes(item.Attributes) || commonJSON.StringFromSections(item.Attributes, "format", "item") != "raster_mosaic" {
		return nil, fmt.Errorf("mosaic scan did not form a current deep raster_mosaic whole item")
	}
	engine, err := s.engineService.GetResourceByIDWithContext(ctx, target.EngineID, uint(run.TenantID))
	if err != nil {
		return nil, fmt.Errorf("mosaic scan engine identity unavailable")
	}
	// Use the same authoritative conversion as the resource-tree API.
	builder := resourcetree.NewTreeBuilder()
	nodes := builder.ConvertMetaItemsForEngine(engine.EngineType, []commonmodels.MetaItem{{
		ID: item.ID, TenantID: item.TenantID, EngineID: item.EngineID, NodeID: item.NodeID,
		ItemType: item.ItemType, Name: item.Name, FullName: item.FullName, Attributes: item.Attributes,
	}})
	tree := builder.ConvertMetaNodes(engine, nodes)
	if len(tree) != 1 || tree[0].Locator == "" {
		return nil, fmt.Errorf("mosaic scan whole locator unavailable")
	}
	return &commonclient.MetaMosaicOutput{Locator: tree[0].Locator, ItemID: item.ID, Fingerprint: item.Fingerprint}, nil
}

// GetMosaicScanResult never applies a user's ad-hoc read rules to a machine.
// The route enforces the dedicated Client and execute Permission; this method
// revalidates the current parent attempt and the durable exact output binding.
func (s *ScanExecutionService) GetMosaicScanResult(ctx context.Context, tenantID int, executionID, parentID string) (*commonclient.MetaMosaicScanResult, error) {
	var result *commonclient.MetaMosaicScanResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var child execution.TaskExecution
		err := tx.Where("tenant_id = ? AND execution_id = ? AND parent_execution_id = ? AND module = ? AND task_type = ? AND source = ?",
			tenantID, executionID, parentID, execution.ModuleMeta, execution.TaskTypeScan, execution.ModuleManager).First(&child).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return commonapi.ErrNotFound
		}
		if err != nil {
			return err
		}
		cfg := scanflow.ParseExecutionConfig(child.ExecutionConfig)
		req := &models.ScanRequest{Source: child.Source, ParentExecutionID: parentID, EngineID: cfg.EngineID, ItemID: cfg.ItemID,
			Targets: cfg.Targets, RefGroups: cfg.RefGroups, CatalogPaths: cfg.CatalogPaths, ScanDepth: cfg.ScanDepth, Force: cfg.Force}
		var original, current commonclient.MetaScanProducedTarget
		if !decodeMosaicFact(child.Metadata["produced_target"], &original) {
			return commonapi.ErrNotFound
		}
		parent, err := verifiedProducedTargetParent(tx, tenantID, req)
		if err != nil {
			return err
		}
		if !decodeMosaicFact(parent.Metadata["produced_target"], &current) || current != original {
			return commonapi.ErrNotFound
		}
		var binding struct {
			ExecutionID string `json:"execution_id"`
		}
		if !decodeMosaicFact(parent.Metadata["meta_scan"], &binding) || binding.ExecutionID != executionID {
			return commonapi.ErrNotFound
		}
		result = &commonclient.MetaMosaicScanResult{ExecutionID: child.ExecutionID, ParentExecutionID: parentID, Status: child.Status}
		if child.Status == execution.ExecutionStatusSuccess {
			var output commonclient.MetaMosaicOutput
			if !decodeMosaicFact(child.Metadata["mosaic_output"], &output) || !validMosaicOutput(output, current) {
				return commonapi.ErrNotFound
			}
			result.Output = &output
		}
		return nil
	})
	return result, err
}

func decodeMosaicFact(value interface{}, target interface{}) bool {
	if value == nil {
		return false
	}
	encoded, err := json.Marshal(value)
	return err == nil && json.Unmarshal(encoded, target) == nil
}

func validMosaicOutput(output commonclient.MetaMosaicOutput, target commonclient.MetaScanProducedTarget) bool {
	loc, err := resourcetree.ParseURI(output.Locator)
	return err == nil && loc.ItemID != nil && *loc.ItemID == output.ItemID && output.ItemID > 0 && output.Fingerprint != "" && loc.EngineID == target.EngineID && loc.FullName() == target.CatalogPath
}
