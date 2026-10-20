package iam

import (
	"time"

	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/google/uuid"
)

// IssueManagerModel3DAuthorizationInput references a pending owner execution.
// Its actor, configuration and complete source composition are derived by the
// authority, never accepted from the HTTP caller.
type IssueManagerModel3DAuthorizationInput struct {
	SourceAccessToken string
	ExecutionID       uuid.UUID
	ExpiresIn         time.Duration
	Audit             AuditMetadata
}

type IssuedManagerModel3DAuthorization struct {
	ID              int64
	ExecutionID     uuid.UUID
	TenantID        int64
	SourceReadScope execution.ManagerModel3DReadScope
	ExpiresAt       time.Time
}

type AuthorizeManagerModel3DInput struct {
	AuthorizationID    int64
	ExecutionID        uuid.UUID
	Attempt            int
	LeaseToken         uuid.UUID
	SourceReadScope    execution.ManagerModel3DReadScope
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	Audit              AuditMetadata
}

// ManagerModel3DAccessObservation holds the uncached current Engine from the
// same source authorization decision. It must not be persisted as a lease.
type ManagerModel3DAccessObservation struct {
	ObservedAt time.Time
	Engine     *commonmodels.Engine
}
