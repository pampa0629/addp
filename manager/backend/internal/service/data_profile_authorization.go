package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
)

type dataProfileAuthorizationConsumer interface {
	CheckProfileAccess(context.Context, uint, string, commonClient.ManagerProfileAccessRequest) (*commonClient.ManagerProfileAccessObservation, error)
}

type profileServiceConsumer struct {
	client *commonClient.SystemServiceClient
}

func (c profileServiceConsumer) CheckProfileAccess(ctx context.Context, tenantID uint, id string, req commonClient.ManagerProfileAccessRequest) (*commonClient.ManagerProfileAccessObservation, error) {
	return c.client.WithTenantID(tenantID).CheckManagerProfileAccess(ctx, id, req)
}

// SetExecutionAccessClient uses the existing Tenant Service token source. It
// never accepts or persists the initiating User credential.
func (s *DataProfileService) SetExecutionAccessClient(client *commonClient.SystemServiceClient) {
	if client == nil {
		s.authorizationConsumer = nil
		return
	}
	s.authorizationConsumer = profileServiceConsumer{client: client}
}

func validateProfileClaim(ctx context.Context, item *commonExecution.TaskExecution) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if item == nil || !ok || item.TenantID <= 0 || item.Module != commonExecution.ModuleManager || item.Source != commonExecution.ModuleManager ||
		item.TaskType != commonExecution.TaskTypeDataProfiling || item.ExecutionBoundary != commonExecution.ExecutionBoundaryBounded ||
		item.TriggerType != commonExecution.TriggerTypeManual || item.SourceTaskID != nil || item.ParentExecutionID != nil ||
		item.Status != commonExecution.ExecutionStatusRunning || item.Attempt <= 0 ||
		item.ExecutionAuthorizationID == nil || *item.ExecutionAuthorizationID <= 0 ||
		item.AuthorizationExpiresAt == nil || item.AuthorizationExpiresAt.IsZero() ||
		item.LeaseExpiresAt == nil || item.LeaseExpiresAt.IsZero() ||
		item.LeaseOwner == nil || *item.LeaseOwner == "" || item.LeaseToken == nil || *item.LeaseToken == "" ||
		lease.ExecutionID != item.ExecutionID || lease.TenantID != item.TenantID || lease.Attempt != item.Attempt ||
		lease.Token != *item.LeaseToken || lease.Owner != *item.LeaseOwner {
		return ErrDataProfileSourceAuthorizationRequired
	}
	return nil
}

func (s *DataProfileService) consumeProfileAuthorization(ctx context.Context, item *commonExecution.TaskExecution, raw json.RawMessage, frozen *frozenDataProfileConfig, plan *DataProfileSamplePlan) error {
	if ctx.Err() != nil {
		return ErrDataProfileUnavailable
	}
	if err := validateProfileClaim(ctx, item); err != nil {
		return err
	}
	if s.authorizationConsumer == nil {
		return ErrDataProfileUnavailable
	}
	if frozen == nil || plan == nil || !reflect.DeepEqual(plan.Positions(), frozen.Pages) {
		return ErrDataProfileSourceChanged
	}
	scope, err := plan.SourceReadScope(raw)
	if err != nil {
		return ErrDataProfileSourceAuthorizationRequired
	}
	if !reflect.DeepEqual(scope.ReadSet, frozen.ReadSet) || scope.ReadSet.Paths[0].EngineID != frozen.EngineID {
		return ErrDataProfileSourceChanged
	}
	lease, _ := commonExecution.LeaseFromContext(ctx)
	observed, err := s.authorizationConsumer.CheckProfileAccess(ctx, uint(item.TenantID), strconv.FormatInt(*item.ExecutionAuthorizationID, 10), commonClient.ManagerProfileAccessRequest{
		ExecutionID: item.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token, SourceReadScope: *scope.Clone(),
	})
	if err != nil {
		var upstream *commonClient.SystemAPIError
		if errors.As(err, &upstream) && (upstream.StatusCode == 400 || upstream.StatusCode == 401 || upstream.StatusCode == 403 || upstream.StatusCode == 409) {
			return ErrDataProfileSourceAuthorizationRequired
		}
		return ErrDataProfileUnavailable
	}
	if observed == nil || observed.ObservedAt.IsZero() || ctx.Err() != nil {
		return ErrDataProfileUnavailable
	}
	return nil
}

func (s *DataProfileService) issueProfileAuthorization(ctx context.Context, credential string, execution *commonExecution.TaskExecution, readSet *plugin.QueryReadSet) (resultErr error) {
	defer func() {
		if resultErr == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.executions.FailUnbound(cleanup, execution); err != nil {
			resultErr = ErrDataProfileUnavailable
		}
	}()
	// Covers the entire post-persistence issuance/binding phase; no lease or
	// source query exists yet. The queue reaps abandoned preparations after two
	// minutes, leaving a full minute for failed-request cleanup.
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	raw, err := s.executions.GetRawExecutionConfig(ctx, execution.TenantID, execution.ExecutionID)
	if err != nil {
		return ErrDataProfileUnavailable
	}
	scope, err := commonExecution.NewManagerProfileReadScope(raw, readSet)
	if err != nil {
		return ErrDataProfileSourceAuthorizationRequired
	}
	issued, err := s.authorizationIssuer.IssueManagerProfile(ctx, credential, commonClient.IssueManagerProfileAuthorizationRequest{ExecutionID: execution.ExecutionID})
	if err != nil {
		var upstream *commonClient.SystemAPIError
		if errors.As(err, &upstream) {
			switch upstream.StatusCode {
			case 401:
				return ErrDataProfileActorExpired
			case 403:
				return ErrDataProfileSourceAuthorizationRequired
			}
		}
		return ErrDataProfileUnavailable
	}
	if issued == nil || issued.ExecutionID != execution.ExecutionID || !issued.Matches(uint(execution.TenantID), *scope) {
		return ErrDataProfileSourceAuthorizationRequired
	}
	if err := s.executions.BindSourceAuthorization(ctx, execution, *scope, issued); err != nil {
		return ErrDataProfileUnavailable
	}
	return nil
}
