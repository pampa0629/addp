package execution

import (
	"fmt"

	"github.com/addp/common/events"
	"github.com/google/uuid"
)

const AudienceCleanup = "cleanup"

// CleanupAuthorizationScope binds one Owner to the exact immutable request.
// The digest includes action, mode, scope, confirmation and the original User.
// This purpose cannot grant engine access or authorize another Owner.
type CleanupAuthorizationScope struct {
	RequestID         string `json:"request_id"`
	ParentExecutionID string `json:"parent_execution_id"`
	RequestDigest     string `json:"request_digest"`
	Module            string `json:"module"`
}

func (s CleanupAuthorizationScope) Validate() error {
	for _, value := range []string{s.RequestID, s.ParentExecutionID} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return fmt.Errorf("invalid cleanup authorization identity")
		}
	}
	if !events.CleanupModuleValid(s.Module) || !internalDigestPattern.MatchString(s.RequestDigest) {
		return fmt.Errorf("invalid cleanup authorization scope")
	}
	return nil
}

func CleanupAuthorizationFor(e TaskExecution) (CleanupAuthorizationScope, error) {
	request, err := CleanupRequestFromExecution(e)
	if err != nil || e.TaskType != TaskTypeCleanupExecutor || request.RequestedBy == 0 || request.CauseEvent == "tenant.deleted" {
		return CleanupAuthorizationScope{}, fmt.Errorf("cleanup requires a trusted active User origin")
	}
	scope := CleanupAuthorizationScope{RequestID: request.RequestID, ParentExecutionID: request.ParentExecutionID, RequestDigest: request.Digest, Module: e.Module}
	return scope, scope.Validate()
}
