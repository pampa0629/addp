package preview

import (
	"context"
	"reflect"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/jsonmap"
)

// PGReadBindings reuses the actual item resolved in this request. Only missing
// dependencies are queried, together, through Meta's formal fact contract.
// Neither a locator nor a prepared path alone is an authorization fact.
func (r *PreviewResolver) PGReadBindings(ctx context.Context, req *PreviewResolverRequest, paths []plugin.EngineCatalogPath) ([]authorizationfacts.Binding, error) {
	if req == nil || req.Engine == nil || req.Engine.EngineType != "postgresql" || req.TenantID == nil || *req.TenantID == 0 || req.MetaItem == nil || req.MetaItem.TenantID != *req.TenantID {
		return nil, ErrPreviewRequiresScannedMeta
	}
	primaryPath := plugin.TabularItemPath(req.Engine.ID, plugin.EngineCatalogTermSchema, jsonmap.String(req.MetaItem.Attributes, "storage", "schema_name"), req.MetaItem.Name)
	primary, err := authorizationfacts.NativeTableBindingFromMeta(*req.MetaItem, primaryPath)
	if err != nil {
		return nil, ErrPreviewRequiresScannedMeta
	}
	bindings := make([]authorizationfacts.Binding, len(paths))
	missing := []string{}
	positions := []int{}
	for i, path := range paths {
		if path.EngineID != req.Engine.ID || len(path.Segments) != 3 || path.Segments[1].Term != plugin.EngineCatalogTermSchema || path.Segments[2].Term != plugin.EngineCatalogTermTable {
			return nil, ErrPreviewRequiresScannedMeta
		}
		if reflect.DeepEqual(path, primary.NativeTarget) {
			bindings[i] = primary
			continue
		}
		fingerprint, err := authorizationfacts.NativeCandidateFingerprint(path)
		if err != nil {
			return nil, ErrPreviewRequiresScannedMeta
		}
		missing = append(missing, fingerprint)
		positions = append(positions, i)
	}
	if len(missing) > 0 {
		if r == nil || r.metaClient == nil {
			return nil, ErrPreviewRequiresScannedMeta
		}
		batch, err := r.metaClient.ReadAuthorizationFacts(ctx, *req.TenantID, missing)
		if err != nil {
			return nil, previewMetadataLookupError(err)
		}
		for i, row := range batch.Data {
			if row.Facts == nil {
				return nil, ErrPreviewRequiresScannedMeta
			}
			binding, err := row.Facts.ProveNativeTarget(paths[positions[i]])
			if err != nil {
				return nil, ErrPreviewRequiresScannedMeta
			}
			bindings[positions[i]] = binding
		}
	}
	return bindings, nil
}
