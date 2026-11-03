package client

import (
	"context"
	"errors"
	"net/http"

	"github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
)

func (c *SystemServiceClient) IssueInitialUploadGrant(ctx context.Context, id, fingerprint string) (*authorization.UploadInitialResult, error) {
	if !nonzeroExecutionUUID(id) || !authorizationfacts.ValidItemFingerprint(fingerprint) {
		return nil, errors.New("invalid upload initial Grant request")
	}
	transport, err := c.productionGrantHTTP()
	if err != nil {
		return nil, err
	}
	var result authorization.UploadInitialResult
	if err := transport.doStrictJSON(ctx, http.MethodPost, "/api/v1/system/runtime/initial-upload-grants/"+id+"/items/"+fingerprint, &result, 16<<10); err != nil {
		return nil, err
	}
	if result.Validate() != nil || result.TenantID != int64(*c.tenantID) || result.UploadID != id || result.ItemFingerprint != fingerprint {
		return nil, errors.New("upload initial Grant identity mismatch")
	}
	return &result, nil
}
