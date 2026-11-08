package engineaccess

import (
	"context"
	"encoding/json"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
)

const ManagerSearchPermission = "manager.search.execute"

func qualifyManagerSearchRead(current shared.AuthContext) error {
	if current.Token.Type != "delegated_access_token" {
		return qualifyOrdinarySourceUser(current, ManagerSearchPermission)
	}
	if current.Principal.Type != "user" || current.Context.Type != "tenant" ||
		!shared.HasContextPermissions(current, ManagerSearchPermission) ||
		len(current.Client.Audiences) != 1 || current.Client.Audiences[0] != "manager" ||
		current.Client.ScopeMode != "restricted" || len(current.Client.Scopes) != 1 || current.Client.Scopes[0] != "data.search" ||
		current.Delegation == nil || current.Client.ClientID == nil || current.Delegation.DelegatedByClientID != *current.Client.ClientID ||
		current.Delegation.AgentRunID == "" || current.Delegation.ToolCallID == "" {
		return commonapi.ErrForbidden
	}
	return nil
}

func (s *Service) checkManagerSearchTargets(ctx context.Context, credential string, tenant int64, request shared.SourceMetadataCheckRequest) (*shared.SourceSearchCheck, error) {
	if s == nil || s.repository == nil || s.repository.db == nil {
		return nil, errSourceReadRules
	}
	timing := managerSearchTiming(ctx)
	if timing != nil {
		timing.candidates = len(request.Targets)
	}
	resolve := func(auth *iam.AuthContextService) (*shared.AuthContext, error) {
		return resolveSearchCredential(ctx, auth, credential)
	}
	started := time.Now()
	if len(request.Targets) == 0 {
		currentTenant, err := s.repository.resolveSourceCredentialTenant(ctx, resolve, qualifyManagerSearchRead)
		if err != nil {
			return nil, err
		}
		if currentTenant != tenant {
			return nil, commonapi.ErrForbidden
		}
		now, err := s.repository.wallClock(ctx)
		return &shared.SourceSearchCheck{ObservedAt: now, Targets: []shared.SourceSearchTargetCheck{}}, err
	}
	started = time.Now()
	bindings, err := s.resolveMetadataBindings(ctx, tenant, request.Targets)
	if timing != nil {
		timing.metadataBinding = time.Since(started)
	}
	if err != nil {
		return nil, err
	}
	started = time.Now()
	result := &shared.SourceSearchCheck{Targets: make([]shared.SourceSearchTargetCheck, len(bindings))}
	var paths []plugin.EngineCatalogPath
	var rules []sourceRuleTarget
	metadataPositions := make([]int, len(bindings))
	contentPositions := make([]int, len(bindings))
	for i, binding := range bindings {
		path := request.Targets[i]
		result.Targets[i].Target = path
		metadataPositions[i], contentPositions[i] = -1, -1
		if binding.ItemFingerprint == "" {
			continue
		}
		metadataPositions[i] = len(rules)
		paths = append(paths, path)
		rules = append(rules, sourceRuleTarget{Binding: binding, Action: shared.SourceAccessMetadataRead})
		if binding.MetadataTarget != nil {
			continue
		}
		if binding.Scope != nil {
			if timing != nil {
				timing.wholeCandidates++
			}
			engine, err := s.repository.metadataBindingEngine(ctx, tenant, int64(path.EngineID))
			if err != nil {
				return nil, err
			}
			if engine.EngineType != "nfs" {
				continue
			}
		}
		contentPositions[i] = len(rules)
		paths = append(paths, path)
		rules = append(rules, sourceRuleTarget{Binding: binding, Action: shared.SourceAccessRead})
	}
	if len(rules) == 0 {
		if timing != nil {
			timing.rulePreparation = time.Since(started)
		}
		started = time.Now()
		currentTenant, err := s.repository.resolveSourceCredentialTenant(ctx, resolve, qualifyManagerSearchRead)
		if timing != nil {
			timing.observedCredential = time.Since(started)
		}
		if err != nil || tenant != currentTenant {
			if err == nil {
				err = commonapi.ErrForbidden
			}
			return nil, err
		}
		result.ObservedAt, err = s.repository.wallClock(ctx)
		if timing != nil {
			timing.ruleObservation = time.Since(started)
		}
		return result, err
	}
	batch, err := json.Marshal(rules)
	if timing != nil {
		timing.rulePreparation = time.Since(started)
		timing.ruleItems = len(rules)
	}
	if err != nil {
		return nil, err
	}
	started = time.Now()
	observation, err := s.repository.observePreparedSourceRules(ctx, shared.SourceAccessMetadataRead, paths, nil, batch, tenant, resolve, qualifyManagerSearchRead, nil)
	if timing != nil {
		timing.ruleObservation = time.Since(started)
	}
	if err != nil {
		return nil, err
	}
	result.ObservedAt = observation.ObservedAt
	for i := range result.Targets {
		metadata, content := metadataPositions[i], contentPositions[i]
		result.Targets[i].Visible = metadata >= 0 && observation.Targets[metadata].Covered
		result.Targets[i].ContentReadable = result.Targets[i].Visible && content >= 0 && observation.Targets[content].Covered
		if result.Targets[i].Visible {
			result.Targets[i].ItemFingerprint = bindings[i].ItemFingerprint
		}
	}
	return result, nil
}
