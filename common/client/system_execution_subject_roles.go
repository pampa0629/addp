package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/execution"
)

type ExecutionSubjectRolesRequest struct {
	ExecutionID string `json:"execution_id"`
	Attempt     int    `json:"attempt"`
	LeaseToken  string `json:"lease_token"`
}

// ResolveExecutionSubjectRoles observes current user roles for this attempt only.
// Neither the response nor the service credential may be persisted in task config.
func (c *SystemServiceClient) ResolveExecutionSubjectRoles(ctx context.Context, authorization *IssuedExecutionAuthorization, owner string, request ExecutionSubjectRolesRequest) (*execution.SubjectRoles, error) {
	invalid := errors.New("invalid execution subject roles binding")
	if c == nil || c.tenantTokens == nil || c.tenantID == nil || *c.tenantID == 0 || authorization == nil || authorization.SourceType != "user" || authorization.InternalTask != nil || (owner != "develop" && owner != "transfer") || (authorization.Audience != owner && !(owner == "develop" && authorization.Audience == "duckdb")) || !canonicalExecutionUUID(request.ExecutionID) || !canonicalExecutionUUID(request.LeaseToken) || request.Attempt <= 0 || authorization.ExecutionID != request.ExecutionID || authorization.TenantID != strconv.FormatUint(uint64(*c.tenantID), 10) || !authorization.ExpiresAt.After(time.Now()) {
		return nil, invalid
	}
	for _, id := range []string{authorization.ID, authorization.ActorPrincipalID, authorization.TenantMembershipID, authorization.IssuedAuthorizationVersion} {
		if _, err := parseCanonicalPositiveID(id); err != nil {
			return nil, invalid
		}
	}
	credential, err := c.tenantTokens.Token(ctx, *c.tenantID)
	if err != nil {
		return nil, err
	}
	if !validExecutionBearer(credential) {
		return nil, invalid
	}
	var result execution.SubjectRoles
	path := fmt.Sprintf("/api/v1/system/execution-authorizations/%s/subject-roles", authorization.ID)
	if err := c.executionAuthorizationJSON(ctx, path, credential, request, &result, http.StatusOK); err != nil {
		return nil, err
	}
	if result.AuthorizationID != authorization.ID || result.ExecutionID != request.ExecutionID || result.Attempt != request.Attempt || result.Owner != owner || result.TenantID != authorization.TenantID || result.ActorPrincipalID != authorization.ActorPrincipalID || result.TenantMembershipID != authorization.TenantMembershipID || result.AuthorizationVersion != authorization.IssuedAuthorizationVersion || result.RoleKeys == nil || result.ObservedAt.IsZero() || result.ObservedAt.After(time.Now().Add(time.Minute)) || !result.ExpiresAt.After(time.Now()) || !result.ExpiresAt.After(result.ObservedAt) || result.ExpiresAt.After(authorization.ExpiresAt) {
		return nil, invalid
	}
	for i, key := range result.RoleKeys {
		if key == "" || strings.TrimSpace(key) != key || (i > 0 && key <= result.RoleKeys[i-1]) {
			return nil, invalid
		}
	}
	return &result, nil
}
