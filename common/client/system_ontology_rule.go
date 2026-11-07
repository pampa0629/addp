package client

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/authorization"
)

func (c *SystemServiceClient) ConsumeOntologyRuleEvaluation(ctx context.Context, call authorization.OntologyRuleCall) (*authorization.CurrentSubjectRoles, error) {
	if c == nil || c.tenantID == nil || call.Evaluation.Digest() == "" {
		return nil, errors.New("invalid ontology rule authorization binding")
	}
	credential, err := c.TenantServiceAccessToken(ctx, *c.tenantID)
	if err != nil {
		return nil, err
	}
	if !validExecutionBearer(credential) {
		return nil, errors.New("invalid ontology rule service credential")
	}
	var roles authorization.CurrentSubjectRoles
	if err = c.executionAuthorizationJSON(ctx, "/api/v1/system/runtime/ontology-rule-evaluations/consume", credential, call, &roles, http.StatusOK); err != nil {
		return nil, err
	}
	if roles.TenantID != strconv.FormatUint(uint64(*c.tenantID), 10) || roles.ActorPrincipalID != call.PrincipalID ||
		roles.RoleKeys == nil || roles.ObservedAt.IsZero() || !roles.ExpiresAt.After(time.Now()) || !roles.ExpiresAt.After(roles.ObservedAt) {
		return nil, errors.New("invalid ontology rule authorization result")
	}
	for _, id := range []string{roles.ActorPrincipalID, roles.TenantMembershipID, roles.AuthorizationVersion} {
		if _, err := parseCanonicalPositiveID(id); err != nil {
			return nil, errors.New("invalid ontology rule authorization subject")
		}
	}
	for i, role := range roles.RoleKeys {
		if role == "" || strings.TrimSpace(role) != role || (i > 0 && role <= roles.RoleKeys[i-1]) {
			return nil, errors.New("invalid ontology rule authorization roles")
		}
	}
	return &roles, nil
}
