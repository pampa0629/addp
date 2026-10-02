package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/addp/common/authorization"
)

// The User token is request-scoped. It is never retained or substituted with
// this client's Service credentials, and a 401 never retries as a machine.
func (c *SystemServiceClient) GetEngineAccessHandlingScope(ctx context.Context, engineID int64, userToken string) (*authorization.EngineAccessHandlingScope, error) {
	if c == nil || c.baseURL == "" || c.httpClient == nil || engineID <= 0 ||
		!strings.HasPrefix(userToken, "addp_at_") || len(userToken) == len("addp_at_") || strings.ContainsAny(userToken, " \t\r\n") {
		return nil, errors.New("handling scope requires an engine and current User Access Token")
	}
	var result authorization.EngineAccessHandlingScope
	status, err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/system/engines/%d/access_handling_scope", engineID), userToken, nil, &result)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || result.EngineID != engineID || result.TenantID <= 0 || result.Operator.PrincipalID <= 0 ||
		result.Operator.MembershipID <= 0 || result.Operator.AuthorizationVersion <= 0 || result.VerifiedAt.IsZero() {
		return nil, errors.New("handling scope response is incomplete or bound to another engine")
	}
	return &result, nil
}
