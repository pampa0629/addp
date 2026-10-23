package service

import (
	"context"
	"strconv"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/develop/backend/internal/models"
)

type exportUserTokenKey struct{}

func WithExportUserToken(ctx context.Context, bearer string) context.Context {
	return context.WithValue(ctx, exportUserTokenKey{}, bearer)
}
func (s *SQLEngineService) transferProtectionClient() *commonClient.TransferClient {
	return commonClient.NewTransferClient(s.cfg.TransferServiceURL, nil)
}
func (s *SQLEngineService) ValidateExportProtectionChoices(ctx context.Context, tenant, user uint, bearer string, task *models.DevTask) error {
	choices, err := models.QueryExportProtectionChoices(task.Content)
	if err != nil || len(choices) == 0 {
		return err
	}
	if task.DevType != "query" || task.GetQueryType() != "sql" && task.GetQueryType() != "mql" || s == nil || s.cfg == nil {
		return dataprotection.ErrRoleOptionUnavailable
	}
	facts, ok := executionUserFacts(ctx)
	if !ok || facts.Principal.ID != strconv.FormatUint(uint64(user), 10) || facts.Context.TenantID == nil || *facts.Context.TenantID != strconv.FormatUint(uint64(tenant), 10) || facts.Token.Type != "first_party_access_token" || facts.Delegation != nil {
		return dataprotection.ErrRoleOptionIneligible
	}
	locator, _ := task.Content["target_locator"].(string)
	if task.GetEngineID() == nil {
		return dataprotection.ErrRoleOptionUnavailable
	}
	if _, err := s.queryProtectionTarget(ctx, tenant, *task.GetEngineID(), locator); err != nil {
		return err
	}
	return s.transferProtectionClient().ValidateSourceProtection(ctx, bearer, &commonClient.TransferSourceProtectionSelection{SourceLocator: locator, Choices: choices})
}
func (s *QueryExportService) validateProtection(ctx context.Context, selection *commonClient.TransferSourceProtectionSelection) error {
	if s.executor == nil || s.executor.sqlEngine == nil || s.executor.sqlEngine.cfg == nil {
		return dataprotection.ErrRoleOptionIneligible
	}
	facts, ok := executionUserFacts(ctx)
	if !ok || facts.Token.Type != "first_party_access_token" || facts.Delegation != nil {
		return dataprotection.ErrRoleOptionIneligible
	}
	bearer, _ := ctx.Value(exportUserTokenKey{}).(string)
	if bearer == "" {
		return dataprotection.ErrRoleOptionIneligible
	}
	return s.executor.sqlEngine.transferProtectionClient().ValidateSourceProtection(ctx, bearer, selection)
}
