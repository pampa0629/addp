package iam

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/google/uuid"
)

const orchestratorExecutePermission = "orchestrator.workflow.execute"

var sha256HexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type AuthorizeTaskSubjectInput struct {
	OwnerModule          string
	TaskType             string
	TaskRef              uuid.UUID
	DefinitionHash       string
	TenantID             int64
	PrincipalID          int64
	TenantMembershipID   int64
	AuthorizationVersion int64
	Audit                 AuditMetadata
}

type ResolveTaskSubjectInput struct {
	SubjectID         int64
	OwnerModule       string
	TaskType          string
	TaskRef           uuid.UUID
	DefinitionHash    string
	TenantID          int64
	ServicePrincipalID int64
	ServiceClientID    string
	Audit              AuditMetadata
}

type ResolvedTaskAuthorizationSubject struct {
	ID                   int64
	OwnerModule          string
	TaskType             string
	TaskRef              uuid.UUID
	DefinitionHash       string
	TenantID             int64
	PrincipalID          int64
	TenantMembershipID   int64
	AuthorizationVersion int64
	AuthorizedAt         time.Time
}

type TaskAuthorizationSubjectService struct {
	repository *Repository
}

func NewTaskAuthorizationSubjectService(repository *Repository) (*TaskAuthorizationSubjectService, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	return &TaskAuthorizationSubjectService{repository: repository}, nil
}

func (s *TaskAuthorizationSubjectService) Authorize(
	ctx context.Context,
	input AuthorizeTaskSubjectInput,
) (*ResolvedTaskAuthorizationSubject, error) {
	if err := validateTaskAuthorizationIdentity(input.OwnerModule, input.TaskType, input.TaskRef, input.DefinitionHash); err != nil {
		return nil, err
	}
	var result *ResolvedTaskAuthorizationSubject
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, membership, now, err := lockAndValidateExecutionActor(
			ctx, tx, input.PrincipalID, input.TenantMembershipID,
			input.TenantID, input.AuthorizationVersion,
		)
		if err != nil {
			return err
		}
		permissions, err := tx.ListEffectiveRoleAssignmentPermissions(
			ctx, principal.ID, principal.PrincipalType, ContextTypeTenant,
			&membership.TenantID, &membership.ID, now,
		)
		if err != nil {
			return err
		}
		if !containsPermission(permissions, orchestratorExecutePermission) {
			return ErrExecutionAuthorizationPermissionDenied
		}

		subject, err := tx.LockTaskAuthorizationSubject(ctx, input.OwnerModule, input.TaskRef)
		if err != nil && !errors.Is(err, commonapi.ErrNotFound) {
			return err
		}
		if subject == nil || subject.ID == 0 {
			subject = &TaskAuthorizationSubject{
				OwnerModule: input.OwnerModule, TaskType: input.TaskType,
				TaskRef: input.TaskRef, DefinitionHash: input.DefinitionHash,
				TenantID: input.TenantID, PrincipalID: input.PrincipalID,
				TenantMembershipID: input.TenantMembershipID,
				AuthorizationVersion: input.AuthorizationVersion,
				AuthorizedAt: now, CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.CreateTaskAuthorizationSubject(ctx, subject); err != nil {
				return err
			}
		} else {
			if subject.TenantID != input.TenantID || subject.TaskType != input.TaskType {
				return commonapi.ErrConflict
			}
			subject.DefinitionHash = input.DefinitionHash
			subject.PrincipalID = input.PrincipalID
			subject.TenantMembershipID = input.TenantMembershipID
			subject.AuthorizationVersion = input.AuthorizationVersion
			subject.AuthorizedAt = now
			subject.UpdatedAt = now
			if err := tx.UpdateTaskAuthorizationSubject(ctx, subject); err != nil {
				return err
			}
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.task_authorization.authorized",
			Result: AuditResultSucceeded, RiskLevel: AuditRiskHigh, ModuleName: "system",
			EntityType: "task_authorization_subject", EntityID: strconv.FormatInt(subject.ID, 10),
			Details: map[string]any{
				"owner_module": input.OwnerModule, "task_type": input.TaskType,
				"task_ref": input.TaskRef.String(), "definition_hash": input.DefinitionHash,
			},
		}); err != nil {
			return err
		}
		result = mapResolvedTaskAuthorizationSubject(subject)
		return nil
	})
	return result, err
}

func (s *TaskAuthorizationSubjectService) Resolve(
	ctx context.Context,
	input ResolveTaskSubjectInput,
) (*ResolvedTaskAuthorizationSubject, error) {
	if input.SubjectID <= 0 || input.TenantID <= 0 || input.ServicePrincipalID <= 0 ||
		input.ServiceClientID != "addp-orchestrator" {
		return nil, ErrExecutionAuthorizationPermissionDenied
	}
	if err := validateTaskAuthorizationIdentity(input.OwnerModule, input.TaskType, input.TaskRef, input.DefinitionHash); err != nil {
		return nil, err
	}
	var result *ResolvedTaskAuthorizationSubject
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		subject, err := tx.GetTaskAuthorizationSubject(ctx, input.SubjectID)
		if err != nil {
			return ErrExecutionAuthorizationUnavailable
		}
		if subject.OwnerModule != input.OwnerModule || subject.TaskType != input.TaskType ||
			subject.TaskRef != input.TaskRef || subject.DefinitionHash != input.DefinitionHash ||
			subject.TenantID != input.TenantID {
			return ErrExecutionAuthorizationPermissionDenied
		}
		principal, membership, now, err := lockAndValidateExecutionActor(
			ctx, tx, subject.PrincipalID, subject.TenantMembershipID,
			subject.TenantID, subject.AuthorizationVersion,
		)
		if err != nil {
			return err
		}
		permissions, err := tx.ListEffectiveRoleAssignmentPermissions(
			ctx, principal.ID, principal.PrincipalType, ContextTypeTenant,
			&membership.TenantID, &membership.ID, now,
		)
		if err != nil {
			return err
		}
		if !containsPermission(permissions, orchestratorExecutePermission) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.task_authorization.resolved",
			Result: AuditResultSucceeded, RiskLevel: AuditRiskMedium, ModuleName: "system",
			EntityType: "task_authorization_subject", EntityID: strconv.FormatInt(subject.ID, 10),
			Details: map[string]any{"owner_module": subject.OwnerModule, "task_ref": subject.TaskRef.String()},
		}); err != nil {
			return err
		}
		result = mapResolvedTaskAuthorizationSubject(subject)
		return nil
	})
	return result, err
}

func validateTaskAuthorizationIdentity(ownerModule, taskType string, taskRef uuid.UUID, definitionHash string) error {
	if strings.TrimSpace(ownerModule) != "orchestrator" || strings.TrimSpace(taskType) != "orchestration" ||
		taskRef == uuid.Nil || !sha256HexPattern.MatchString(definitionHash) {
		return fmt.Errorf("%w: invalid task authorization identity", commonapi.ErrBadRequest)
	}
	return nil
}

func containsPermission(rows []RoleAssignmentPermissionProjection, key string) bool {
	for _, row := range rows {
		if row.PermissionKey == key {
			return true
		}
	}
	return false
}

func mapResolvedTaskAuthorizationSubject(subject *TaskAuthorizationSubject) *ResolvedTaskAuthorizationSubject {
	return &ResolvedTaskAuthorizationSubject{
		ID: subject.ID, OwnerModule: subject.OwnerModule, TaskType: subject.TaskType,
		TaskRef: subject.TaskRef, DefinitionHash: subject.DefinitionHash,
		TenantID: subject.TenantID, PrincipalID: subject.PrincipalID,
		TenantMembershipID: subject.TenantMembershipID,
		AuthorizationVersion: subject.AuthorizationVersion, AuthorizedAt: subject.AuthorizedAt.UTC(),
	}
}
