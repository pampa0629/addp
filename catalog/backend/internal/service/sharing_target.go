package service

import (
	"context"
	"fmt"
	"slices"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

type SharingTargetResolver interface {
	ResolveSharingTarget(context.Context, int64, int64, string) (plugin.EngineCatalogPath, error)
}

type sharingTargetResolver struct {
	meta   *commonClient.MetaClient
	system *commonClient.SystemServiceClient
}

func NewSharingTargetResolver(meta *commonClient.MetaClient, system *commonClient.SystemServiceClient) SharingTargetResolver {
	return &sharingTargetResolver{meta: meta, system: system}
}

func (r *sharingTargetResolver) ResolveSharingTarget(ctx context.Context, tenantID, itemID int64, fingerprint string) (plugin.EngineCatalogPath, error) {
	if r == nil || r.meta == nil || r.system == nil || tenantID <= 0 || itemID <= 0 {
		return plugin.EngineCatalogPath{}, ErrReferenceValidationUnavailable
	}
	// Existing tenant-scoped Meta metadata read: no source connection or sample.
	facts, err := r.meta.WithTenantID(uint(tenantID)).GetItemAncestors(uint(itemID))
	if err != nil {
		return plugin.EngineCatalogPath{}, fmt.Errorf("%w: Meta current ancestry: %v", ErrReferenceValidationUnavailable, err)
	}
	if facts == nil || facts.Item.ID != uint(itemID) || facts.Item.TenantID != uint(tenantID) || facts.Item.Fingerprint != fingerprint {
		return plugin.EngineCatalogPath{}, ErrSharingTargetUnsupported
	}
	descriptor, err := r.system.WithTenantID(uint(tenantID)).GetEngineRuntimeDescriptor(ctx, facts.Item.EngineID)
	if err != nil {
		return plugin.EngineCatalogPath{}, fmt.Errorf("%w: System engine descriptor: %v", ErrReferenceValidationUnavailable, err)
	}
	if descriptor == nil || descriptor.ID != facts.Item.EngineID || descriptor.LifecycleState != "active" || descriptor.Capabilities == nil {
		return plugin.EngineCatalogPath{}, ErrSharingTargetUnsupported
	}
	caps, err := plugin.ParseEngineCapabilities(string(*descriptor.Capabilities))
	if err != nil || caps == nil || caps.Storage == nil || caps.Storage.CatalogModel == nil {
		return plugin.EngineCatalogPath{}, ErrSharingTargetUnsupported
	}
	return databaseSharingPath(tenantID, fingerprint, facts, *caps.Storage.CatalogModel)
}

// First source-read vertical slice: declared database namespaces and table leaf.
// Meta ancestry provides exact names; never split a presentation full_name.
func databaseSharingPath(tenantID int64, fingerprint string, facts *commonModels.MetaItemAncestors, model plugin.EngineCatalogModelSpec) (plugin.EngineCatalogPath, error) {
	invalid := func() (plugin.EngineCatalogPath, error) {
		return plugin.EngineCatalogPath{}, ErrSharingTargetUnsupported
	}
	if tenantID <= 0 || facts == nil || facts.Item.ID == 0 || facts.Item.TenantID != uint(tenantID) || facts.Item.EngineID == 0 || facts.Item.ItemType != "table" ||
		facts.Item.Fingerprint != fingerprint || model.PathVersion != plugin.EngineCatalogPathVersion || model.RootTerm != plugin.EngineCatalogTermServer ||
		len(model.Levels) < 2 || len(model.Levels) > 128 || len(facts.Ancestors) != len(model.Levels) {
		return invalid()
	}
	root := facts.Ancestors[0]
	if root.ID == 0 || root.EngineID != facts.Item.EngineID || root.TenantID != uint(tenantID) || root.ParentNodeID != nil || root.FullName != "" || root.NodeType != model.RootTerm {
		return invalid()
	}
	names := make([]string, 0, len(model.Levels))
	seen := map[uint]bool{root.ID: true}
	for i, node := range facts.Ancestors[1:] {
		level := model.Levels[i]
		if node.ID == 0 || seen[node.ID] || level.Role != plugin.EngineCatalogRoleBranch || len(level.Kinds) != 1 || level.Term != node.NodeType || node.EngineID != facts.Item.EngineID ||
			node.TenantID != uint(tenantID) || node.ParentNodeID == nil || *node.ParentNodeID != facts.Ancestors[i].ID {
			return invalid()
		}
		seen[node.ID] = true
		names = append(names, node.Name)
	}
	leaf := model.Levels[len(model.Levels)-1]
	if leaf.Role != plugin.EngineCatalogRoleLeaf || leaf.Term != "table" || !slices.Contains(leaf.Kinds, facts.Item.ItemType) ||
		facts.Item.NodeID != facts.Ancestors[len(facts.Ancestors)-1].ID {
		return invalid()
	}
	names = append(names, facts.Item.Name)
	// Check Meta's declared logical identity without normalizing source names.
	fullName := strings.Join(names, ".")
	if facts.Item.FullName != fullName || commonModels.GenerateItemFingerprint(facts.Item.EngineID, fullName) != fingerprint {
		return invalid()
	}
	// The shared converter owns typed catalog path construction and name bounds.
	path, err := resourcetree.EngineCatalogPathFromLocator(model, &resourcetree.ResourceLocator{
		EngineID: facts.Item.EngineID, Type: resourcetree.TypeTable, Path: names,
	})
	if err != nil {
		return invalid()
	}
	return path, nil
}
