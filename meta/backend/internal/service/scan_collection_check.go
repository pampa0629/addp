package service

import (
	"context"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/scanresource"
)

type collectionAuthorizationClient interface {
	CheckMetaCollection(context.Context, uint, execution.MetaCollectionAccessRequest) (*execution.MetaCollectionAccessObservation, error)
}

func (s *ScanExecutionService) SetCollectionAuthorizationClient(client collectionAuthorizationClient) {
	s.collectionAuthorization = client
}

func (s *ScanExecutionService) bindCollectionRead(ctx context.Context, run *execution.TaskExecution, lease execution.Lease) (context.Context, error) {
	if run.SourceTaskID == nil {
		return ctx, nil
	}
	if s.collectionAuthorization == nil {
		return nil, ErrScanPlanFactsUnavailable
	}
	facts, err := s.GetScanPlanFacts(ctx, uint(run.TenantID), run.ExecutionID)
	if err != nil {
		return nil, err
	}
	if facts.BasisState != authorizationfacts.AuthorizationBasisComplete || facts.ScanDepth != "basic" || facts.TaskID == nil || *facts.TaskID != *run.SourceTaskID {
		return nil, commonapi.ErrForbidden
	}
	request := execution.MetaCollectionAccessRequest{ExecutionID: run.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token, ExecutionPlanDigest: facts.ExecutionPlanDigest}
	check := scanresource.CollectionReadCheck(func(callCtx context.Context, resource *commonmodels.Engine) error {
		if resource != nil && (resource.ID != facts.EngineID || resource.TenantID == nil || int(*resource.TenantID) != run.TenantID || resource.EngineType != "postgresql" || strconv.FormatInt(resource.Version, 10) != facts.EngineVersion) {
			return commonapi.ErrForbidden
		}
		observed, err := s.collectionAuthorization.CheckMetaCollection(callCtx, uint(run.TenantID), request)
		if err != nil {
			return err
		}
		if observed == nil || observed.ExecutionID != request.ExecutionID || observed.ExecutionPlanDigest != request.ExecutionPlanDigest || observed.QualificationID == "" || observed.ObservedAt.IsZero() {
			return commonapi.ErrForbidden
		}
		return nil
	})
	if err := check(ctx, nil); err != nil {
		return nil, err
	}
	return scanresource.WithCollectionReadCheck(ctx, check), nil
}
