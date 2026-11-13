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

// ObserveCurrentSourceRead consumes System's fixed synchronous preview contract.
// credential is the current request's User or delegated data.preview token,
// never this client's Service token. Success is not a reusable access lease.
func (c *SystemServiceClient) ObserveCurrentSourceRead(ctx context.Context, credential string, targets []plugin.EngineCatalogPath) error {
	return c.checkSourceRead(ctx, credential, targets, "/api/v1/system/engine-access/read-observations/current-source", true)
}

func (c *SystemServiceClient) checkSourceRead(ctx context.Context, credential string, targets []plugin.EngineCatalogPath, path string, delegated bool) error {
	request := struct {
		Targets []plugin.EngineCatalogPath `json:"targets"`
	}{Targets: targets}
	return c.checkSourceReadRequest(ctx, credential, targets, path, delegated, request)
}

func (c *SystemServiceClient) checkSourceReadRequest(ctx context.Context, credential string, targets []plugin.EngineCatalogPath, path string, delegated bool, request any) error {
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
		request, &response, 4096)
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

// CheckDevelopWorkflowResultRead consumes only the current ordinary User token.
// The owner supplies the complete actual source ReadSet from this result.
func (c *SystemServiceClient) CheckDevelopWorkflowResultRead(ctx context.Context, credential string, targets []plugin.EngineCatalogPath) error {
	return c.checkSourceRead(ctx, credential, targets, "/api/v1/system/engine-access/read-checks/develop-workflow-result", false)
}
