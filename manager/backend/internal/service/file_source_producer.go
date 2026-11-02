package service

import (
	"context"
	"encoding/json"
	"errors"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/engineaccess"
	"time"
)

type fileSourceExecutionRepository interface {
	RawExecutionConfig(context.Context, int, string) (json.RawMessage, error)
	FailUnboundExecution(context.Context, *commonExecution.TaskExecution) error
	BindSourceAuthorization(context.Context, *commonExecution.TaskExecution, commonExecution.ManagerFileReadScope, *commonClient.IssuedManagerFileAuthorization) error
}

func issueFileExecutionAuthorization(ctx context.Context, repo fileSourceExecutionRepository, issuer *commonClient.SystemExecutionAuthorizationClient, expected *commonExecution.TaskExecution, credential, purpose string) (err error) {
	if repo == nil || issuer == nil || expected == nil || credential == "" {
		return ErrFileSourceAuthorizationRequired
	}
	defer func() {
		if err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			_ = repo.FailUnboundExecution(cleanupCtx, expected)
		}
	}()
	raw, err := repo.RawExecutionConfig(ctx, expected.TenantID, expected.ExecutionID)
	if err != nil {
		return err
	}
	var header struct {
		OwnerTask     struct{ ID, Version string }                `json:"owner_task"`
		EngineVersion string                                      `json:"source_engine_version"`
		Composition   *authorizationfacts.FileCompositionBoundary `json:"source_composition"`
	}
	err = json.Unmarshal(raw, &header)
	if err != nil {
		return err
	}
	scope, err := commonExecution.NewManagerFileReadScope(raw, header.OwnerTask.ID, header.OwnerTask.Version, header.EngineVersion, header.Composition)
	if err != nil || scope.Purpose() != purpose {
		return ErrFileSourceAuthorizationRequired
	}
	issueCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	var issue func(context.Context, string, commonClient.IssueManagerFileAuthorizationRequest) (*commonClient.IssuedManagerFileAuthorization, error)
	switch purpose {
	case commonExecution.TaskTypeModel3DGLBGeneration:
		issue = issuer.IssueManagerModel3D
	case commonExecution.TaskTypeRasterCOGGeneration:
		issue = issuer.IssueManagerRasterCOG
	default:
		return ErrFileSourceAuthorizationRequired
	}
	issued, err := issue(issueCtx, credential, commonClient.IssueManagerFileAuthorizationRequest{ExecutionID: expected.ExecutionID, ExpiresIn: 3600})
	if err != nil && issueCtx.Err() == nil {
		var upstream *commonClient.SystemAPIError
		// One explicit reconciliation of an unknown outcome, with the same
		// pending UUID. System must return the original EA and original expiry.
		// Definite client rejection is never replayed. The SDK itself has no retry.
		if !errors.As(err, &upstream) || upstream.StatusCode == 0 || upstream.StatusCode >= 500 {
			issued, err = issue(issueCtx, credential, commonClient.IssueManagerFileAuthorizationRequest{ExecutionID: expected.ExecutionID, ExpiresIn: 3600})
		}
	}
	if err != nil {
		var upstream *commonClient.SystemAPIError
		if errors.As(err, &upstream) && upstream.StatusCode == 401 {
			return commonClient.ErrManagerPreviewCredentialRejected
		}
		if errors.As(err, &upstream) && (upstream.StatusCode == 400 || upstream.StatusCode == 401 || upstream.StatusCode == 403 || upstream.StatusCode == 409) {
			return ErrFileSourceAuthorizationRequired
		}
		return engineaccess.ErrUnavailable
	}
	if !issued.Matches(uint(expected.TenantID), *scope) || issued.ExecutionID != expected.ExecutionID {
		return ErrFileSourceAuthorizationRequired
	}
	return repo.BindSourceAuthorization(ctx, expected, *scope, issued)
}
