package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
)

var (
	ErrManagerPreviewCredentialRejected = errors.New("manager preview credential rejected")
	ErrManagerPreviewReadDenied         = errors.New("manager preview source read denied")
	ErrManagerPreviewReadUnavailable    = errors.New("manager preview source read check unavailable")
)

// CheckManagerPreviewRead consumes System's fixed synchronous preview contract.
// credential is the current request's User or delegated data.preview token,
// never this client's Service token. Success is not a reusable access lease.
func (c *SystemServiceClient) CheckManagerPreviewRead(ctx context.Context, credential string, targets []plugin.EngineCatalogPath) error {
	return c.checkManagerSourceRead(ctx, credential, targets, "/api/v1/system/engine-access/read-checks/manager-preview", true)
}

// CheckManagerProfileResultRead uses only the current ordinary User request.
// Targets must be the persisted actual sampling ReadSet, not current dependencies.
func (c *SystemServiceClient) CheckManagerProfileResultRead(ctx context.Context, credential string, targets []plugin.EngineCatalogPath) error {
	return c.checkManagerSourceRead(ctx, credential, targets, "/api/v1/system/engine-access/read-checks/manager-profile-result", false)
}

func (c *SystemServiceClient) checkManagerSourceRead(ctx context.Context, credential string, targets []plugin.EngineCatalogPath, path string, delegated bool) error {
	validToken := false
	prefixes := []string{"addp_at_"}
	if delegated {
		prefixes = append(prefixes, "addp_dat_")
	}
	for _, prefix := range prefixes {
		validToken = validToken || strings.HasPrefix(credential, prefix) && len(credential) > len(prefix)
	}
	if !validToken || strings.ContainsAny(credential, " \t\r\n") {
		return ErrManagerPreviewCredentialRejected
	}
	if len(targets) == 0 || len(targets) > 200 {
		return ErrManagerPreviewReadDenied
	}
	for _, target := range targets {
		if _, err := authorization.EncodeSharingTarget(target); err != nil {
			return ErrManagerPreviewReadDenied
		}
	}
	if c == nil || c.baseURL == "" || c.httpClient == nil {
		return ErrManagerPreviewReadUnavailable
	}
	var response struct {
		ObservedAt time.Time `json:"observed_at"`
	}
	// A redirect is not the fixed System check. Keep caller-owned clients
	// unchanged, and do not forward this credential or repeat the operation.
	bound := *c
	httpClient := *c.httpClient
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	bound.httpClient = &httpClient
	status, err := bound.doJSON(ctx, http.MethodPost, path, credential,
		struct {
			Targets []plugin.EngineCatalogPath `json:"targets"`
		}{Targets: targets}, &response, 4096)
	if status == http.StatusUnauthorized {
		return ErrManagerPreviewCredentialRejected
	}
	if status == http.StatusForbidden {
		return ErrManagerPreviewReadDenied
	}
	// Do not expose an upstream response body or credential-bearing transport
	// diagnostic to owner logging. Never retry as a machine after rejection.
	if err != nil || status != http.StatusOK || response.ObservedAt.IsZero() {
		return ErrManagerPreviewReadUnavailable
	}
	return nil
}
