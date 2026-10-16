package service

import (
	"encoding/json"
	"errors"
	"path"
	"slices"
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
	if req.Source != commonclient.MetaScanSourceDevelopProducedTarget && req.Source != commonclient.MetaScanSourceManagerMosaicProducedTarget && req.ParentExecutionID == "" {
		return nil
	}
	parent, err := verifiedProducedTargetParent(tx, child.TenantID, req)
	if err != nil {
		return err
	}
	if req.Source == commonclient.MetaScanSourceManagerMosaicProducedTarget {
		if child.Metadata == nil {
			child.Metadata = map[string]interface{}{}
		}
		child.Metadata["produced_target"] = parent.Metadata["produced_target"]
	}
	principal, membership, version := *parent.ActorPrincipalID, *parent.ActorTenantMembershipID, *parent.IssuedAuthorizationVersion
	triggeredBy, id := *parent.TriggeredBy, parent.ExecutionID
	child.ParentExecutionID = &id
	child.TriggeredBy = &triggeredBy
	child.ActorPrincipalID = &principal
	child.ActorTenantMembershipID = &membership
	child.IssuedAuthorizationVersion = &version
	return nil
}

// Caller holds the returned parent's shared lock until its transaction ends.
func verifiedProducedTargetParent(tx *gorm.DB, tenantID int, req *models.ScanRequest) (*execution.TaskExecution, error) {
	module, taskType := execution.ModuleDevelop, execution.TaskTypeWorkflow
	if req.Source == commonclient.MetaScanSourceManagerMosaicProducedTarget {
		module, taskType = execution.ModuleManager, execution.TaskTypeRasterMosaicGeneration
	} else if req.Source != commonclient.MetaScanSourceDevelopProducedTarget {
		return nil, commonapi.ErrBadRequest
	}
	parentID, err := uuid.Parse(req.ParentExecutionID)
	if err != nil || parentID == uuid.Nil {
		return nil, commonapi.ErrBadRequest
	}
	var parent execution.TaskExecution
	err = tx.Clauses(clause.Locking{Strength: "SHARE"}).Where(
		"tenant_id = ? AND execution_id = ? AND module = ? AND task_type = ? AND status = ?",
		tenantID, parentID.String(), module, taskType, execution.ExecutionStatusRunning,
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
		now := time.Now().UTC()
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
				return nil, err
			}
		}
		if parent.ExecutionBoundary != execution.ExecutionBoundaryBounded || parent.Attempt <= 0 || parent.LeaseToken == nil || *parent.LeaseToken == "" || parent.LeaseOwner == nil || *parent.LeaseOwner == "" || parent.LeaseExpiresAt == nil || !parent.LeaseExpiresAt.After(now) || !matchesManagerMosaicProducedTarget(parent, req) {
			return nil, commonapi.ErrNotFound
		}
	} else if !matchesDevelopProducedTarget(parent, req) {
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
