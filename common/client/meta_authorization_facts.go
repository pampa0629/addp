package client

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
)

// MetaAuthorizationFactsTiming contains only durations for one successful,
// synchronous owner query. Transport includes existing token acquisition.
type MetaAuthorizationFactsTiming struct {
	Preparation, HTTP, ResponseRead, StrictJSON, Decode, Validation, Total time.Duration
}

type metaFactsObserverKey struct{}
type metaFactsTimingKey struct{}
type metaFactsTiming struct {
	MetaAuthorizationFactsTiming
	started time.Time
}

// WithMetaAuthorizationFactsTiming observes this context's successful calls
// without changing the request, response validation or retry policy.
func WithMetaAuthorizationFactsTiming(ctx context.Context, observe func(MetaAuthorizationFactsTiming)) context.Context {
	return context.WithValue(ctx, metaFactsObserverKey{}, observe)
}

// ReadAuthorizationFacts is the exact, bounded and uncached Meta owner contract.
func (c *MetaClient) ReadAuthorizationFacts(ctx context.Context, tenantID uint, fingerprints []string) (*authorizationfacts.AuthorizationFactsBatch, error) {
	query := authorizationfacts.AuthorizationFactsQuery{Fingerprints: fingerprints}
	if c == nil || c.httpClient == nil || c.baseURL == "" || tenantID == 0 || query.Validate() != nil {
		return nil, errors.New("invalid Meta authorization facts request")
	}
	observe, _ := ctx.Value(metaFactsObserverKey{}).(func(MetaAuthorizationFactsTiming))
	var timing *metaFactsTiming
	if observe != nil {
		timing = &metaFactsTiming{started: time.Now()}
		ctx = context.WithValue(ctx, metaFactsTimingKey{}, timing)
	}
	encoded, err := c.postMetaOwnerQuery(ctx, tenantID, "/api/v1/meta/runtime/data-items/authorization-facts/query", query, authorizationfacts.MaxAuthorizationFactsBatchBytes, 24)
	if err != nil {
		return nil, err
	}
	var result authorizationfacts.AuthorizationFactsBatch
	var started time.Time
	if timing != nil {
		started = time.Now()
	}
	err = json.Unmarshal(encoded, &result)
	if timing != nil {
		timing.Decode = time.Since(started)
	}
	if err != nil {
		return nil, errors.New("invalid Meta authorization facts response")
	}
	if timing != nil {
		started = time.Now()
	}
	err = result.Validate(query)
	if timing != nil {
		timing.Validation = time.Since(started)
	}
	if err != nil {
		return nil, errors.New("invalid Meta authorization facts response")
	}
	if timing != nil {
		timing.Total = time.Since(timing.started)
		observe(timing.MetaAuthorizationFactsTiming)
	}
	return &result, nil
}
