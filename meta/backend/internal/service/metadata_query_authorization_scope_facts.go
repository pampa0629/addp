package service

import (
	"errors"
	"path"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonjson "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func projectWholeContainerAuthorizationFacts(tx *gorm.DB, item models.MetaItem, facts *authorizationfacts.AuthorizationFacts) error {
	finish := func(state authorizationfacts.AuthorizationBasisState) error {
		facts.BasisState = state
		return facts.Validate()
	}
	if item.ItemType != "file" || commonjson.String(item.Attributes, "item", "format") != "filegdb" ||
		commonjson.String(item.Attributes, "item", "data_type") != "container" {
		return finish(authorizationfacts.AuthorizationBasisUnsupported)
	}
	identity := commonjson.Section(item.Attributes, "item")
	if identity["scope_exclusive"] != true || identity["claim_policy"] != "whole_scope" {
		return finish(authorizationfacts.AuthorizationBasisIncomplete)
	}
	// A whole FileGDB cannot import an external composition into its scope.
	if _, exists := identity["refs"]; exists {
		return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
	}
	root := plugin.FileDirectoryPath(item.EngineID, item.FullName)
	scope := &authorizationfacts.ScopeBoundary{Format: "filegdb", RuleVersion: 1, Root: root}
	if scope.Validate(item.EngineID) != nil || root.StringPath() != item.FullName || path.Base(item.FullName) != item.Name ||
		commonjson.String(item.Attributes, "storage", "physical_path") != item.FullName {
		return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
	}
	// Whole items belong to their scope node, not to the directory's parent.
	var node models.MetaNode
	err := tx.Where("tenant_id = ? AND engine_id = ? AND id = ? AND deleted_at IS NULL", item.TenantID, item.EngineID, item.NodeID).First(&node).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return finish(authorizationfacts.AuthorizationBasisIncomplete)
	}
	if err != nil {
		return err
	}
	if node.NodeType != "dir" || node.FullName != item.FullName || node.Name != item.Name || node.ParentNodeID == nil {
		return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
	}
	parentItem := item
	parentItem.NodeID = *node.ParentNodeID
	state, err := validateFileAuthorizationParents(tx, parentItem)
	if err != nil {
		return err
	}
	if state != authorizationfacts.AuthorizationBasisComplete {
		return finish(state)
	}
	facts.Boundary = &authorizationfacts.AuthorizationBoundary{Layout: "whole", Scope: scope}
	hash, err := facts.ComputeBoundaryHash()
	if err != nil {
		return err
	}
	facts.BoundaryHash = &hash
	return finish(authorizationfacts.AuthorizationBasisComplete)
}
