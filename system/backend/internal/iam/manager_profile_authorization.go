package iam

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/google/uuid"
)

type ManagerProfileAuthorizationService struct {
	kernel *managerSourceAuthorizationService
}

func NewManagerProfileAuthorizationService(repository *Repository, prepare ExecutionSourceReadPreparer) *ManagerProfileAuthorizationService {
	return &ManagerProfileAuthorizationService{kernel: newManagerSourceAuthorizationService(repository, prepare, managerSourceProfile)}
}

type IssueManagerProfileAuthorizationInput struct {
	SourceAccessToken string
	ExecutionID       uuid.UUID
	ExpiresIn         time.Duration
	Audit             AuditMetadata
}
type IssuedManagerProfileAuthorization struct {
	ID              int64
	ExecutionID     uuid.UUID
	TenantID        int64
	SourceReadScope execution.ManagerProfileReadScope
	ExpiresAt       time.Time
}
type AuthorizeManagerProfileInput struct {
	AuthorizationID    int64
	ExecutionID        uuid.UUID
	Attempt            int
	LeaseToken         uuid.UUID
	SourceReadScope    execution.ManagerProfileReadScope
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	Audit              AuditMetadata
}

func (s *ManagerProfileAuthorizationService) Issue(ctx context.Context, input IssueManagerProfileAuthorizationInput) (*IssuedManagerProfileAuthorization, error) {
	if s == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	row, err := s.kernel.issue(ctx, issueManagerSourceInput(input))
	if err != nil {
		return nil, err
	}
	scope := row.SourceReadScope.Profile()
	if scope == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	return &IssuedManagerProfileAuthorization{ID: row.ID, ExecutionID: row.ExecutionID, TenantID: row.TenantID, ExpiresAt: row.ExpiresAt, SourceReadScope: *scope}, nil
}
func (s *ManagerProfileAuthorizationService) Authorize(ctx context.Context, input AuthorizeManagerProfileInput) (time.Time, error) {
	if s == nil {
		return time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	scope := NewManagerProfileSourceScope(&input.SourceReadScope)
	observation, err := s.kernel.authorize(ctx, authorizeManagerSourceInput{AuthorizationID: input.AuthorizationID, ExecutionID: input.ExecutionID, Attempt: input.Attempt, LeaseToken: input.LeaseToken, SourceReadScope: *scope, ServicePrincipalID: input.ServicePrincipalID, ServiceClientID: input.ServiceClientID, TenantID: input.TenantID, Audit: input.Audit})
	if err != nil {
		return time.Time{}, err
	}
	return observation.ObservedAt, nil
}
func managerProfileScope(raw json.RawMessage) (*execution.ManagerProfileReadScope, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	var version string
	var engineID uint
	if json.Unmarshal(fields["config_version"], &version) != nil || version != "data-profile-config/v6" ||
		json.Unmarshal(fields["engine_id"], &engineID) != nil || engineID == 0 {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	var readSet plugin.QueryReadSet
	decoder := json.NewDecoder(bytes.NewReader(fields["read_set"]))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&readSet) != nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	scope, err := execution.NewManagerProfileReadScope(raw, &readSet)
	if err != nil || readSet.Paths[0].EngineID != engineID {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	return scope, nil
}
