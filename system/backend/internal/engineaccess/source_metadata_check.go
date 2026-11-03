package engineaccess

import (
	"context"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"strings"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/system/internal/iam"
)

const MetaCatalogReadPermission = "meta.catalog.read"
const MetaLineageReadPermission = "meta.lineage.read"

func qualifyMetaMetadataRead(current shared.AuthContext) error {
	if current.Token.Type == "delegated_access_token" {
		if current.Principal.Type != "user" || current.Context.Type != "tenant" ||
			!shared.HasContextPermissions(current, MetaCatalogReadPermission) ||
			len(current.Client.Audiences) != 1 || current.Client.Audiences[0] != "meta" ||
			current.Client.ScopeMode != "restricted" || len(current.Client.Scopes) != 1 ||
			(current.Client.Scopes[0] != "resource.children.list" && current.Client.Scopes[0] != "resource.ancestors.get") ||
			current.Delegation == nil || current.Client.ClientID == nil ||
			current.Delegation.DelegatedByClientID != *current.Client.ClientID ||
			current.Delegation.AgentRunID == "" || current.Delegation.ToolCallID == "" {
			return commonapi.ErrForbidden
		}
		return nil
	}
	return qualifyOrdinarySourceUser(current, MetaCatalogReadPermission)
}

func (s *Service) CheckMetaMetadataRead(ctx context.Context, credential string, request shared.SourceMetadataCheckRequest) (*shared.SourceMetadataCheck, error) {
	return s.checkMetaVisibility(ctx, credential, request, qualifyMetaMetadataRead)
}

func qualifyMetaLineageRead(current shared.AuthContext) error {
	return qualifyOrdinarySourceUser(current, MetaLineageReadPermission)
}

func (s *Service) CheckMetaLineageRead(ctx context.Context, credential string, request shared.SourceMetadataCheckRequest) (*shared.SourceMetadataCheck, error) {
	return s.checkMetaVisibility(ctx, credential, request, qualifyMetaLineageRead)
}

func (s *Service) checkMetaVisibility(ctx context.Context, credential string, request shared.SourceMetadataCheckRequest, qualify func(shared.AuthContext) error) (*shared.SourceMetadataCheck, error) {
	if !request.Valid() {
		return nil, commonapi.ErrBadRequest
	}
	if s == nil || s.repository == nil || s.repository.db == nil {
		return nil, errSourceReadRules
	}
	resolve := func(auth *iam.AuthContextService) (*shared.AuthContext, error) {
		if strings.HasPrefix(credential, "addp_dat_") {
			return auth.ResolveDelegatedAccessToken(ctx, credential)
		}
		return auth.ResolveUserAccessToken(ctx, credential)
	}
	tenant, err := s.repository.resolveSourceCredentialTenant(ctx, resolve, qualify)
	if err != nil {
		return nil, err
	}
	seenEngines := map[uint]bool{}
	for _, target := range request.Targets {
		if seenEngines[target.EngineID] {
			continue
		}
		if err := s.repository.checkSourceAccessAction(ctx, tenant, int64(target.EngineID), shared.SourceAccessMetadataRead); err != nil {
			return nil, err
		}
		seenEngines[target.EngineID] = true
	}
	paths, _, err := encodeSourceReadTargets(request.Targets)
	if err != nil {
		return nil, err
	}
	// Missing, changed or unsupported identities are invisible independently.
	// They cannot expose their existence or deny the rest of a candidate batch.
	result := &shared.SourceMetadataCheck{Targets: make([]shared.SourceMetadataTargetCheck, len(paths))}
	knownPaths := []plugin.EngineCatalogPath{}
	bindings := []authorizationfacts.Binding{}
	positions := []int{}
	resolved, err := s.resolveMetadataBindings(ctx, tenant, paths)
	if err != nil {
		return nil, err
	}
	if len(resolved) != len(paths) {
		return nil, errSourceReadRules
	}
	for index, path := range paths {
		result.Targets[index].Target = path
		if resolved[index].ItemFingerprint == "" {
			continue
		}
		knownPaths = append(knownPaths, path)
		bindings = append(bindings, resolved[index])
		positions = append(positions, index)
	}
	if len(knownPaths) == 0 {
		currentTenant, err := s.repository.resolveSourceCredentialTenant(ctx, resolve, qualify)
		if err != nil {
			return nil, err
		}
		if currentTenant != tenant {
			return nil, commonapi.ErrForbidden
		}
		result.ObservedAt, err = s.repository.wallClock(ctx)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	observation, err := s.repository.observeSourceRules(ctx, credential, shared.SourceAccessMetadataRead,
		knownPaths, bindings, tenant, resolve, qualify, nil)
	if err != nil {
		return nil, err
	}
	if observation == nil || observation.ObservedAt.IsZero() || len(observation.Targets) != len(knownPaths) {
		return nil, errSourceReadRules
	}
	result.ObservedAt = observation.ObservedAt
	for index, rule := range observation.Targets {
		result.Targets[positions[index]].Visible = rule.Covered
	}
	return result, nil
}
