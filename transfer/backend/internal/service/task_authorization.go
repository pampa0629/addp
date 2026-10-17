package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/google/uuid"
)

var ErrTaskAuthorizationUnavailable = errors.New("task authorization service is unavailable")

type TaskSubjectAuthorizer interface {
	AuthorizeTaskSubject(context.Context, string, commonClient.TaskAuthorizationSubjectRequest) (*commonClient.TaskAuthorizationSubject, error)
}

type taskAuthorizationTokenKey struct{}

// WithTaskAuthorizationToken is used only inside a synchronous verified User
// request. Neither the context nor credential is retained by a task or worker.
func WithTaskAuthorizationToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, taskAuthorizationTokenKey{}, token)
}

func (s *TaskService) SetTaskSubjectAuthorizer(authorizer TaskSubjectAuthorizer) {
	s.taskAuthorizer = authorizer
}

func (s *TaskService) authorizeSchedule(ctx context.Context, task *models.TransferTask) error {
	if strings.TrimSpace(task.Schedule) == "" {
		clearScheduleAuthorization(task)
		return nil
	}
	actor := &commonExecution.TaskExecution{TenantID: int(task.TenantID)}
	if err := commonExecution.ApplyUserOrigin(ctx, actor); err != nil {
		return err
	}
	token, _ := ctx.Value(taskAuthorizationTokenKey{}).(string)
	if !strings.HasPrefix(token, "addp_at_") || len(token) == len("addp_at_") {
		return fmt.Errorf("%w: task authorization requires a current User credential", commonAPI.ErrForbidden)
	}
	if s.taskAuthorizer == nil {
		return ErrTaskAuthorizationUnavailable
	}
	if task.ApplyIdentity == "" {
		task.ApplyIdentity = uuid.NewString()
	}
	hash, err := task.ScheduleAuthorizationHash()
	if err != nil {
		return err
	}
	request := commonClient.TaskAuthorizationSubjectRequest{OwnerModule: "transfer", TaskType: "sync", TaskRef: task.ApplyIdentity, DefinitionHash: hash}
	subject, err := s.taskAuthorizer.AuthorizeTaskSubject(ctx, token, request)
	if err != nil {
		if status, ok := commonClient.SystemAPIStatusCode(err); ok && status == 401 {
			return commonAPI.ErrUnauthorized
		}
		if status, ok := commonClient.SystemAPIStatusCode(err); (ok && status == 403) || errors.Is(err, commonAPI.ErrForbidden) {
			return fmt.Errorf("%w: task authorization was rejected", commonAPI.ErrForbidden)
		}
		return ErrTaskAuthorizationUnavailable
	}
	if subject == nil || subject.OwnerModule != request.OwnerModule || subject.TaskType != request.TaskType || subject.TaskRef != request.TaskRef || subject.DefinitionHash != hash || subject.AuthorizedAt.IsZero() ||
		subject.TenantID != strconv.FormatUint(uint64(task.TenantID), 10) || subject.PrincipalID != strconv.FormatInt(*actor.ActorPrincipalID, 10) || subject.TenantMembershipID != strconv.FormatInt(*actor.ActorTenantMembershipID, 10) || subject.AuthorizationVersion != strconv.FormatInt(*actor.IssuedAuthorizationVersion, 10) {
		return fmt.Errorf("%w: task authorization origin does not match current User", commonAPI.ErrForbidden)
	}
	id, err := strconv.ParseInt(subject.ID, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != subject.ID {
		return fmt.Errorf("%w: invalid task authorization subject", commonAPI.ErrForbidden)
	}
	task.AuthorizationSubjectID = &id
	task.AuthorizationDefinitionHash = &hash
	task.AuthorizationPrincipalID = actor.ActorPrincipalID
	task.AuthorizationMembershipID = actor.ActorTenantMembershipID
	task.AuthorizationVersion = actor.IssuedAuthorizationVersion
	authorizedAt := subject.AuthorizedAt.UTC()
	task.AuthorizedAt = &authorizedAt
	return nil
}

func clearScheduleAuthorization(task *models.TransferTask) {
	task.AuthorizationSubjectID = nil
	task.AuthorizationDefinitionHash = nil
	task.AuthorizationPrincipalID = nil
	task.AuthorizationMembershipID = nil
	task.AuthorizationVersion = nil
	task.AuthorizedAt = nil
}
