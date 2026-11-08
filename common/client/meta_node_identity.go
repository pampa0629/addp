package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/jsonmap"
)

// ResolveNodeIdentity reads one current owner fact; it never scans or authorizes.
func (c *MetaClient) ResolveNodeIdentity(ctx context.Context, tenantID uint, query authorizationfacts.NodeIdentityQuery) (*authorizationfacts.NodeIdentity, error) {
	if c == nil || c.httpClient == nil || c.baseURL == "" || tenantID == 0 || query.Validate() != nil {
		return nil, errors.New("invalid Meta node identity query")
	}
	encoded, err := c.metaOwnerQuery(ctx, tenantID, http.MethodPost, "/api/v1/meta/runtime/node-identities/resolve", query, authorizationfacts.MaxNodeIdentityBytes, 8)
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

// QueryNodeIdentities is one bounded, non-retrying child-node identity query.
func (c *MetaClient) QueryNodeIdentities(ctx context.Context, tenantID uint, query authorizationfacts.NodeIdentityPageQuery) (*authorizationfacts.NodeIdentityPage, error) {
	if c == nil || c.httpClient == nil || c.baseURL == "" || tenantID == 0 || query.Validate() != nil {
		return nil, errors.New("invalid Meta child node query")
	}
	encoded, err := c.metaOwnerQuery(ctx, tenantID, http.MethodPost, "/api/v1/meta/runtime/node-identities/query", query, authorizationfacts.MaxNodeIdentityPageBytes, 8)
	if err != nil {
		return nil, err
	}
	shape, err := jsonmap.DecodeObjectStrict(encoded, authorizationfacts.MaxNodeIdentityPageBytes, 8)
	if err != nil || len(shape) != 5 || !validNodeIdentityShape(shape["parent"]) {
		return nil, errors.New("invalid Meta child node response")
	}
	nodes, ok := shape["nodes"].([]any)
	if !ok {
		return nil, errors.New("invalid Meta child node list")
	}
	for _, node := range nodes {
		if !validNodeIdentityShape(node) {
			return nil, errors.New("invalid Meta child node observation")
		}
	}
	var result authorizationfacts.NodeIdentityPage
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || result.Validate(tenantID, query) != nil {
		return nil, errors.New("invalid Meta child node response")
	}
	return &result, nil
}
func validNodeIdentityShape(value any) bool {
	object, ok := value.(map[string]any)
	if !ok || len(object) != 3 {
		return false
	}
	_, ok = object["exists"].(bool)
	return ok
}
