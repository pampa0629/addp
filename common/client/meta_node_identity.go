package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/jsonmap"
)

// ResolveNodeIdentity reads one current owner fact; it never scans or authorizes.
func (c *MetaClient) ResolveNodeIdentity(ctx context.Context, tenantID uint, query authorizationfacts.NodeIdentityQuery) (*authorizationfacts.NodeIdentity, error) {
	if c == nil || c.httpClient == nil || c.baseURL == "" || tenantID == 0 || query.Validate() != nil {
		return nil, errors.New("invalid Meta node identity query")
	}
	encoded, err := c.postIdentityQuery(ctx, tenantID, "/api/v1/meta/runtime/node-identities/resolve", query, authorizationfacts.MaxNodeIdentityBytes)
	if err != nil {
		return nil, err
	}
	shape, err := jsonmap.DecodeObjectStrict(encoded, authorizationfacts.MaxNodeIdentityBytes, 8)
	if err != nil || len(shape) != 3 {
		return nil, errors.New("invalid Meta node identity response")
	}
	if _, ok := shape["exists"].(bool); !ok {
		return nil, errors.New("invalid Meta node existence observation")
	}
	var result authorizationfacts.NodeIdentity
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || result.Validate(tenantID, query) != nil {
		return nil, errors.New("invalid Meta node identity response")
	}
	return &result, nil
}
