package resourcetree

import (
	"fmt"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

// CatalogPathFromMetaItem resolves physical names only from tenant-bound Owner
// facts. Locator paths and flattened display names are never name authorities.
func CatalogPathFromMetaItem(model plugin.EngineCatalogModelSpec, engineID, tenantID, itemID uint, facts *models.MetaItemAncestors) (plugin.EngineCatalogPath, error) {
	fail := func() (plugin.EngineCatalogPath, error) {
		return plugin.EngineCatalogPath{}, fmt.Errorf("invalid Meta item ancestor facts")
	}
	if facts == nil || engineID == 0 || tenantID == 0 || itemID == 0 {
		return fail()
	}
	item := facts.Item
	if item.ID != itemID || item.EngineID != engineID || item.TenantID != tenantID || len(facts.Ancestors) > 128 {
		return fail()
	}
	names := make([]string, 0, len(facts.Ancestors)+1)
	seen := map[uint]bool{}
	var parent uint
	for i, node := range facts.Ancestors {
		if node.ID == 0 || seen[node.ID] || node.EngineID != engineID || node.TenantID != tenantID {
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
		}
		if node.Name == "" {
			return fail()
		}
		names = append(names, node.Name)
	}
	if item.NodeID != parent || item.Name == "" {
		return fail()
	}
	names = append(names, item.Name)
	return EngineCatalogPathFromLocator(model, &ResourceLocator{EngineID: engineID, Type: ResourceType(item.ItemType), Path: names})
}
