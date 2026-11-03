package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
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
	if _, uploaded := run.ExecutionConfig["upload_receipt"]; uploaded {
		return s.uploadReadContext(ctx, run, lease)
	}
	var refresh *authorization.MetadataRefreshScope
	if raw, exists := run.ExecutionConfig["refresh_scope"]; exists {
		encoded, err := json.Marshal(raw)
		if err != nil || json.Unmarshal(encoded, &refresh) != nil || refresh == nil {
			return nil, commonapi.ErrForbidden
		}
	}
	if run.SourceTaskID == nil && refresh == nil {
		profile, _ := run.ExecutionConfig["engine_type"].(string)
		// Trusted production adapters retain their independently verified owner
		// receipt/parent route. Ordinary PG/NFS runs have no unqualified bypass.
		if run.ParentExecutionID == nil && run.Metadata["schema_change_source"] == nil && (profile == "postgresql" || profile == "nfs" || profile == "") {
			return nil, commonapi.ErrForbidden
		}
		return ctx, nil
	}
	if (refresh == nil && run.Source != "meta") || run.ParentExecutionID != nil {
		return nil, commonapi.ErrForbidden
	}
	if s.collectionAuthorization == nil {
		return nil, ErrScanPlanFactsUnavailable
	}
	facts, err := s.GetScanPlanFacts(ctx, uint(run.TenantID), run.ExecutionID)
	if err != nil {
		return nil, err
	}
	if facts.BasisState != authorizationfacts.AuthorizationBasisComplete || (refresh == nil && (facts.ScanDepth != "basic" || facts.TaskID == nil || run.SourceTaskID == nil || *facts.TaskID != *run.SourceTaskID)) || (refresh != nil && facts.RefreshScope == nil) {
		return nil, commonapi.ErrForbidden
	}
	request := execution.MetaCollectionAccessRequest{ExecutionID: run.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token, ExecutionPlanDigest: facts.ExecutionPlanDigest}
	check := scanresource.CollectionReadCheck(func(callCtx context.Context, resource *commonmodels.Engine) error {
		if err := s.checkScanCancellation(callCtx, run.TenantID, run.ExecutionID); err != nil {
			return err
		}
		if resource != nil && (resource.ID != facts.EngineID || resource.TenantID == nil || int(*resource.TenantID) != run.TenantID ||
			(refresh == nil && resource.EngineType != "postgresql") || (refresh != nil && ((resource.EngineType != "postgresql" && resource.EngineType != "nfs") || (resource.EngineType == "postgresql") != (refresh.Query.Target.Segments[0].Term == "server"))) || strconv.FormatInt(resource.Version, 10) != facts.EngineVersion) {
			return commonapi.ErrForbidden
		}
		observed, err := s.collectionAuthorization.CheckMetaCollection(callCtx, uint(run.TenantID), request)
		if err != nil {
			// Preserve a formal denial in the existing safe execution diagnostics;
			// dependency/transport failures must never be classified as denial.
			if status, ok := client.TenantAPIStatusCode(err); ok && status == http.StatusForbidden {
				return errors.Join(commonapi.ErrForbidden, err)
			}
			return err
		}
		if observed == nil || observed.ExecutionID != request.ExecutionID || observed.ExecutionPlanDigest != request.ExecutionPlanDigest || observed.ObservedAt.IsZero() {
			return commonapi.ErrForbidden
		}
		return nil
	})
	if err := check(ctx, nil); err != nil {
		return nil, err
	}
	ctx = scanresource.WithCollectionReadCheck(ctx, check)
	if refresh != nil {
		ctx = scanresource.WithMetadataRefreshScope(ctx, refresh)
	}
	return ctx, nil
}
