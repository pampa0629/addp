package client

import (
	"context"
	"errors"
	"fmt"
	"github.com/addp/common/authorization"

	"net/http"
	"strings"
)

// The exact request-scoped User token is used once; no Service fallback.
func (c *SystemServiceClient) GetMetaScanManagementScope(ctx context.Context, engineID int64, operation, userToken string) (*authorization.EngineAccessHandlingScope, error) {
	if c == nil || c.baseURL == "" || c.httpClient == nil || engineID <= 0 || !strings.HasPrefix(userToken, "addp_at_") || len(userToken) == len("addp_at_") || strings.ContainsAny(userToken, " \t\r\n") {
		return nil, errors.New("scan management requires an engine and current User Access Token")
	}
	switch operation {
	case "create", "update", "delete", "execute", "cancel":
	default:
		return nil, errors.New("invalid scan management operation")
	}
	var result authorization.EngineAccessHandlingScope
	status, err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/system/engines/%d/scan_%s_scope", engineID, operation), userToken, nil, &result)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || result.EngineID != engineID || result.TenantID <= 0 || result.Operator.PrincipalID <= 0 || result.Operator.MembershipID <= 0 || result.Operator.AuthorizationVersion <= 0 || result.VerifiedAt.IsZero() {
		return nil, errors.New("invalid scan management scope observation")
	}
	return &result, nil
}
