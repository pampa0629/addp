package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/semantic"
)

type ExecutionAuthorizationIssuer interface {
	Issue(context.Context, string, client.IssueExecutionAuthorizationRequest) (*client.IssuedExecutionAuthorization, error)
}

// AdmitProjection is the only admission path for a frozen publication intent.
// The current request's User Token is never retained or passed to a worker.
func (s *RevisionService) AdmitProjection(ctx context.Context, actor models.Actor, scope semantic.Scope, version uint64, userToken string, issuer ExecutionAuthorizationIssuer) error {
	if issuer == nil || !strings.HasPrefix(userToken, "addp_at_") || len(userToken) == len("addp_at_") {
		return repository.ErrInvalid
	}
	record, err := s.repo.ProjectionIntent(ctx, actor, scope, version)
	if err != nil {
		return err
	}
	if _, err := s.Get(ctx, actor, scope); err != nil {
		return err
	}
	boundary := execution.InternalTaskScope{TaskType: models.ProjectionTaskType, ResourceID: scope.OntologyID,
		Revision: strconv.FormatUint(scope.Revision, 10), Digest: record.Digest, Generation: *record.Generation}
	issued, err := issuer.Issue(ctx, userToken, client.IssueExecutionAuthorizationRequest{
		Audience: execution.AudienceOntology, ExecutionID: *record.BuildExecutionID, InternalTask: &boundary,
	})
	if err == nil {
		err = s.repo.AttachProjectionAuthorization(ctx, actor, scope, version, issued)
	}
	if err == nil {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return errors.Join(err, s.repo.FailProjectionAdmission(cleanupCtx, actor, record))
}
