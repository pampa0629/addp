package client

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

// DevelopQueryReadRequest carries Provider facts, never a caller-selected
// permission or principal. ReadSet must be non-nil even for proven empty reads.
// SampleCredential is used only in the current synchronous sample request;
// callers must never persist or log it.
type DevelopQueryReadRequest struct {
	AuthorizationID  string               `json:"authorization_id"`
	ExecutionID      string               `json:"execution_id"`
	EngineID         string               `json:"engine_id"`
	EngineVersion    string               `json:"engine_version"`
	ReadSet          *plugin.QueryReadSet `json:"read_set"`
	Attempt          int                  `json:"attempt,omitempty"`
	LeaseToken       string               `json:"lease_token,omitempty"`
	SampleCredential string               `json:"sample_credential,omitempty"`
}

func (r DevelopQueryReadRequest) Validate() error {
	invalid := errors.New("invalid Develop query read observation")
	for _, value := range []string{r.AuthorizationID, r.EngineID, r.EngineVersion} {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
			return invalid
		}
	}
	id, err := uuid.Parse(r.ExecutionID)
	if err != nil || id == uuid.Nil || id.String() != r.ExecutionID || r.ReadSet == nil || r.ReadSet.Paths == nil || len(r.ReadSet.Paths) > 200 {
		return invalid
	}
	if r.SampleCredential != "" {
		if r.Attempt != 0 || r.LeaseToken != "" || !strings.HasPrefix(r.SampleCredential, "addp_at_") || r.SampleCredential == "addp_at_" || strings.ContainsAny(r.SampleCredential, " \t\r\n") {
			return invalid
		}
	} else {
		lease, err := uuid.Parse(r.LeaseToken)
		if err != nil || lease == uuid.Nil || lease.String() != r.LeaseToken || r.Attempt <= 0 {
			return invalid
		}
	}
	seen := make(map[string]bool, len(r.ReadSet.Paths))
	for _, path := range r.ReadSet.Paths {
		if strconv.FormatUint(uint64(path.EngineID), 10) != r.EngineID || len(path.Segments) != 3 || path.Segments[1].Term != plugin.EngineCatalogTermSchema || path.Segments[2].Term != plugin.EngineCatalogTermTable {
			return invalid
		}
		encoded, err := authorization.EncodeSharingTarget(path)
		if err != nil || seen[string(encoded)] {
			return invalid
		}
		seen[string(encoded)] = true
	}
	return nil
}

// ExpiresAt bounds this observation, not permission validity for a later read.
// Each next batch and delivery must make a fresh observation.
type DevelopQueryReadObservation struct {
	ObservedAt time.Time `json:"observed_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

var (
	ErrDevelopQueryReadDenied      = errors.New("Develop query source read is not permitted")
	ErrDevelopQueryReadUnavailable = errors.New("Develop query source authorization is unavailable")
)

func (c *SystemServiceClient) CheckDevelopQueryRead(ctx context.Context, request DevelopQueryReadRequest) (*DevelopQueryReadObservation, error) {
	if request.Validate() != nil {
		return nil, ErrDevelopQueryReadDenied
	}
	if c == nil || c.tenantID == nil || c.httpClient == nil {
		return nil, ErrDevelopQueryReadUnavailable
	}
	token, err := c.TenantServiceAccessToken(ctx, *c.tenantID)
	if err != nil {
		return nil, ErrDevelopQueryReadUnavailable
	}
	bound, transport := *c, *c.httpClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	bound.httpClient = &transport
	var result DevelopQueryReadObservation
	// Do not retry a POST or forward a sample credential to a redirect target.
	status, err := bound.doJSON(ctx, http.MethodPost, "/api/v1/system/engine-access/read-observations/develop-query", token, request, &result, 4096)
	if status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound {
		return nil, ErrDevelopQueryReadDenied
	}
	if err != nil || status != http.StatusOK || result.ObservedAt.IsZero() || !result.ExpiresAt.After(result.ObservedAt) {
		return nil, ErrDevelopQueryReadUnavailable
	}
	return &result, nil
}
