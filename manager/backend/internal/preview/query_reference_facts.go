package preview

import (
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

// The locator identifies an item, not its verified physical SQL name. Resolve
// the latter from Meta's ordered, tenant-bound ancestor names without splitting
// either the caller path or the flattened display full_name.
func queryCatalogPathFromMeta(req *PreviewResolverRequest, facts *models.MetaItemAncestors) (plugin.EngineCatalogPath, error) {
	fail := func() (plugin.EngineCatalogPath, error) {
		return plugin.EngineCatalogPath{}, ErrPreviewRequiresScannedMeta
	}
	if req == nil || req.Engine == nil || req.MetaItemID == nil || req.TenantID == nil || facts == nil {
		return fail()
	}
	item := facts.Item
	if item.ID != *req.MetaItemID || item.EngineID != req.Engine.ID || item.TenantID != *req.TenantID || item.Name != req.ItemName || item.FullName != req.ItemFullName || item.ItemType != req.ItemType {
		return fail()
	}
	registered, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return fail()
	}
	provider, ok := registered.(plugin.EngineCatalogModelProvider)
	if !ok || len(facts.Ancestors) > 128 {
		return fail()
	}
	names := make([]string, 0, len(facts.Ancestors)+1)
	seen := map[uint]bool{}
	var parent uint
	for i, node := range facts.Ancestors {
		if node.ID == 0 || seen[node.ID] || node.EngineID != item.EngineID || node.TenantID != item.TenantID {
			return fail()
		}
		if i == 0 {
			if node.ParentNodeID != nil {
				return fail()
			}
		} else if node.ParentNodeID == nil || *node.ParentNodeID != parent {
			return fail()
		}
		seen[node.ID], parent = true, node.ID
		if i == 0 && node.FullName == "" {
			continue
		} // structural root has no business name
		if node.Name == "" {
			return fail()
		}
		names = append(names, node.Name)
	}
	if item.NodeID != parent || item.Name == "" {
		return fail()
	}
	names = append(names, item.Name)
	return resourcetree.EngineCatalogPathFromLocator(provider.EngineCatalogModel(), &resourcetree.ResourceLocator{EngineID: item.EngineID, Type: resourcetree.ResourceType(item.ItemType), Path: names})
}
