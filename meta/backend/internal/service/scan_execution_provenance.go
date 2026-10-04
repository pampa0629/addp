package service

import (
	"encoding/json"
	"errors"
	"strings"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// inheritDevelopProducedTargetActor runs only in the scan creation transaction.
// Caller authentication is enforced at the HTTP boundary; this verifies durable
// provenance and copies identity facts, never an execution authorization grant.
func inheritDevelopProducedTargetActor(tx *gorm.DB, child *execution.TaskExecution, req *models.ScanRequest) error {
	if req.Source != commonclient.MetaScanSourceDevelopProducedTarget && req.ParentExecutionID == "" {
		return nil
	}
	if req.Source != commonclient.MetaScanSourceDevelopProducedTarget {
		return commonapi.ErrBadRequest
	}
	parentID, err := uuid.Parse(req.ParentExecutionID)
	if err != nil || parentID == uuid.Nil {
		return commonapi.ErrBadRequest
	}
	var parent execution.TaskExecution
	err = tx.Clauses(clause.Locking{Strength: "SHARE"}).Where(
		"tenant_id = ? AND execution_id = ? AND module = ? AND task_type = ? AND status = ?",
		child.TenantID, parentID.String(), execution.ModuleDevelop, execution.TaskTypeWorkflow, execution.ExecutionStatusRunning,
	).First(&parent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return commonapi.ErrNotFound
	}
	if err != nil {
		return err
	}
	if parent.ActorPrincipalID == nil || *parent.ActorPrincipalID <= 0 ||
		parent.ActorTenantMembershipID == nil || *parent.ActorTenantMembershipID <= 0 ||
		parent.IssuedAuthorizationVersion == nil || *parent.IssuedAuthorizationVersion <= 0 ||
		parent.TriggeredBy == nil || int64(*parent.TriggeredBy) != *parent.ActorPrincipalID {
		return commonapi.ErrNotFound
	}
	if !matchesDevelopProducedTarget(parent, req) {
		return commonapi.ErrNotFound
	}
	principal, membership, version := *parent.ActorPrincipalID, *parent.ActorTenantMembershipID, *parent.IssuedAuthorizationVersion
	triggeredBy, id := *parent.TriggeredBy, parentID.String()
	child.ParentExecutionID = &id
	child.TriggeredBy = &triggeredBy
	child.ActorPrincipalID = &principal
	child.ActorTenantMembershipID = &membership
	child.IssuedAuthorizationVersion = &version
	return nil
}

func matchesDevelopProducedTarget(parent execution.TaskExecution, req *models.ScanRequest) bool {
	if req.EngineID == 0 || req.NodeID != 0 || req.ItemID != 0 || len(req.CatalogPaths) != 0 || req.ScanDepth != "deep" || !req.Force {
		return false
	}
	// outputs is the existing stable result contract, rather than free-form
	// source strings or client-declared user / authorization fields.
	var outputs map[string]struct {
		Resource struct {
			Locator string `json:"locator"`
		} `json:"resource"`
	}
	encoded, err := json.Marshal(parent.Metadata["outputs"])
	if err != nil || json.Unmarshal(encoded, &outputs) != nil {
		return false
	}
	for _, output := range outputs {
		locator, err := resourcetree.ParseURI(output.Resource.Locator)
		if err != nil || locator.EngineID != req.EngineID || len(locator.Path) == 0 {
			continue
		}
		if locator.Type == resourcetree.TypeFile {
			if len(req.Targets) == 0 && len(req.RefGroups) == 1 && len(req.RefGroups[0].Refs) == 0 && req.RefGroups[0].Primary == strings.Join(locator.Path, "/") {
				return true
			}
		} else if len(req.RefGroups) == 0 && len(req.Targets) == 1 && req.Targets[0] == output.Resource.Locator {
			return true
		}
	}
	return false
}
