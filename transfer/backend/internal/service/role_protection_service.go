package service

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dbbridge"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/resourcetree"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
)

type transferRoleGate interface {
	RoleOptions(context.Context, int64, dataprotection.ResourceReference, string, []string, time.Time) ([]dataprotection.FieldRoleOptions, error)
	ValidateRoleChoices(context.Context, int64, *dataprotection.QueryRoleSelection, string, time.Time) error
}
type transferAuthorizationIssuer interface {
	Issue(context.Context, string, commonClient.IssueExecutionAuthorizationRequest) (*commonClient.IssuedExecutionAuthorization, error)
}
type RoleProtectionService struct {
	system *commonClient.SystemServiceClient
	issuer transferAuthorizationIssuer
	gate   transferRoleGate
}

func NewRoleProtectionService(system *commonClient.SystemServiceClient, issuer transferAuthorizationIssuer, gate transferRoleGate) *RoleProtectionService {
	return &RoleProtectionService{system, issuer, gate}
}

type protectionAuthContextKey struct{}

func WithProtectionAuthContext(ctx context.Context, facts authorization.AuthContext) context.Context {
	return context.WithValue(ctx, protectionAuthContextKey{}, facts)
}
func (s *RoleProtectionService) current(ctx context.Context, tenant uint) (*authorization.CurrentSubjectRoles, error) {
	facts, ok := ctx.Value(protectionAuthContextKey{}).(authorization.AuthContext)
	token, _ := ctx.Value(taskAuthorizationTokenKey{}).(string)
	if !ok || s == nil || s.system == nil || facts.Context.TenantID == nil || *facts.Context.TenantID != strconv.FormatUint(uint64(tenant), 10) {
		return nil, dataprotection.ErrRoleOptionIneligible
	}
	roles, err := s.system.ResolveCurrentSubjectRoles(ctx, token, &facts)
	if err != nil {
		return nil, errors.Join(dataprotection.ErrRoleOptionIneligible, err)
	}
	return roles, nil
}
func (s *RoleProtectionService) target(ctx context.Context, tenant uint, uri string) (dataprotection.ResourceReference, error) {
	invalid := dataprotection.ErrRoleOptionUnavailable
	locator, err := resourcetree.ParseURI(strings.TrimSpace(uri))
	if err != nil || locator.EngineID == 0 {
		return dataprotection.ResourceReference{}, invalid
	}
	descriptor, err := s.system.WithTenantID(tenant).GetEngineRuntimeDescriptor(ctx, locator.EngineID)
	if err != nil {
		return dataprotection.ResourceReference{}, invalid
	}
	model, err := dbbridge.EngineCatalogModel(descriptor.EngineType)
	if err != nil {
		return dataprotection.ResourceReference{}, invalid
	}
	path, err := resourcetree.EngineCatalogPathFromLocator(model, locator)
	if err != nil {
		return dataprotection.ResourceReference{}, invalid
	}
	target, err := dataprotection.DataItemTargetFromCatalogPath(model, path)
	if err != nil {
		return target, invalid
	}
	return target, nil
}
func (s *RoleProtectionService) Options(ctx context.Context, tenant uint, locator string) (*commonClient.TransferProtectionOptions, error) {
	roles, err := s.current(ctx, tenant)
	if err != nil {
		return nil, err
	}
	target, err := s.target(ctx, tenant, locator)
	if err != nil {
		return nil, err
	}
	if s.gate == nil {
		return nil, dataprotection.ErrNotReady
	}
	fields, err := s.gate.RoleOptions(ctx, int64(tenant), target, "export", roles.RoleKeys, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if !roles.ExpiresAt.After(time.Now().UTC()) {
		return nil, dataprotection.ErrRoleOptionIneligible
	}
	return &commonClient.TransferProtectionOptions{Fields: fields}, nil
}
func (s *RoleProtectionService) Validate(ctx context.Context, tenant uint, request *commonClient.TransferSourceProtectionSelection) error {
	if request == nil || len(request.Choices) == 0 || dataprotection.ValidateFieldRoleChoices(request.Choices) != nil {
		return dataprotection.ErrRoleOptionUnavailable
	}
	roles, err := s.current(ctx, tenant)
	if err != nil {
		return err
	}
	target, err := s.target(ctx, tenant, request.SourceLocator)
	if err != nil {
		return err
	}
	for _, choice := range request.Choices {
		if choice.Target != target {
			return dataprotection.ErrRoleOptionUnavailable
		}
	}
	if s.gate == nil {
		return dataprotection.ErrNotReady
	}
	return s.gate.ValidateRoleChoices(ctx, int64(tenant), &dataprotection.QueryRoleSelection{Choices: request.Choices, RoleKeys: roles.RoleKeys, ExpiresAt: roles.ExpiresAt}, "export", time.Now().UTC())
}
func (s *TaskService) SetRoleProtection(service *RoleProtectionService) { s.roleProtection = service }
func (s *TaskService) validateRoleTask(ctx context.Context, task *models.TransferTask) error {
	choices, err := models.SourceProtectionChoices(task.Config)
	if err != nil || len(choices) == 0 {
		return err
	}
	if task.Schedule != "" || task.Enabled || s.roleProtection == nil {
		return dataprotection.ErrRoleOptionUnavailable
	}
	source, _ := task.Config["source"].(map[string]interface{})
	locator, _ := source["locator"].(string)
	return s.roleProtection.Validate(ctx, task.TenantID, &commonClient.TransferSourceProtectionSelection{SourceLocator: locator, Choices: choices})
}
func (s *TaskService) authorizeRoleExecution(ctx context.Context, task *models.TransferTask, record *commonExecution.TaskExecution) error {
	choices, err := models.SourceProtectionChoices(task.Config)
	if err != nil || len(choices) == 0 {
		return err
	}
	if record.ParentExecutionID != nil || record.TriggerType != "manual" || record.Source != "transfer" {
		return dataprotection.ErrRoleOptionIneligible
	}
	if err := s.validateRoleTask(ctx, task); err != nil {
		return err
	}
	if err := commonExecution.ApplyUserOrigin(ctx, record); err != nil {
		return err
	}
	spec, err := planner.ParseTableExportTaskSpec(task.Config, task.BatchSize)
	if err != nil {
		return dataprotection.ErrRoleOptionUnavailable
	}
	accesses, err := roleTransferAccesses(spec)
	if err != nil {
		return err
	}
	token, _ := ctx.Value(taskAuthorizationTokenKey{}).(string)
	if s.roleProtection.issuer == nil {
		return dataprotection.ErrRoleOptionIneligible
	}
	issued, err := s.roleProtection.issuer.Issue(ctx, token, commonClient.IssueExecutionAuthorizationRequest{Audience: "transfer", ExecutionID: record.ExecutionID, Accesses: accesses, ExpiresIn: 3600})
	if err != nil {
		if status, ok := commonClient.SystemAPIStatusCode(err); ok && status == 403 {
			return commonAPI.ErrForbidden
		} else if ok && status == 401 {
			return commonAPI.ErrUnauthorized
		}
		return ErrTaskAuthorizationUnavailable
	}
	return attachRoleAuthorization(record, issued)
}
func attachRoleAuthorization(record *commonExecution.TaskExecution, issued *commonClient.IssuedExecutionAuthorization) error {
	if record == nil || record.TriggeredBy == nil || *record.TriggeredBy <= 0 {
		return dataprotection.ErrRoleOptionIneligible
	}
	if err := commonClient.ValidateTransferUserAuthorization(issued, uint(record.TenantID), uint(*record.TriggeredBy), record.ExecutionID); err != nil {
		return err
	}
	fields, err := commonClient.TaskExecutionAuthorizationFields(issued)
	if err != nil {
		return dataprotection.ErrRoleOptionIneligible
	}
	principal := fields["actor_principal_id"].(*int64)
	member := fields["actor_tenant_membership_id"].(*int64)
	version := fields["issued_authorization_version"].(*int64)
	if record.ActorPrincipalID != nil && (*record.ActorPrincipalID != *principal || record.ActorTenantMembershipID == nil || *record.ActorTenantMembershipID != *member || record.IssuedAuthorizationVersion == nil || *record.IssuedAuthorizationVersion != *version) {
		return dataprotection.ErrRoleOptionIneligible
	}
	record.ExecutionAuthorizationID = fields["execution_authorization_id"].(*int64)
	record.ActorPrincipalID = principal
	record.ActorTenantMembershipID = member
	record.IssuedAuthorizationVersion = version
	expiry := issued.ExpiresAt.UTC()
	record.AuthorizationExpiresAt = &expiry
	return nil
}
func roleTransferAccesses(spec planner.TableExportTaskSpec) ([]commonClient.ExecutionEngineAccessScope, error) {
	source, err := spec.Source.EngineRef()
	if err != nil {
		return nil, err
	}
	scopes := map[uint][]string{source.ID: {"read"}}
	if !planner.IsInfraLocatorURI(spec.Target.ParentLocator) && !planner.IsInfraLocatorURI(spec.Target.Locator) {
		target, err := spec.Target.EngineRef()
		if err != nil {
			return nil, err
		}
		if target.ID == source.ID {
			scopes[source.ID] = []string{"read", "write"}
		} else {
			scopes[target.ID] = []string{"write"}
		}
	}
	ids := []uint{}
	for id := range scopes {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	accesses := []commonClient.ExecutionEngineAccessScope{}
	for _, id := range ids {
		accesses = append(accesses, commonClient.ExecutionEngineAccessScope{EngineID: strconv.FormatUint(uint64(id), 10), Effects: scopes[id]})
	}
	return accesses, nil
}

func (s *TaskService) SourceProtectionOptions(ctx context.Context, tenant uint, locator string) (*commonClient.TransferProtectionOptions, error) {
	if s.roleProtection == nil {
		return nil, dataprotection.ErrNotReady
	}
	return s.roleProtection.Options(ctx, tenant, locator)
}
func (s *TaskService) CheckSourceProtection(ctx context.Context, tenant uint, request *commonClient.TransferSourceProtectionSelection) error {
	if s.roleProtection == nil {
		return dataprotection.ErrNotReady
	}
	return s.roleProtection.Validate(ctx, tenant, request)
}
