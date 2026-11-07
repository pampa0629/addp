package service

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/resourcetree"
	"github.com/addp/service/internal/models"
)

type queryProtectionUserKey struct{}
type ontologyFactRolesKey struct{}
type queryProtectionUser struct {
	facts  authorization.AuthContext
	bearer string
}

// WithQueryProtectionUser carries verified request-local facts only. It is never persisted.
func WithQueryProtectionUser(ctx context.Context, facts authorization.AuthContext, bearer string) context.Context {
	return context.WithValue(ctx, queryProtectionUserKey{}, queryProtectionUser{authorization.CloneAuthContext(facts), bearer})
}

// WithOntologyFactRoles carries the System observation from the one-time claim.
func WithOntologyFactRoles(ctx context.Context, roles *authorization.CurrentSubjectRoles) context.Context {
	return context.WithValue(ctx, ontologyFactRolesKey{}, roles)
}

type queryRoleGate interface {
	RoleOptions(context.Context, int64, dataprotection.ResourceReference, []string) ([]dataprotection.FieldRoleOptions, error)
	ValidateRoleChoices(context.Context, int64, *dataprotection.QueryRoleSelection) error
}

func (s *QueryExecutorService) currentProtectionRoles(ctx context.Context, tenant uint) (*authorization.CurrentSubjectRoles, error) {
	if roles, ok := ctx.Value(ontologyFactRolesKey{}).(*authorization.CurrentSubjectRoles); ok {
		if roles == nil || roles.TenantID != strconv.FormatUint(uint64(tenant), 10) || !roles.ExpiresAt.After(time.Now()) {
			return nil, dataprotection.ErrRoleOptionIneligible
		}
		return roles, nil
	}
	user, ok := ctx.Value(queryProtectionUserKey{}).(queryProtectionUser)
	if !ok || user.facts.Context.TenantID == nil || *user.facts.Context.TenantID != strconv.FormatUint(uint64(tenant), 10) || s == nil || s.systemService == nil {
		return nil, dataprotection.ErrRoleOptionIneligible
	}
	roles, err := s.systemService.ResolveCurrentSubjectRoles(ctx, user.bearer, &user.facts)
	if err != nil {
		return nil, errors.Join(dataprotection.ErrRoleOptionIneligible, err)
	}
	return roles, nil
}

func (s *QueryExecutorService) QueryProtectionOptions(ctx context.Context, tenant uint, request *models.QueryProtectionOptionsRequest) (*models.QueryProtectionOptionsResponse, error) {
	roles, err := s.currentProtectionRoles(ctx, tenant)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	var sources map[dataprotection.ResourceReference]map[string][]string
	switch request.ConfigType {
	case "table":
		if request.EngineID != 0 || request.SQL != "" || request.Parameters != nil {
			return nil, dataprotection.ErrRoleOptionUnavailable
		}
		parsed, err := resourcetree.ParseURI(strings.TrimSpace(request.TargetLocator))
		if err != nil || parsed.EngineID == 0 || parsed.ItemID == nil || *parsed.ItemID == 0 {
			return nil, dataprotection.ErrRoleOptionUnavailable
		}
		descriptor, err := s.systemService.WithTenantID(tenant).GetEngineRuntimeDescriptor(ctx, parsed.EngineID)
		if err != nil {
			return nil, err
		}
		model, err := dbbridge.EngineCatalogModel(descriptor.EngineType)
		if err != nil {
			return nil, dataprotection.ErrRoleOptionUnavailable
		}
		path, err := resourcetree.EngineCatalogPathFromLocator(model, parsed)
		if err != nil {
			return nil, dataprotection.ErrRoleOptionUnavailable
		}
		target, err := dataprotection.DataItemTargetFromCatalogPath(model, path)
		if err != nil {
			return nil, dataprotection.ErrRoleOptionUnavailable
		}
		sources = map[dataprotection.ResourceReference]map[string][]string{target: nil}
	case "sql":
		if request.TargetLocator != "" {
			return nil, dataprotection.ErrRoleOptionUnavailable
		}
		source, err := s.prepareRoleSQL(ctx, tenant, request.EngineID, request.SQL, request.Parameters)
		if err != nil {
			return nil, err
		}
		sources = source.targets
	default:
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	gate, ok := s.protectionGate.(queryRoleGate)
	if !ok {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	fields := []models.QueryProtectionFieldOptions{}
	for target, eligible := range sources {
		candidates, err := gate.RoleOptions(ctx, int64(tenant), target, roles.RoleKeys)
		if err != nil {
			return nil, err
		}
		for _, field := range candidates {
			outputs, selected := eligible[field.ComponentKey]
			if eligible != nil && !selected {
				continue
			}
			candidate := models.QueryProtectionFieldOptions{FieldRoleOptions: field}
			if len(sources) > 1 {
				candidate.OutputFields = slices.Clone(outputs)
				sort.Strings(candidate.OutputFields)
			}
			fields = append(fields, candidate)
		}
	}
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].ComponentKey != fields[j].ComponentKey {
			return fields[i].ComponentKey < fields[j].ComponentKey
		}
		return fields[i].Target.ResourceIdentity < fields[j].Target.ResourceIdentity
	})
	if !roles.ExpiresAt.After(time.Now().UTC()) {
		return nil, dataprotection.ErrRoleOptionIneligible
	}
	return &models.QueryProtectionOptionsResponse{Fields: fields}, nil
}

func (s *QueryExecutorService) serviceRoleSelection(ctx context.Context, service *models.QueryService) (*dataprotection.QueryRoleSelection, error) {
	choices, err := service.ProtectionChoices()
	if err != nil || len(choices) == 0 {
		return nil, err
	}
	source, err := s.definitionRoleSource(ctx, service)
	if err != nil {
		return nil, err
	}
	selection, err := s.roleSelectionForSource(ctx, service, source)
	if err != nil {
		return nil, err
	}
	if service.ConfigType == "sql" {
		// Reuse the actual protection compiler for schema/consumption proof;
		// do not execute this metadata-only plan or persist a parallel snapshot.
		_, end, err := s.protectionGate.BeginPreparedQuery(ctx, service.TenantID, source.engine, source.prepared, selection)
		if err != nil {
			return nil, err
		}
		end()
	}
	return selection, nil
}

func (s *QueryExecutorService) roleSelectionForSource(ctx context.Context, service *models.QueryService, source *queryRoleSource) (*dataprotection.QueryRoleSelection, error) {
	choices, err := service.ProtectionChoices()
	if err != nil || len(choices) == 0 {
		return nil, err
	}
	if source == nil {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	for _, choice := range choices {
		if _, ok := source.targets[choice.Target][choice.ComponentKey]; !ok {
			return nil, dataprotection.ErrRoleOptionUnavailable
		}
	}
	roles, err := s.currentProtectionRoles(ctx, service.TenantID)
	if err != nil {
		return nil, err
	}
	selection := &dataprotection.QueryRoleSelection{Choices: choices, RoleKeys: roles.RoleKeys, ExpiresAt: roles.ExpiresAt}
	gate, ok := s.protectionGate.(queryRoleGate)
	if !ok {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	if err := gate.ValidateRoleChoices(ctx, int64(service.TenantID), selection); err != nil {
		return nil, err
	}
	return selection, nil
}

func (s *QueryServiceService) validateProtectionChoices(ctx context.Context, service *models.QueryService) error {
	choices, err := service.ProtectionChoices()
	if err != nil || len(choices) == 0 {
		return err
	}
	if s.roleProtection == nil {
		return dataprotection.ErrRoleOptionUnavailable
	}
	_, err = s.roleProtection.serviceRoleSelection(ctx, service)
	return err
}
