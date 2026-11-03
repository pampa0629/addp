package service

import (
	"context"
	"errors"
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
type queryProtectionUser struct {
	facts  authorization.AuthContext
	bearer string
}

// WithQueryProtectionUser carries verified request-local facts only. It is never persisted.
func WithQueryProtectionUser(ctx context.Context, facts authorization.AuthContext, bearer string) context.Context {
	return context.WithValue(ctx, queryProtectionUserKey{}, queryProtectionUser{authorization.CloneAuthContext(facts), bearer})
}

type queryRoleGate interface {
	RoleOptions(context.Context, int64, dataprotection.ResourceReference, []string) ([]dataprotection.FieldRoleOptions, error)
	ValidateRoleChoices(context.Context, int64, *dataprotection.QueryRoleSelection) error
}

func (s *QueryExecutorService) currentProtectionRoles(ctx context.Context, tenant uint) (*authorization.CurrentSubjectRoles, error) {
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
	var target dataprotection.ResourceReference
	var sqlSource *queryRoleSource
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
		target, err = dataprotection.DataItemTargetFromCatalogPath(model, path)
		if err != nil {
			return nil, dataprotection.ErrRoleOptionUnavailable
		}
	case "sql":
		if request.TargetLocator != "" {
			return nil, dataprotection.ErrRoleOptionUnavailable
		}
		source, err := s.prepareRoleSQL(ctx, tenant, request.EngineID, request.SQL, request.Parameters)
		if err != nil {
			return nil, err
		}
		target = source.target
		sqlSource = source
	default:
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	gate, ok := s.protectionGate.(queryRoleGate)
	if !ok {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	fields, err := gate.RoleOptions(ctx, int64(tenant), target, roles.RoleKeys)
	if err != nil {
		return nil, err
	}
	if sqlSource != nil {
		eligible := make([]dataprotection.FieldRoleOptions, 0, len(fields))
		for _, field := range fields {
			if sqlSource.fields[field.ComponentKey] {
				eligible = append(eligible, field)
			}
		}
		fields = eligible
	}
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
		if choice.Target != source.target || !source.fields[choice.ComponentKey] {
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
