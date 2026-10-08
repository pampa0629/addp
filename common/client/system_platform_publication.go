package client

import (
	"context"
	"net/http"
	"time"

	"github.com/addp/common/authorization"
)

// CheckPlatformPublication performs a fresh System IAM check using only the
// client's Platform Service identity. It does not publish or issue a credential.
func (c *SystemServiceClient) CheckPlatformPublication(ctx context.Context, binding authorization.PlatformPublicationCheck) (*authorization.PlatformPublicationObservation, error) {
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result authorization.PlatformPublicationObservation
	if err := c.doPlatformJSONBounded(ctx, http.MethodPost, "/api/v1/system/runtime/platform-definition-publication-checks", binding, &result, authorization.PlatformPublicationCheckByteLimit); err != nil {
		return nil, err
	}
	if err := result.Validate(binding); err != nil {
		return nil, err
	}
	return &result, nil
}
