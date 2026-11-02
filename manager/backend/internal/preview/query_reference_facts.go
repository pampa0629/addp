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
	if !ok {
		return fail()
	}
	path, err := resourcetree.CatalogPathFromMetaItem(provider.EngineCatalogModel(), item.EngineID, item.TenantID, item.ID, facts)
	if err != nil {
		return fail()
	}
	return path, nil
}
