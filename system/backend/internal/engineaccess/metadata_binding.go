package engineaccess

import (
	"context"
	"github.com/addp/common/client"
	"net/http"
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
	result := make([]authorizationfacts.Binding, 0, len(paths))
	for _, target := range paths {
		fingerprint, err := authorizationfacts.PrimaryCandidateFingerprint(target)
		if err != nil {
			return nil, ErrAuthorizationBoundaryChanged
		}
		facts, err := s.authorizationFacts.ReadAuthorizationFacts(ctx, uint(tenant), fingerprint)
		if status, ok := client.TenantAPIStatusCode(err); ok && status == http.StatusNotFound {
			return nil, ErrAuthorizationBoundaryChanged
		}
		if err != nil || facts == nil || facts.Validate() != nil || facts.ItemFingerprint != fingerprint {
			return nil, ErrAuthorizationFactsUnavailable
		}
		binding, err := facts.ProveMetadataTarget(target)
		if err != nil {
			return nil, ErrAuthorizationBoundaryChanged
		}
		engine, err := s.repository.engine(ctx, tenant, int64(target.EngineID), false)
		if err != nil {
			return nil, err
		}
		// Every representation must match the actual registered catalog model.
		// Metadata admission never depends on a content reader or source IO.
		model, err := dbbridge.EngineCatalogModel(engine.EngineType)
		if err != nil {
			return nil, ErrAuthorizationBoundaryChanged
		}
		names := make([]string, 0, len(target.Segments)-1)
		for _, segment := range target.Segments[1:] {
			names = append(names, segment.Name)
		}
		canonical, err := resourcetree.EngineCatalogPathFromLocator(model, &resourcetree.ResourceLocator{EngineID: target.EngineID, Type: resourcetree.ResourceType(target.Segments[len(target.Segments)-1].Kind), Path: names})
		if err != nil || !reflect.DeepEqual(canonical, target) {
			return nil, ErrAuthorizationBoundaryChanged
		}
		result = append(result, binding)
	}
	return result, nil
}
