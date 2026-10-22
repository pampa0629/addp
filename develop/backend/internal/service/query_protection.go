package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dbbridge"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/resourcetree"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/protection"
)

type roleChoiceGate interface {
	RoleOptions(context.Context, int64, dataprotection.ResourceReference, []string) ([]dataprotection.FieldRoleOptions, error)
	ValidateRoleChoices(context.Context, int64, *dataprotection.QueryRoleSelection) error
}

// currentQueryRoles takes no subject selectors; facts and bearer must be this request's current User.
func (s *SQLEngineService) currentQueryRoles(ctx context.Context, tenant uint, bearer string) (*authorization.CurrentSubjectRoles, error) {
	facts, ok := executionUserFacts(ctx)
	currentTenant, _, _, _ := executionActorIDs(facts)
	if !ok || currentTenant != int(tenant) || s == nil || s.systemService == nil {
		return nil, dataprotection.ErrRoleOptionIneligible
	}
	roles, err := s.systemService.ResolveCurrentSubjectRoles(ctx, bearer, &facts)
	if err != nil {
		return nil, errors.Join(dataprotection.ErrRoleOptionIneligible, err)
	}
	return roles, nil
}

func (s *SQLEngineService) QueryProtectionOptions(ctx context.Context, tenant uint, bearer string, engineID uint, locator string) (*models.QueryProtectionOptionsResponse, error) {
	roles, err := s.currentQueryRoles(ctx, tenant, bearer)
	if err != nil {
		return nil, err
	}
	target, err := s.queryProtectionTarget(ctx, tenant, engineID, locator)
	if err != nil {
		return nil, err
	}
	gate, ok := s.protectionGate.(roleChoiceGate)
	if !ok {
		return nil, protection.ErrRequired
	}
	fields, err := gate.RoleOptions(ctx, int64(tenant), target, roles.RoleKeys)
	if err != nil {
		return nil, err
	}
	if !roles.ExpiresAt.After(time.Now().UTC()) {
		return nil, dataprotection.ErrRoleOptionIneligible
	}
	return &models.QueryProtectionOptionsResponse{Fields: fields}, nil
}

func (s *SQLEngineService) queryProtectionTarget(ctx context.Context, tenant, engineID uint, locator string) (dataprotection.ResourceReference, error) {
	invalid := dataprotection.ErrRoleOptionUnavailable
	parsed, err := resourcetree.ParseURI(strings.TrimSpace(locator))
	if err != nil || parsed.EngineID != engineID || engineID == 0 || s == nil || s.systemService == nil {
		return dataprotection.ResourceReference{}, invalid
	}
	descriptor, err := s.systemService.WithTenantID(tenant).GetEngineRuntimeDescriptor(ctx, engineID)
	if err != nil {
		return dataprotection.ResourceReference{}, invalid
	}
	model, err := dbbridge.EngineCatalogModel(descriptor.EngineType)
	if err != nil {
		return dataprotection.ResourceReference{}, invalid
	}
	path, err := resourcetree.EngineCatalogPathFromLocator(model, parsed)
	if err != nil {
		return dataprotection.ResourceReference{}, invalid
	}
	target, err := dataprotection.DataItemTargetFromCatalogPath(model, path)
	if err != nil {
		return dataprotection.ResourceReference{}, invalid
	}
	return target, nil
}

func (s *SQLEngineService) ValidateQueryProtectionChoices(ctx context.Context, tenant, user uint, bearer string, task *models.DevTask) error {
	choices, err := models.QueryProtectionChoices(task.Content)
	if err != nil {
		return err
	}
	if len(choices) == 0 {
		return nil
	}
	if task.DevType != "query" {
		return dataprotection.ErrRoleOptionUnavailable
	}
	facts, ok := executionUserFacts(ctx)
	if !ok || facts.Principal.ID != strconv.FormatUint(uint64(user), 10) {
		return dataprotection.ErrRoleOptionIneligible
	}
	roles, err := s.currentQueryRoles(ctx, tenant, bearer)
	if err != nil {
		return err
	}
	engine := task.GetEngineID()
	if engine == nil {
		return dataprotection.ErrRoleOptionUnavailable
	}
	locator, _ := task.Content["target_locator"].(string)
	target, err := s.queryProtectionTarget(ctx, tenant, *engine, locator)
	if err != nil {
		return err
	}
	for _, choice := range choices {
		if choice.Target != target {
			return dataprotection.ErrRoleOptionUnavailable
		}
	}
	gate, ok := s.protectionGate.(roleChoiceGate)
	if !ok {
		return protection.ErrRequired
	}
	return gate.ValidateRoleChoices(ctx, int64(tenant), &dataprotection.QueryRoleSelection{Choices: choices, RoleKeys: roles.RoleKeys, ExpiresAt: roles.ExpiresAt})
}

func (s *SQLEngineService) executionQueryRoles(ctx context.Context, execution *commonExecution.TaskExecution, lease commonExecution.Lease, issued *IssuedSQLExecutionAuthorization, task *models.DevTask) (*dataprotection.QueryRoleSelection, error) {
	choices, err := models.QueryProtectionChoices(task.Content)
	if err != nil || len(choices) == 0 {
		return nil, err
	}
	if s == nil || s.systemService == nil || issued == nil || len(issued.Effects) != 1 || issued.Effects[0] != SQLExecutionEffectRead || task.GetQueryType() != "sql" && task.GetQueryType() != "mql" {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	audience := commonExecution.AudienceDevelop
	roles, err := s.systemService.WithTenantID(uint(execution.TenantID)).ResolveExecutionSubjectRoles(ctx, &commonClient.IssuedExecutionAuthorization{
		ID: strconv.FormatInt(issued.AuthorizationID, 10), ExecutionID: execution.ExecutionID, Audience: audience, SourceType: "user", TenantID: strconv.Itoa(execution.TenantID),
		ActorPrincipalID: strconv.FormatInt(issued.ActorPrincipalID, 10), TenantMembershipID: strconv.FormatInt(issued.ActorTenantMembershipID, 10), IssuedAuthorizationVersion: strconv.FormatInt(issued.IssuedAuthorizationVersion, 10), ExpiresAt: issued.ExpiresAt,
	}, "develop", commonClient.ExecutionSubjectRolesRequest{ExecutionID: execution.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token})
	if err != nil {
		return nil, errors.Join(protection.ErrRequired, dataprotection.ErrRoleOptionIneligible)
	}
	return &dataprotection.QueryRoleSelection{Choices: choices, RoleKeys: roles.RoleKeys, ExpiresAt: roles.ExpiresAt}, nil
}
