package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/addp/common/authorization"
	plugin "github.com/addp/common/engine/plugin"
)

// TransferProductionReceipt is owner evidence, never an Allow or an assertion
// of initial-Grant eligibility. System must independently resolve Meta and IAM.
type TransferProductionReceipt struct {
	SchemaVersion      string                        `json:"schema_version"`
	TenantID           int64                         `json:"tenant_id,string" swaggertype:"string"`
	ExecutionID        string                        `json:"execution_id"`
	Attempt            int                           `json:"attempt"`
	RetryOfExecutionID *string                       `json:"retry_of_execution_id,omitempty"`
	Target             plugin.EngineCatalogPath      `json:"target"`
	Preparation        plugin.TableTargetDisposition `json:"preparation"`
	CommittedAt        time.Time                     `json:"committed_at"`
	UserOrigin         *TransferProductionUserOrigin `json:"user_origin"`
}

type TransferProductionUserOrigin struct {
	PrincipalID          int64 `json:"principal_id,string" swaggertype:"string"`
	TenantMembershipID   int64 `json:"tenant_membership_id,string" swaggertype:"string"`
	AuthorizationVersion int64 `json:"authorization_version,string" swaggertype:"string"`
}

const TransferProductionReceiptSchema = "transfer.production-receipt/v1"

func (r TransferProductionReceipt) Validate() error {
	if r.SchemaVersion != TransferProductionReceiptSchema || r.TenantID <= 0 || !nonzeroExecutionUUID(r.ExecutionID) || r.Attempt <= 0 || r.CommittedAt.IsZero() {
		return errors.New("invalid Transfer production receipt identity")
	}
	if r.RetryOfExecutionID != nil && (!nonzeroExecutionUUID(*r.RetryOfExecutionID) || *r.RetryOfExecutionID == r.ExecutionID) {
		return errors.New("invalid Transfer production retry origin")
	}
	if _, err := authorization.EncodeSharingTarget(r.Target); err != nil || len(r.Target.Segments) != 3 || r.Target.Segments[1].Term != plugin.EngineCatalogTermSchema || r.Target.Segments[2].Kind != plugin.EngineCatalogKindTable {
		return errors.New("invalid PG production target")
	}
	if r.Preparation != plugin.TableTargetCreated && r.Preparation != plugin.TableTargetRecreated && r.Preparation != plugin.TableTargetReused {
		return errors.New("invalid Transfer production preparation")
	}
	if r.UserOrigin != nil && (r.UserOrigin.PrincipalID <= 0 || r.UserOrigin.TenantMembershipID <= 0 || r.UserOrigin.AuthorizationVersion <= 0) {
		return errors.New("incomplete Transfer production user origin")
	}
	return nil
}

// ReadProductionReceipt uses only this client's tenant Service credential and
// configured origin; no user token, redirect, arbitrary owner URL or cache.
func (c *TransferClient) ReadProductionReceipt(ctx context.Context, tenantID uint, executionID string) (*TransferProductionReceipt, error) {
	if c == nil || c.httpClient == nil || c.baseURL == "" || c.tokenSource == nil || tenantID == 0 || !nonzeroExecutionUUID(executionID) {
		return nil, errors.New("invalid Transfer production receipt request")
	}
	path := "/api/v1/transfer/runtime/executions/" + executionID + "/production-receipt"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	token, err := c.tokenSource.Token(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, errors.New("Transfer production receipt requires a Service credential")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	transport := *c.httpClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := transport.Do(req)
	if err != nil {
		return nil, &TenantTransportError{Method: req.Method, Path: path, Cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &TenantAPIError{Method: req.Method, Path: path, StatusCode: response.StatusCode}
	}
	const limit = 16 << 10
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(body) > limit || !utf8.Valid(body) {
		return nil, errors.New("invalid Transfer production receipt response size")
	}
	var result TransferProductionReceipt
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.Validate() != nil || result.ExecutionID != executionID || result.TenantID != int64(tenantID) {
		return nil, fmt.Errorf("invalid Transfer production receipt response")
	}
	return &result, nil
}
