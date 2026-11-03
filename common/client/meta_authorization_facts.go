package client

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/addp/common/dataitem/authorizationfacts"
)

// ReadAuthorizationFacts is the exact, bounded and uncached Meta owner contract.
func (c *MetaClient) ReadAuthorizationFacts(ctx context.Context, tenantID uint, fingerprints []string) (*authorizationfacts.AuthorizationFactsBatch, error) {
	query := authorizationfacts.AuthorizationFactsQuery{Fingerprints: fingerprints}
	if c == nil || c.httpClient == nil || c.baseURL == "" || tenantID == 0 || query.Validate() != nil {
		return nil, errors.New("invalid Meta authorization facts request")
	}
	encoded, err := c.postMetaOwnerQuery(ctx, tenantID, "/api/v1/meta/runtime/data-items/authorization-facts/query", query, authorizationfacts.MaxAuthorizationFactsBatchBytes, 24)
	if err != nil {
		return nil, err
	}
	var result authorizationfacts.AuthorizationFactsBatch
	if json.Unmarshal(encoded, &result) != nil || result.Validate(query) != nil {
		return nil, errors.New("invalid Meta authorization facts response")
	}
	return &result, nil
}
