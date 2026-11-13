package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

// ManagerPGReadCheckRequest is a synchronous Manager resource handoff, not an
// execution or a reusable access decision. Never log or persist UserCredential.
type ManagerPGReadCheckRequest struct {
	UserCredential string                       `json:"user_credential"`
	Sources        []authorizationfacts.Binding `json:"sources"`
}

func (r ManagerPGReadCheckRequest) Validate() error {
	if r.UserCredential == "addp_at_" || r.UserCredential == "addp_dat_" || r.UserCredential == "" || strings.ContainsAny(r.UserCredential, " \t\r\n") ||
		!(strings.HasPrefix(r.UserCredential, "addp_at_") || strings.HasPrefix(r.UserCredential, "addp_dat_")) ||
		len(r.Sources) == 0 || len(r.Sources) > 200 {
		return errors.New("invalid Manager PG read handoff")
	}
	seen := map[string]bool{}
	for _, source := range r.Sources {
		p := source.NativeTarget
		if p.EngineID != r.Sources[0].NativeTarget.EngineID {
			return errors.New("Manager PG sources must use one Engine")
		}
		if source.Validate() != nil || source.FileComposition != nil || source.Scope != nil || source.MetadataTarget != nil ||
			len(p.Segments) != 3 || p.Segments[1].Term != plugin.EngineCatalogTermSchema || p.Segments[2].Term != plugin.EngineCatalogTermTable || seen[source.ItemFingerprint] {
			return errors.New("invalid or duplicate Manager PG source")
		}
		seen[source.ItemFingerprint] = true
	}
	return nil
}

func (c *SystemServiceClient) CheckManagerPGPreviewRead(ctx context.Context, tenantID uint, credential string, sources []authorizationfacts.Binding) error {
	return c.WithTenantID(tenantID).checkManagerPGRead(ctx, credential, sources, "/api/v1/system/engine-access/read-checks/manager-pg-preview")
}

func (c *SystemServiceClient) CheckManagerPGProfileResultRead(ctx context.Context, tenantID uint, credential string, sources []authorizationfacts.Binding) error {
	return c.WithTenantID(tenantID).checkManagerPGRead(ctx, credential, sources, "/api/v1/system/engine-access/read-checks/manager-pg-profile-result")
}

func (c *SystemServiceClient) checkManagerPGRead(ctx context.Context, credential string, sources []authorizationfacts.Binding, path string) error {
	request := ManagerPGReadCheckRequest{UserCredential: credential, Sources: sources}
	if request.Validate() != nil {
		return ErrManagerPreviewReadDenied
	}
	if c == nil || c.tenantID == nil || c.httpClient == nil {
		return ErrManagerPreviewReadUnavailable
	}
	token, err := c.TenantServiceAccessToken(ctx, *c.tenantID)
	if err != nil {
		return ErrManagerPreviewReadUnavailable
	}
	bound := *c
	transport := *c.httpClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	bound.httpClient = &transport
	var result struct {
		ObservedAt time.Time `json:"observed_at"`
	}
	// Do not use doTenantJSON: a failed POST must not be implicitly repeated.
	status, err := bound.doJSON(ctx, http.MethodPost, path, token, request, &result, 4096)
	if status == http.StatusUnauthorized {
		return ErrManagerPreviewCredentialRejected
	}
	if status == http.StatusForbidden {
		return ErrManagerPreviewReadDenied
	}
	if err != nil || status != http.StatusOK || result.ObservedAt.IsZero() {
		return ErrManagerPreviewReadUnavailable
	}
	return nil
}
