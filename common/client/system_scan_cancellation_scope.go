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
func (c *SystemServiceClient) GetMetaScanCancellationScope(ctx context.Context, engineID int64, userToken string) (*authorization.EngineAccessHandlingScope, error) {
	if c == nil || c.baseURL == "" || c.httpClient == nil || engineID <= 0 || !strings.HasPrefix(userToken, "addp_at_") || len(userToken) == len("addp_at_") || strings.ContainsAny(userToken, " \t\r\n") {
		return nil, errors.New("scan cancellation requires an engine and current User Access Token")
	}
	var result authorization.EngineAccessHandlingScope
	status, err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/system/engines/%d/scan_cancellation_scope", engineID), userToken, nil, &result)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || result.EngineID != engineID || result.TenantID <= 0 || result.Operator.PrincipalID <= 0 || result.Operator.MembershipID <= 0 || result.Operator.AuthorizationVersion <= 0 || result.VerifiedAt.IsZero() {
		return nil, errors.New("invalid scan cancellation scope observation")
	}
	return &result, nil
}
