package client

import (
	"context"
	"errors"
	"net/http"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/execution"
	"github.com/addp/common/production"
)

// MetaProductionIdentification is technical identity/newness evidence, never an
// access verdict. An unfinished or failed scan supplies no binding or new item.
type MetaProductionIdentification struct {
	Reference        production.Reference        `json:"reference"`
	TenantID         int64                       `json:"tenant_id,string" swaggertype:"string"`
	ScanExecutionID  string                      `json:"scan_execution_id"`
	Status           string                      `json:"status"`
	EngineVersion    int64                       `json:"engine_version,string" swaggertype:"string"`
	Binding          *authorizationfacts.Binding `json:"binding,omitempty"`
	LogicalItemState string                      `json:"logical_item_state,omitempty"`
}

func (r MetaProductionIdentification) Validate() error {
	if r.Reference.Validate() != nil || r.TenantID <= 0 || r.EngineVersion <= 0 || !nonzeroExecutionUUID(r.ScanExecutionID) || r.ScanExecutionID == r.Reference.ExecutionID {
		return errors.New("invalid production identification")
	}
	switch r.Status {
	case execution.ExecutionStatusSuccess:
		if r.Binding == nil || r.Binding.Validate() != nil || (r.LogicalItemState != "new" && r.LogicalItemState != "existing") {
			return errors.New("incomplete production identity")
		}
	case execution.ExecutionStatusPending, execution.ExecutionStatusRunning, execution.ExecutionStatusFailed, execution.ExecutionStatusTimeout, execution.ExecutionStatusCancelled:
		if r.Binding != nil || r.LogicalItemState != "" {
			return errors.New("unfinished production identification claimed a result")
		}
	default:
		return errors.New("invalid production scan status")
	}
	return nil
}
func (c *MetaClient) productionIdentification(ctx context.Context, tenant uint, ref production.Reference, method string) (*MetaProductionIdentification, error) {
	if c == nil || tenant == 0 || ref.Validate() != nil {
		return nil, errors.New("invalid production identification request")
	}
	var result MetaProductionIdentification
	transport := newTenantHTTPClient(c.baseURL, c.serviceTokenSource, c.httpClient).withTenantID(tenant)
	path := "/api/v1/meta/runtime/productions/" + ref.Owner + "/" + ref.ExecutionID + "/" + ref.ResultID + "/basic-scan"
	if err := transport.doStrictJSON(ctx, method, path, &result, 256<<10); err != nil {
		return nil, err
	}
	if result.Validate() != nil || result.TenantID != int64(tenant) || result.Reference != ref {
		return nil, errors.New("production identification identity mismatch")
	}
	return &result, nil
}
func (c *MetaClient) EnsureProductionBasicScan(ctx context.Context, tenant uint, ref production.Reference) (*MetaProductionIdentification, error) {
	return c.productionIdentification(ctx, tenant, ref, http.MethodPost)
}
func (c *MetaClient) ReadProductionIdentification(ctx context.Context, tenant uint, ref production.Reference) (*MetaProductionIdentification, error) {
	return c.productionIdentification(ctx, tenant, ref, http.MethodGet)
}
