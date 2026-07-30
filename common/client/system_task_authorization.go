package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type TaskAuthorizationSubjectRequest struct {
	OwnerModule    string `json:"owner_module"`
	TaskType       string `json:"task_type"`
	TaskRef        string `json:"task_ref"`
	DefinitionHash string `json:"definition_hash"`
}

type TaskAuthorizationSubject struct {
	ID                   string    `json:"id"`
	OwnerModule          string    `json:"owner_module"`
	TaskType             string    `json:"task_type"`
	TaskRef              string    `json:"task_ref"`
	DefinitionHash       string    `json:"definition_hash"`
	TenantID             string    `json:"tenant_id"`
	PrincipalID          string    `json:"principal_id"`
	TenantMembershipID   string    `json:"tenant_membership_id"`
	AuthorizationVersion string    `json:"authorization_version"`
	AuthorizedAt         time.Time `json:"authorized_at"`
}

func (c *SystemExecutionAuthorizationClient) AuthorizeTaskSubject(
	ctx context.Context,
	userAccessToken string,
	request TaskAuthorizationSubjectRequest,
) (*TaskAuthorizationSubject, error) {
	if c == nil || c.system == nil || c.system.baseURL == "" ||
		!strings.HasPrefix(userAccessToken, "addp_at_") || len(userAccessToken) == len("addp_at_") {
		return nil, errors.New("task authorization requires a System URL and User Access Token")
	}
	var response TaskAuthorizationSubject
	_, err := c.system.doJSON(
		ctx, http.MethodPost, "/api/v1/system/auth/task-authorization-subjects",
		userAccessToken, request, &response,
	)
	if err != nil {
		return nil, err
	}
	if err := validateTaskAuthorizationSubject(&response, request); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *SystemServiceClient) ResolveTaskAuthorizationSubject(
	ctx context.Context,
	subjectID string,
	request TaskAuthorizationSubjectRequest,
) (*TaskAuthorizationSubject, error) {
	if _, err := parseCanonicalPositiveID(subjectID); err != nil {
		return nil, errors.New("task authorization resolve requires a canonical subject ID")
	}
	var response TaskAuthorizationSubject
	path := fmt.Sprintf("/api/v1/system/runtime/task-authorization-subjects/%s/resolve", subjectID)
	if err := c.doTenantJSON(ctx, http.MethodPost, path, request, &response); err != nil {
		return nil, err
	}
	if response.ID != subjectID {
		return nil, errors.New("System task authorization returned a mismatched subject")
	}
	if err := validateTaskAuthorizationSubject(&response, request); err != nil {
		return nil, err
	}
	return &response, nil
}

func validateTaskAuthorizationSubject(
	response *TaskAuthorizationSubject,
	request TaskAuthorizationSubjectRequest,
) error {
	if response == nil || response.OwnerModule != request.OwnerModule || response.TaskType != request.TaskType ||
		response.TaskRef != request.TaskRef || response.DefinitionHash != request.DefinitionHash ||
		response.AuthorizedAt.IsZero() {
		return errors.New("System task authorization returned an invalid response")
	}
	if parsed, err := uuid.Parse(response.TaskRef); err != nil || parsed == uuid.Nil || parsed.String() != response.TaskRef {
		return errors.New("System task authorization returned an invalid task ref")
	}
	for _, value := range []string{
		response.ID, response.TenantID, response.PrincipalID,
		response.TenantMembershipID, response.AuthorizationVersion,
	} {
		if _, err := parseCanonicalPositiveID(value); err != nil {
			return errors.New("System task authorization returned an invalid IAM ID")
		}
	}
	return nil
}
