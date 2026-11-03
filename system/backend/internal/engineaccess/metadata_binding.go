package engineaccess

import (
	"context"
	"reflect"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resourcetree"
)

// Same trusted Meta facts and current source rule store as content checks.
// The metadata-only representation can never enter resolveReadBindings.
func (s *Service) resolveMetadataBindings(ctx context.Context, tenant int64, paths []plugin.EngineCatalogPath) ([]authorizationfacts.Binding, error) {
	if s == nil || s.authorizationFacts == nil || tenant <= 0 {
		return nil, ErrAuthorizationFactsUnavailable
	}
	result := make([]authorizationfacts.Binding, len(paths))
	fingerprints := []string{}
	keys := make([]string, len(paths))
	seen := map[string]bool{}
	for i, target := range paths {
		fingerprint, err := authorizationfacts.PrimaryCandidateFingerprint(target)
		if err != nil {
			continue
		}
		keys[i] = fingerprint
		if !seen[fingerprint] {
			fingerprints = append(fingerprints, fingerprint)
			seen[fingerprint] = true
		}
	}
	factsByKey := map[string]*authorizationfacts.AuthorizationFacts{}
	for offset := 0; offset < len(fingerprints); offset += authorizationfacts.MaxAuthorizationFactsQueryItems {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		end := offset + authorizationfacts.MaxAuthorizationFactsQueryItems
		if end > len(fingerprints) {
			end = len(fingerprints)
		}
		query := authorizationfacts.AuthorizationFactsQuery{Fingerprints: fingerprints[offset:end]}
		batch, err := s.authorizationFacts.ReadAuthorizationFacts(ctx, uint(tenant), query.Fingerprints)
		if err != nil || batch.Validate(query) != nil {
			return nil, ErrAuthorizationFactsUnavailable
		}
		for _, entry := range batch.Data {
			factsByKey[entry.ItemFingerprint] = entry.Facts
		}
	}
	catalogModels := map[uint]plugin.EngineCatalogModelSpec{}
	for index, target := range paths {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		facts := factsByKey[keys[index]]
		if facts == nil {
			continue
		}
		binding, err := facts.ProveMetadataTarget(target)
		if err != nil {
			continue
		}
		// The registered model is a technical fact shared only within this
		// request. Every target still proves its own exact typed identity;
		// current source rules are observed separately after these bindings.
		model, ok := catalogModels[target.EngineID]
		if !ok {
			engine, err := s.repository.engine(ctx, tenant, int64(target.EngineID), false)
			if err != nil {
				return nil, err
			}
			model, err = dbbridge.EngineCatalogModel(engine.EngineType)
			if err != nil {
				continue
			}
			catalogModels[target.EngineID] = model
		}
		names := make([]string, 0, len(target.Segments)-1)
		for _, segment := range target.Segments[1:] {
			names = append(names, segment.Name)
		}
		canonical, err := resourcetree.EngineCatalogPathFromLocator(model, &resourcetree.ResourceLocator{EngineID: target.EngineID, Type: resourcetree.ResourceType(target.Segments[len(target.Segments)-1].Kind), Path: names})
		if err != nil || !reflect.DeepEqual(canonical, target) {
			continue
		}
		result[index] = binding
	}
	return result, nil
}
