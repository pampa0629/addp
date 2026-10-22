package client

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/addp/common/authorization"
)

// ResolveCurrentSubjectRoles uses only this request's User bearer. Never retain
// the credential or observation and never substitute this client's Service token.
func (c *SystemServiceClient) ResolveCurrentSubjectRoles(ctx context.Context, bearer string, current *authorization.AuthContext) (*authorization.CurrentSubjectRoles, error) {
	if !validExecutionBearer(bearer) || current == nil || authorization.ValidateAuthContext(*current) != nil || !current.Token.ExpiresAt.After(time.Now().UTC()) || current.Principal.Type != "user" || current.Context.Type != "tenant" || current.Token.Type != "first_party_access_token" || current.Delegation != nil {
		return nil, errors.New("current tenant user roles require a live first-party User credential")
	}
	var result authorization.CurrentSubjectRoles
	if err := c.boundedAuthorizationJSON(ctx, http.MethodGet, "/api/v1/system/auth/subject-roles", bearer, nil, &result, http.StatusOK); err != nil {
		return nil, err
	}
	if err := result.ValidateFor(current, time.Now().UTC()); err != nil {
		return nil, err
	}
	return &result, nil
}
