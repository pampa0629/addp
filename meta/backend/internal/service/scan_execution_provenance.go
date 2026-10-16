package service

import (
	"encoding/json"
	"errors"
	"path"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/common/format/rastermosaic"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// inheritProducedTargetActor runs only in the scan creation transaction.
// Caller authentication is enforced at the HTTP boundary; this verifies durable
// provenance and copies identity facts, never an execution authorization grant.
func inheritProducedTargetActor(tx *gorm.DB, child *execution.TaskExecution, req *models.ScanRequest) error {
	if req.SchemaChangeSource != nil {
		return inheritAppliedSchemaChangeActor(tx, child, req)
	}
	if req.ParentExecutionID == "" {
		return nil
	}
	parent, err := verifiedProducedTargetParent(tx, child.TenantID, req)
	if err != nil {
		return err
	}
	if parent.TaskType == execution.TaskTypeRasterMosaicGeneration {
		if child.Metadata == nil {
			child.Metadata = map[string]interface{}{}
		}
		child.Metadata["produced_target"] = parent.Metadata["produced_target"]
	}
	id := parent.ExecutionID
	child.ParentExecutionID = &id
	return execution.CopyActorOrigin(child, parent)
}

// Caller holds the returned parent's shared lock until its transaction ends.
func verifiedProducedTargetParent(tx *gorm.DB, tenantID int, req *models.ScanRequest) (*execution.TaskExecution, error) {
	module := req.Source
	if module != execution.ModuleDevelop && module != execution.ModuleManager && module != execution.ModuleTransfer {
		return nil, commonapi.ErrBadRequest
	}
	parentID, err := uuid.Parse(req.ParentExecutionID)
	if err != nil || parentID == uuid.Nil {
		return nil, commonapi.ErrBadRequest
	}
	var parent execution.TaskExecution
	err = tx.Clauses(clause.Locking{Strength: "SHARE"}).Where(
		"tenant_id = ? AND execution_id = ? AND module = ? AND status = ?",
		tenantID, parentID.String(), module, execution.ExecutionStatusRunning,
	).First(&parent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, commonapi.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if parent.ActorPrincipalID == nil || *parent.ActorPrincipalID <= 0 ||
		parent.ActorTenantMembershipID == nil || *parent.ActorTenantMembershipID <= 0 ||
		parent.IssuedAuthorizationVersion == nil || *parent.IssuedAuthorizationVersion <= 0 ||
		parent.TriggeredBy == nil || int64(*parent.TriggeredBy) != *parent.ActorPrincipalID {
		return nil, commonapi.ErrNotFound
	}
	if module == execution.ModuleManager {
		if parent.TaskType != execution.TaskTypeRasterMosaicGeneration && parent.TaskType != execution.TaskTypeVectorTileSetGeneration {
			return nil, commonapi.ErrNotFound
		}
		now := time.Now().UTC()
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
				return nil, err
			}
		}
		if parent.ExecutionBoundary != execution.ExecutionBoundaryBounded || parent.Attempt <= 0 || parent.LeaseToken == nil || *parent.LeaseToken == "" || parent.LeaseOwner == nil || *parent.LeaseOwner == "" || parent.LeaseExpiresAt == nil || !parent.LeaseExpiresAt.After(now) || !matchesManagerProducedTarget(parent, req) {
			return nil, commonapi.ErrNotFound
		}
	} else if module == execution.ModuleTransfer {
		if parent.TaskType != execution.TaskTypeSync || !matchesTransferProducedTarget(parent, req) {
			return nil, commonapi.ErrNotFound
		}
	} else if parent.TaskType != execution.TaskTypeWorkflow || !matchesDevelopProducedTarget(parent, req) {
		return nil, commonapi.ErrNotFound
	}
	return &parent, nil
}

func matchesManagerMosaicProducedTarget(parent execution.TaskExecution, req *models.ScanRequest) bool {
	if req.EngineID == 0 || req.NodeID != 0 || req.ItemID != 0 || len(req.Targets) != 0 || len(req.RefGroups) != 0 || len(req.CatalogPaths) != 1 || req.ScanDepth != "deep" || !req.Force {
		return false
	}
	var target commonclient.MetaScanProducedTarget
	encoded, err := json.Marshal(parent.Metadata["produced_target"])
	if err != nil || json.Unmarshal(encoded, &target) != nil {
		return false
	}
	manifest, ok := parent.Metadata["manifest_locator"].(string)
	if !ok || strings.TrimSpace(manifest) == "" {
		return false
	}
	if target.Attempt != parent.Attempt || target.EngineID != req.EngineID || target.ManifestRef != rastermosaic.ManifestFileName || parent.Metadata["manifest_ref"] != target.ManifestRef {
		return false
	}
	if target.CatalogPath == "" || target.CatalogPath == "." || target.CatalogPath == ".." || path.IsAbs(target.CatalogPath) || strings.HasPrefix(target.CatalogPath, "../") || strings.Contains(target.CatalogPath, "\\") || path.Clean(target.CatalogPath) != target.CatalogPath {
		return false
	}
	return target.CatalogPath == req.CatalogPaths[0]
}

func matchesDevelopProducedTarget(parent execution.TaskExecution, req *models.ScanRequest) bool {
	if req.EngineID == 0 || req.NodeID != 0 || req.ItemID != 0 || len(req.CatalogPaths) != 0 || req.ScanDepth != "deep" || !req.Force {
		return false
	}
	// outputs is the existing stable result contract, rather than free-form
	// source strings or client-declared user / authorization fields.
	type resourceOutput struct {
		Resource struct {
			Locator string `json:"locator"`
		} `json:"resource"`
	}
	var outputs map[string]struct {
		resourceOutput
		Tiles map[string]resourceOutput `json:"tiles"`
	}
	encoded, err := json.Marshal(parent.Metadata["outputs"])
	if err != nil || json.Unmarshal(encoded, &outputs) != nil {
		return false
	}
	for taskID, output := range outputs {
		// Only the named tiles contract carries batch leaves. Do not traverse
		// arbitrary result JSON or infer a prefix scope from a tile object.
		for _, tile := range output.Tiles {
			locator, err := resourcetree.ParseURI(tile.Resource.Locator)
			if err == nil && (locator.Type == resourcetree.TypeFile || locator.Type == resourcetree.TypeObject) &&
				matchesDevelopProducedLeaf(tile.Resource.Locator, req) {
				return true
			}
		}
		if matchesDevelopProducedLeaf(output.Resource.Locator, req) {
			return true
		}
		locator, err := resourcetree.ParseURI(output.Resource.Locator)
		if err != nil || locator.EngineID != req.EngineID || len(locator.Path) == 0 {
			continue
		}
		if locator.Type == resourcetree.TypeObject && len(req.RefGroups) == 0 && len(req.Targets) == 1 {
			var result struct {
				ProducedTargets []struct {
					TaskID   string   `json:"task_id"`
					EngineID uint     `json:"engine_id"`
					Type     string   `json:"type"`
					Path     []string `json:"path"`
					Locator  string   `json:"locator"`
					Layout   string   `json:"layout"`
				} `json:"produced_targets"`
			}
			encoded, err := json.Marshal(parent.Metadata["result"])
			if err != nil || json.Unmarshal(encoded, &result) != nil {
				continue
			}
			for _, target := range result.ProducedTargets {
				if target.TaskID != taskID || target.EngineID != locator.EngineID || target.Type != "object" || target.Layout != "whole" ||
					target.Locator != output.Resource.Locator || !slices.Equal(target.Path, locator.Path) {
					continue
				}
				locator.Type = resourcetree.TypePrefix
				if req.Targets[0] == locator.ToURI() {
					return true
				}
			}
		}
	}
	return false
}

// matchesDevelopProducedLeaf matches one exact persisted resource. Files keep
// their existing single-primary scan contract; objects never expand to prefixes.
func matchesDevelopProducedLeaf(raw string, req *models.ScanRequest) bool {
	locator, err := resourcetree.ParseURI(raw)
	if err != nil || locator.EngineID != req.EngineID || len(locator.Path) == 0 {
		return false
	}
	if locator.Type == resourcetree.TypeFile {
		return len(req.Targets) == 0 && len(req.RefGroups) == 1 && len(req.RefGroups[0].Refs) == 0 &&
			req.RefGroups[0].Primary == strings.Join(locator.Path, "/")
	}
	return len(req.RefGroups) == 0 && len(req.Targets) == 1 && req.Targets[0] == raw
}

// Mosaic completion requires a manifest receipt, not merely source=manager.
func isMosaicProducedScan(run *execution.TaskExecution) bool {
	if run == nil || run.Source != execution.ModuleManager || run.ParentExecutionID == nil {
		return false
	}
	var target commonclient.MetaScanProducedTarget
	return decodeMosaicFact(run.Metadata["produced_target"], &target) && target.ManifestRef == rastermosaic.ManifestFileName
}

func matchesTransferProducedTarget(parent execution.TaskExecution, req *models.ScanRequest) bool {
	if req.EngineID == 0 || req.NodeID != 0 || req.ItemID != 0 || len(req.CatalogPaths) != 0 || req.ScanDepth != "deep" || !req.Force {
		return false
	}
	var output struct {
		ContinuousPrepared bool                  `json:"continuous_prepared"`
		ExecutionID        string                `json:"execution_id"`
		TargetLocator      string                `json:"target_locator"`
		RefGroups          []models.ScanRefGroup `json:"ref_groups"`
	}
	encoded, err := json.Marshal(parent.Metadata["outputs"])
	if err != nil || json.Unmarshal(encoded, &output) != nil || output.ExecutionID != parent.ExecutionID {
		return false
	}
	if parent.ExecutionBoundary != execution.ExecutionBoundaryBounded && (parent.ExecutionBoundary != execution.ExecutionBoundaryContinuous || !output.ContinuousPrepared) {
		return false
	}
	locator, err := resourcetree.ParseURI(output.TargetLocator)
	if err != nil || locator.EngineID != req.EngineID || len(locator.Path) == 0 {
		return false
	}
	if len(output.RefGroups) > 0 {
		return len(req.Targets) == 0 && reflect.DeepEqual(output.RefGroups, req.RefGroups)
	}
	return len(req.RefGroups) == 0 && len(req.Targets) == 1 && req.Targets[0] == output.TargetLocator
}

func matchesManagerProducedTarget(parent execution.TaskExecution, req *models.ScanRequest) bool {
	if parent.TaskType == execution.TaskTypeRasterMosaicGeneration {
		return matchesManagerMosaicProducedTarget(parent, req)
	}
	if parent.TaskType != execution.TaskTypeVectorTileSetGeneration || req.EngineID == 0 || req.NodeID != 0 || req.ItemID != 0 || len(req.CatalogPaths) != 0 || len(req.Targets) != 0 || len(req.RefGroups) != 1 || len(req.RefGroups[0].Refs) != 0 || req.ScanDepth != "deep" || !req.Force {
		return false
	}
	var target commonclient.MetaScanProducedTarget
	if !decodeMosaicFact(parent.Metadata["produced_target"], &target) || target.EngineID != req.EngineID || target.Attempt != parent.Attempt || target.ManifestRef != "" {
		return false
	}
	return target.CatalogPath != "" && target.CatalogPath != "." && !path.IsAbs(target.CatalogPath) && !strings.HasPrefix(target.CatalogPath, "../") && !strings.Contains(target.CatalogPath, "\\") && path.Clean(target.CatalogPath) == target.CatalogPath && parent.Metadata["catalog_path"] == target.CatalogPath && req.RefGroups[0].Primary == target.CatalogPath
}

func inheritAppliedSchemaChangeActor(tx *gorm.DB, child *execution.TaskExecution, req *models.ScanRequest) error {
	ref := req.SchemaChangeSource
	id, err := uuid.Parse(ref.ExecutionID)
	if err != nil || id == uuid.Nil || id.String() != ref.ExecutionID || ref.RequestID == 0 || ref.ClaimToken == "" {
		return commonapi.ErrBadRequest
	}
	var historical execution.TaskExecution
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id = ? AND execution_id = ? AND module = ? AND task_type = ? AND execution_boundary = ? AND status = ?", child.TenantID, ref.ExecutionID, execution.ModuleTransfer, execution.TaskTypeSync, execution.ExecutionBoundaryContinuous, execution.ExecutionStatusFailed).First(&historical).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return commonapi.ErrNotFound
		}
		return err
	}
	var fact execution.AppliedSchemaChangeScanOrigin
	encoded, err := json.Marshal(historical.Metadata[execution.SchemaChangeScanOriginKey])
	if err != nil || json.Unmarshal(encoded, &fact) != nil {
		return commonapi.ErrNotFound
	}
	now := time.Now().UTC()
	if tx.Dialector.Name() == "postgres" {
		if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			return err
		}
	}
	if fact.RequestID != ref.RequestID || fact.TaskID == 0 || historical.SourceTaskID == nil || *historical.SourceTaskID != strconv.FormatUint(uint64(fact.TaskID), 10) || fact.Generation == 0 || fact.FromRevision == 0 || fact.ToRevision != fact.FromRevision+1 || fact.AppliedAt.IsZero() || fact.AppliedAt.After(now) || fact.Attempt == 0 || fact.ClaimToken != ref.ClaimToken || fact.LeaseUntil == nil || !fact.LeaseUntil.After(now) || historical.Metadata["stop_reason"] != "schema_change_blocked" {
		return commonapi.ErrNotFound
	}
	continuous, _ := historical.Metadata["continuous"].(map[string]interface{})
	var projection struct {
		RequestID    uint   `json:"request_id"`
		Status       string `json:"status"`
		Generation   uint64 `json:"generation"`
		FromRevision uint64 `json:"from_revision"`
		ToRevision   uint64 `json:"to_revision"`
		MetadataScan struct {
			Status  string `json:"status"`
			Attempt uint64 `json:"attempt"`
		} `json:"metadata_scan"`
	}
	encoded, err = json.Marshal(continuous["schema_change"])
	if err != nil || json.Unmarshal(encoded, &projection) != nil || projection.Status != "applied" || projection.MetadataScan.Status != "running" || projection.MetadataScan.Attempt != fact.Attempt || projection.RequestID != fact.RequestID || projection.Generation != fact.Generation || projection.FromRevision != fact.FromRevision || projection.ToRevision != fact.ToRevision {
		return commonapi.ErrNotFound
	}
	locator, err := resourcetree.ParseURI(fact.TargetLocator)
	if err != nil || locator.Type != resourcetree.TypeTable || len(locator.Path) < 2 || locator.EngineID != req.EngineID || len(req.Targets) != 1 || req.Targets[0] != fact.TargetLocator {
		return commonapi.ErrBadRequest
	}
	source := execution.TaskExecution{TenantID: child.TenantID, TriggeredBy: &fact.TriggeredBy, ActorPrincipalID: &fact.ActorPrincipalID, ActorTenantMembershipID: &fact.ActorTenantMembershipID, IssuedAuthorizationVersion: &fact.IssuedAuthorizationVersion}
	if err := execution.CopyActorOrigin(child, &source); err != nil {
		return commonapi.ErrNotFound
	}
	if child.Metadata == nil {
		child.Metadata = map[string]interface{}{}
	}
	child.Metadata["schema_change_source"] = map[string]interface{}{"execution_id": ref.ExecutionID, "request_id": ref.RequestID}
	return nil
}
