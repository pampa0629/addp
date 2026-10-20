package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/addp/manager/internal/repository"
)

type model3DGLBMetaClient interface {
	GetItemByIDForTenant(context.Context, uint, uint) (*commonModels.MetaItem, error)
}

func (s *Model3DGLBTaskService) SetSourceAuthorizationClients(meta model3DGLBMetaClient, issuer *commonClient.SystemExecutionAuthorizationClient, consumer *commonClient.SystemServiceClient) {
	s.sourceMeta, s.sourceIssuer, s.sourceConsumer = meta, issuer, consumer
}

func (s *Model3DGLBTaskService) sourceCandidate(ctx context.Context, tenant uint, config Model3DGLBExecutionConfig) (*model3DGLBSourceCandidate, error) {
	if s.sourceMeta == nil {
		return nil, engineaccess.ErrUnavailable
	}
	item, err := s.sourceMeta.GetItemByIDForTenant(ctx, tenant, config.Source.ItemID)
	if err != nil {
		return nil, engineaccess.ErrUnavailable
	}
	return model3DGLBCandidateFromMeta(tenant, config.Source, item)
}

// Classify with System before creating pending. A caller cannot opt out by
// omitting an FTP marker or claiming a different engine_type in task config.
func (s *Model3DGLBTaskService) freezeSourceExecution(ctx context.Context, snapshot *repository.Model3DGLBTaskSnapshot, item *commonExecution.TaskExecution, credential string) (bool, error) {
	var keys map[string]json.RawMessage
	if json.Unmarshal(snapshot.Config, &keys) != nil {
		return false, ErrModel3DGLBSourceAuthorizationRequired
	}
	for _, key := range []string{"config_version", "owner_task", "source_engine_version", "source_composition"} {
		if _, exists := keys[key]; exists {
			return false, ErrModel3DGLBSourceAuthorizationRequired
		}
	}
	engine, err := s.sourceEngineForSnapshot(ctx, snapshot)
	if err != nil || engine.EngineType != "ftp" {
		return false, err
	}
	if s.sourceIssuer == nil || s.sourceConsumer == nil || s.sourceMeta == nil {
		return true, engineaccess.ErrUnavailable
	}
	if credential == "" {
		return true, ErrModel3DGLBSourceAuthorizationRequired
	}
	config, err := decodeFTPModel3DGLBConversion(snapshot.Config)
	if err != nil {
		return true, err
	}
	candidate, err := s.sourceCandidate(ctx, snapshot.TenantID, config)
	if err != nil {
		return true, err
	}
	item.ExecutionConfig, err = freezeFTPModel3DGLBConfig(snapshot, engine, candidate, item.ExecutionID, s.bucket)
	return true, err
}

// Only the execution UUID goes to System. The current User bearer exists in
// this request stack only; System re-reads pending/config/Actor/Meta/Grant.
func (s *Model3DGLBTaskService) issueSourceAuthorization(ctx context.Context, expected *commonExecution.TaskExecution, credential string) (err error) {
	defer func() {
		if err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			_ = s.repo.FailUnboundExecution(cleanupCtx, expected)
		}
	}()
	raw, err := s.repo.RawExecutionConfig(ctx, expected.TenantID, expected.ExecutionID)
	if err != nil {
		return err
	}
	header, err := decodeModel3DGLBFrozenHeader(raw)
	if err != nil {
		return err
	}
	scope, err := commonExecution.NewManagerModel3DReadScope(raw, header.OwnerTask.ID, header.OwnerTask.Version, header.EngineVersion, header.Composition)
	if err != nil {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	issueCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	issued, err := s.sourceIssuer.IssueManagerModel3D(issueCtx, credential, commonClient.IssueManagerModel3DAuthorizationRequest{ExecutionID: expected.ExecutionID, ExpiresIn: 3600})
	if err != nil && issueCtx.Err() == nil {
		var upstream *commonClient.SystemAPIError
		// One explicit reconciliation of an unknown outcome, with the same
		// pending UUID. System must return the original EA and original expiry.
		// Definite client rejection is never replayed. The SDK itself has no retry.
		if !errors.As(err, &upstream) || upstream.StatusCode == 0 || upstream.StatusCode >= 500 {
			issued, err = s.sourceIssuer.IssueManagerModel3D(issueCtx, credential, commonClient.IssueManagerModel3DAuthorizationRequest{ExecutionID: expected.ExecutionID, ExpiresIn: 3600})
		}
	}
	if err != nil {
		var upstream *commonClient.SystemAPIError
		if errors.As(err, &upstream) && upstream.StatusCode == 401 {
			return commonClient.ErrManagerPreviewCredentialRejected
		}
		if errors.As(err, &upstream) && (upstream.StatusCode == 400 || upstream.StatusCode == 401 || upstream.StatusCode == 403 || upstream.StatusCode == 409) {
			return ErrModel3DGLBSourceAuthorizationRequired
		}
		return engineaccess.ErrUnavailable
	}
	if !issued.Matches(uint(expected.TenantID), *scope) || issued.ExecutionID != expected.ExecutionID {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	return s.repo.BindSourceAuthorization(ctx, expected, *scope, issued)
}
