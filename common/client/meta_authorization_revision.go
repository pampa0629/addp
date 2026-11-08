package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/addp/common/dataitem/authorizationfacts"
)

func (c *MetaClient) ReadAuthorizationBasisRevision(ctx context.Context, tenantID uint) (*authorizationfacts.Revision, error) {
	if c == nil || c.httpClient == nil || c.baseURL == "" || tenantID == 0 {
		return nil, errors.New("invalid Meta authorization revision request")
	}
	encoded, err := c.metaOwnerQuery(ctx, tenantID, http.MethodGet, "/api/v1/meta/runtime/data-items/authorization-facts/revision", nil, 4096, 4)
	if err != nil {
		return nil, err
	}
	var result authorizationfacts.Revision
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || result.Validate() != nil {
		return nil, errors.New("invalid Meta authorization revision response")
	}
	return &result, nil
}
