package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/addp/common/dataitem/authorizationfacts"
)

// QueryDataItemIdentities is one bounded read-only owner query. Never retries
// POST or follows redirects; an unavailable owner is not an empty inventory.
func (c *MetaClient) QueryDataItemIdentities(ctx context.Context, tenantID uint, query authorizationfacts.IdentityQuery) (*authorizationfacts.IdentityPage, error) {
	if c == nil || c.httpClient == nil || c.baseURL == "" || tenantID == 0 || query.Validate() != nil {
		return nil, errors.New("invalid Meta identity query")
	}
	const path = "/api/v1/meta/runtime/data-item-identities/query"
	encoded, err := c.postIdentityQuery(ctx, tenantID, path, query, authorizationfacts.MaxIdentityQueryBytes)
	if err != nil {
		return nil, err
	}
	var result authorizationfacts.IdentityPage
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || result.Validate(query) != nil {
		return nil, errors.New("invalid Meta identity response")
	}
	return &result, nil
}
