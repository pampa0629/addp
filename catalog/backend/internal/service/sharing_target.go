package service

import (
	"context"
	"net/http"
	"slices"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

type SharingTargetResolver interface {
	ResolveSharingTarget(context.Context, int64, string) (plugin.EngineCatalogPath, error)
}

type sharingTargetResolver struct {
	meta   *commonClient.MetaClient
	system *commonClient.SystemServiceClient
}

func NewSharingTargetResolver(meta *commonClient.MetaClient, system *commonClient.SystemServiceClient) SharingTargetResolver {
	return &sharingTargetResolver{meta: meta, system: system}
}

func (r *sharingTargetResolver) ResolveSharingTarget(ctx context.Context, tenantID int64, fingerprint string) (plugin.EngineCatalogPath, error) {
	if r == nil || r.meta == nil || r.system == nil || tenantID <= 0 {
		return plugin.EngineCatalogPath{}, ErrReferenceValidationUnavailable
	}
	if !authorizationfacts.ValidItemFingerprint(fingerprint) {
		return plugin.EngineCatalogPath{}, ErrSharingTargetUnsupported
	}
	// Meta alone establishes the DataItem boundary. Use Catalog's own tenant
	// Service credential, outside its business transaction; never reconstruct
	// an authorization target from ancestry, row IDs or presentation names.
	facts, err := r.meta.ReadAuthorizationFacts(ctx, uint(tenantID), fingerprint)
	if err != nil {
		if status, ok := commonClient.TenantAPIStatusCode(err); ok && status == http.StatusNotFound {
			return plugin.EngineCatalogPath{}, ErrSharingTargetUnsupported
		}
		return plugin.EngineCatalogPath{}, ErrReferenceValidationUnavailable
	}
	if facts == nil || facts.Validate() != nil || facts.ItemFingerprint != fingerprint {
		return plugin.EngineCatalogPath{}, ErrReferenceValidationUnavailable
	}
	if facts.BasisState != authorizationfacts.AuthorizationBasisComplete {
		return plugin.EngineCatalogPath{}, ErrSharingTargetUnsupported
	}
	// This remains a resource observation, not a Grant. System still owns the
	// current Engine lifecycle and subsequent independent access adjudication.
	descriptor, err := r.system.WithTenantID(uint(tenantID)).GetEngineRuntimeDescriptor(ctx, facts.EngineID)
	if err != nil {
		return plugin.EngineCatalogPath{}, ErrReferenceValidationUnavailable
	}
	if descriptor == nil || descriptor.ID != facts.EngineID || descriptor.LifecycleState != "active" || descriptor.Capabilities == nil {
		return plugin.EngineCatalogPath{}, ErrSharingTargetUnsupported
	}
	path := facts.Boundary.NativeTarget
	caps, err := plugin.ParseEngineCapabilities(string(*descriptor.Capabilities))
	if err != nil || caps == nil || caps.Storage == nil || caps.Storage.CatalogModel == nil {
		return plugin.EngineCatalogPath{}, ErrSharingTargetUnsupported
	}
	// Retain the current Engine model check without deriving Meta's boundary.
	model := caps.Storage.CatalogModel
	if model.PathVersion != path.Version || model.RootTerm != path.Segments[0].Term || len(model.Levels) != len(path.Segments)-1 {
		return plugin.EngineCatalogPath{}, ErrSharingTargetUnsupported
	}
	for i, level := range model.Levels {
		segment := path.Segments[i+1]
		role := plugin.EngineCatalogRoleBranch
		if i == len(model.Levels)-1 {
			role = plugin.EngineCatalogRoleLeaf
		}
		if level.Term != segment.Term || level.Role != role || !slices.Contains(level.Kinds, segment.Kind) {
			return plugin.EngineCatalogPath{}, ErrSharingTargetUnsupported
		}
	}
	return path, nil
}
