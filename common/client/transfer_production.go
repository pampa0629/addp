package client

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/addp/common/authorization"
	plugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
)

// TransferProductionReceipt is owner evidence, never an Allow or an assertion
// of initial-Grant eligibility. System must independently resolve Meta and IAM.
type TransferProductionReceipt struct {
	SchemaVersion       string                        `json:"schema_version"`
	TenantID            int64                         `json:"tenant_id,string" swaggertype:"string"`
	ExecutionID         string                        `json:"execution_id"`
	Attempt             int                           `json:"attempt"`
	RetryOfExecutionID  *string                       `json:"retry_of_execution_id,omitempty"`
	Target              plugin.EngineCatalogPath      `json:"target"`
	Preparation         plugin.TableTargetDisposition `json:"preparation"`
	CommittedAt         time.Time                     `json:"committed_at"`
	UserOrigin          *TransferProductionUserOrigin `json:"user_origin"`
	SourceReadSet       *plugin.QueryReadSet          `json:"source_read_set"`
	CreatedAndCommitted bool                          `json:"created_and_committed"`
}

type TransferProductionUserOrigin struct {
	PrincipalID          int64 `json:"principal_id,string" swaggertype:"string"`
	TenantMembershipID   int64 `json:"tenant_membership_id,string" swaggertype:"string"`
	AuthorizationVersion int64 `json:"authorization_version,string" swaggertype:"string"`
}

const TransferProductionReceiptSchema = "transfer.production-receipt/v3"

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
	if r.CreatedAndCommitted && r.Preparation != plugin.TableTargetCreated {
		return errors.New("Transfer creation evidence contradicts preparation")
	}
	if r.UserOrigin != nil && (r.UserOrigin.PrincipalID <= 0 || r.UserOrigin.TenantMembershipID <= 0 || r.UserOrigin.AuthorizationVersion <= 0) {
		return errors.New("incomplete Transfer production user origin")
	}
	if err := execution.ValidateProductionReadSet(r.SourceReadSet); err != nil {
		return err
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
	var result TransferProductionReceipt
	transport := newTenantHTTPClient(c.baseURL, c.tokenSource, c.httpClient).withTenantID(tenantID)
	if err := transport.doStrictJSON(ctx, http.MethodGet, path, &result, 256<<10); err != nil {
		return nil, err
	}
	if result.Validate() != nil || result.ExecutionID != executionID || result.TenantID != int64(tenantID) {
		return nil, errors.New("invalid Transfer production receipt response")
	}
	return &result, nil
}
