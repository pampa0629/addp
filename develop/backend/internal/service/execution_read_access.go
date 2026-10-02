package service

import (
	"context"
	"strconv"

	"github.com/addp/common/authorization"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/taskprovider"
	developauthorization "github.com/addp/develop/backend/internal/authorization"
	"github.com/addp/develop/backend/internal/models"
)

type executionAuthContextKey struct{}
type exportResourceReadKey struct{}

// WithExportResourceRead is installed only after the route-bound Resource Ticket guard.
func WithExportResourceRead(ctx context.Context) context.Context {
	return context.WithValue(ctx, exportResourceReadKey{}, true)
}

// WithExecutionAuthContext carries detached, verified identity facts in the
// current request only. No bearer credential is retained in this context.
func WithExecutionAuthContext(ctx context.Context, facts authorization.AuthContext) context.Context {
	if authorization.ValidateAuthContext(facts) != nil {
		return ctx
	}
	return context.WithValue(ctx, executionAuthContextKey{}, authorization.CloneAuthContext(facts))
}

func executionUserFacts(ctx context.Context) (authorization.AuthContext, bool) {
	facts, ok := ctx.Value(executionAuthContextKey{}).(authorization.AuthContext)
	if !ok || facts.Principal.Type != "user" || facts.Context.Type != "tenant" {
		return facts, false
	}
	switch facts.Token.Type {
	case "resource_access_ticket":
		allowed, _ := ctx.Value(exportResourceReadKey{}).(bool)
		return facts, allowed
	case "first_party_access_token", "oauth_access_token", "delegated_access_token":
		return facts, true
	default:
		return facts, false
	}
}

func executionActorIDs(facts authorization.AuthContext) (int, int64, int64, int64) {
	if facts.Context.TenantID == nil || facts.Context.TenantMembershipID == nil {
		return 0, 0, 0, 0
	}
	tenant, _ := strconv.Atoi(*facts.Context.TenantID)
	principal, _ := strconv.ParseInt(facts.Principal.ID, 10, 64)
	membership, _ := strconv.ParseInt(*facts.Context.TenantMembershipID, 10, 64)
	version, _ := strconv.ParseInt(facts.Authorization.AuthorizationVersion, 10, 64)
	return tenant, principal, membership, version
}

func (e *DevExecutor) professionalExecutionRepository(ctx context.Context, tenantID uint) (*commonExecution.TaskExecutionRepository, context.Context) {
	facts, ok := executionUserFacts(ctx)
	tenant, principal, membership, version := executionActorIDs(facts)
	if !ok || tenant != int(tenantID) {
		tenant, principal, membership, version = 0, 0, 0, 0
	}
	scope := commonExecution.ReadScope{Module: commonExecution.ModuleDevelop, TenantID: tenant, PrincipalID: principal, Grants: []commonExecution.ReadGrant{}}
	if ok && authorization.HasContextPermissions(facts, developauthorization.PermissionDevelopTaskRead) {
		if authorization.HasContextPermissions(facts, developauthorization.PermissionDevelopDataReadExecute) {
			for _, taskType := range []string{commonExecution.TaskTypeQuery, commonExecution.TaskTypeWorkflow} {
				scope.Grants = append(scope.Grants, commonExecution.ReadGrant{TaskType: taskType, TaskHistory: true, OwnAdHoc: true})
			}
		}
		if authorization.HasContextPermissions(facts, developauthorization.PermissionDevelopNotebookRead) {
			scope.Grants = append(scope.Grants, commonExecution.ReadGrant{TaskType: commonExecution.TaskTypeScript, TaskHistory: true, OwnAdHoc: true})
		}
	}
	return e.taskExecutionRepo.ForCurrentActor(tenant, principal, membership, version), commonExecution.WithReadScopes(ctx, []commonExecution.ReadScope{scope})
}

// Script does not create a data-engine Execution Authorization at admission;
// persist its origin from the same verified User request instead.
func applyScriptExecutionActor(ctx context.Context, execution *commonExecution.TaskExecution) {
	if execution.TaskType != commonExecution.TaskTypeScript {
		return
	}
	facts, ok := executionUserFacts(ctx)
	tenant, principal, membership, version := executionActorIDs(facts)
	if !ok || tenant != execution.TenantID || principal <= 0 || membership <= 0 || version <= 0 || execution.TriggeredBy == nil || int64(*execution.TriggeredBy) != principal {
		return
	}
	execution.ActorPrincipalID = &principal
	execution.ActorTenantMembershipID = &membership
	execution.IssuedAuthorizationVersion = &version
}

func professionalExecutionResponse(execution *commonExecution.TaskExecution) *models.ExecutionWithDevTask {
	response := &models.ExecutionWithDevTask{Observation: commonExecution.Observe(execution), ExecutionConfig: execution.ExecutionConfig, Outputs: taskprovider.ExecutionOutputs(execution.Metadata)}
	if result, ok := execution.Metadata["result"]; ok {
		if response.Metadata == nil {
			response.Metadata = map[string]interface{}{}
		}
		response.Metadata["result"] = result
	}
	return response
}
