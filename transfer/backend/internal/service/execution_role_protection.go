package service

import (
	"context"
	"strconv"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
)

func roleExecutionAuthorization(record *commonExecution.TaskExecution) (*commonClient.IssuedExecutionAuthorization, error) {
	if record == nil || record.TriggerType != "manual" || record.ParentExecutionID != nil || record.ExecutionAuthorizationID == nil || record.ActorPrincipalID == nil || record.ActorTenantMembershipID == nil || record.IssuedAuthorizationVersion == nil || record.AuthorizationExpiresAt == nil {
		return nil, dataprotection.ErrRoleOptionIneligible
	}
	issued := &commonClient.IssuedExecutionAuthorization{ID: strconv.FormatInt(*record.ExecutionAuthorizationID, 10), Audience: "transfer", ExecutionID: record.ExecutionID, TenantID: strconv.Itoa(record.TenantID), ActorPrincipalID: strconv.FormatInt(*record.ActorPrincipalID, 10), TenantMembershipID: strconv.FormatInt(*record.ActorTenantMembershipID, 10), IssuedAuthorizationVersion: strconv.FormatInt(*record.IssuedAuthorizationVersion, 10), SourceType: "user", ExpiresAt: *record.AuthorizationExpiresAt}
	if record.TriggeredBy == nil {
		return nil, dataprotection.ErrRoleOptionIneligible
	}
	return issued, commonClient.ValidateTransferUserAuthorization(issued, uint(record.TenantID), uint(*record.TriggeredBy), record.ExecutionID)
}
func (s *ExecutionEngineService) executionRoleResolver(record *commonExecution.TaskExecution, config map[string]interface{}) func(context.Context) (*dataprotection.QueryRoleSelection, error) {
	return func(ctx context.Context) (*dataprotection.QueryRoleSelection, error) {
		choices, err := models.SourceProtectionChoices(config)
		if err != nil || len(choices) == 0 {
			return nil, err
		}
		issued, err := roleExecutionAuthorization(record)
		if err != nil {
			return nil, err
		}
		lease, ok := commonExecution.LeaseFromContext(ctx)
		if !ok || lease.ExecutionID != record.ExecutionID || lease.TenantID != record.TenantID || s.systemRuntime == nil {
			return nil, dataprotection.ErrRoleOptionIneligible
		}
		roles, err := s.systemRuntime.WithTenantID(uint(record.TenantID)).ResolveExecutionSubjectRoles(ctx, issued, "transfer", commonClient.ExecutionSubjectRolesRequest{ExecutionID: record.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token})
		if err != nil {
			return nil, dataprotection.ErrRoleOptionIneligible
		}
		return &dataprotection.QueryRoleSelection{Choices: choices, RoleKeys: roles.RoleKeys, ExpiresAt: roles.ExpiresAt}, nil
	}
}

// Role-specialized reads use the exact user's EA for source and target engine
// access, rather than acquiring credentials under a worker's general identity.
func (s *ExecutionEngineService) roleEngineResolver(ctx context.Context, record *commonExecution.TaskExecution, spec planner.TableExportTaskSpec) (planner.EngineResolver, error) {
	issued, err := roleExecutionAuthorization(record)
	if err != nil {
		return nil, err
	}
	scopes, err := roleTransferAccesses(spec)
	if err != nil || s.systemRuntime == nil {
		return nil, dataprotection.ErrRoleOptionIneligible
	}
	resolver := planner.StaticEngineResolver{}
	for _, scope := range scopes {
		access, err := s.systemRuntime.WithTenantID(uint(record.TenantID)).GetExecutionEngineAccess(ctx, issued.ID, commonClient.ExecutionEngineAccessRequest{ExecutionID: record.ExecutionID, EngineID: scope.EngineID, RequiredEffects: scope.Effects})
		if err != nil {
			return nil, dataprotection.ErrRoleOptionIneligible
		}
		binding, err := planner.EngineBindingFromEngine(access.Engine)
		if err != nil {
			return nil, err
		}
		id, _ := strconv.ParseUint(scope.EngineID, 10, 32)
		resolver[uint(id)] = binding
	}
	return planner.NewHybridEngineResolver(resolver, s.infraEngineResolver()), nil
}
