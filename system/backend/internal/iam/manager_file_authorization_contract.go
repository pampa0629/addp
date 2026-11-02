package iam

import (
	"time"

	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/google/uuid"
)

// IssueManagerFileAuthorizationInput references a pending owner execution.
// Its actor, configuration and complete source composition are derived by the
// authority, never accepted from the HTTP caller.
type IssueManagerFileAuthorizationInput struct {
	SourceAccessToken string
	ExecutionID       uuid.UUID
	ExpiresIn         time.Duration
	Audit             AuditMetadata
}

type IssuedManagerFileAuthorization struct {
	ID              int64
	ExecutionID     uuid.UUID
	TenantID        int64
	SourceReadScope execution.ManagerFileReadScope
	ExpiresAt       time.Time
}

type AuthorizeManagerFileInput struct {
	AuthorizationID    int64
	ExecutionID        uuid.UUID
	Attempt            int
	LeaseToken         uuid.UUID
	SourceReadScope    execution.ManagerFileReadScope
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	Audit              AuditMetadata
}

// ManagerFileAccessObservation holds the uncached current Engine from the
// same source authorization decision. It must not be persisted as a lease.
type ManagerFileAccessObservation struct {
	ObservedAt time.Time
	Engine     *commonmodels.Engine
}
