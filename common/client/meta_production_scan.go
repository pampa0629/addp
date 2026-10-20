package client

import (
	"context"
	"errors"
	"net/http"

	"github.com/addp/common/execution"
)

// A minimal technical scan receipt, not data access or a task configuration.
type MetaProductionBasicScan struct {
	ProductionExecutionID string `json:"production_execution_id"`
	ScanExecutionID       string `json:"scan_execution_id"`
	Status                string `json:"status"`
}

func (r MetaProductionBasicScan) Validate() error {
	if !nonzeroExecutionUUID(r.ProductionExecutionID) || !nonzeroExecutionUUID(r.ScanExecutionID) || r.ProductionExecutionID == r.ScanExecutionID {
		return errors.New("invalid production scan identity")
	}
	switch r.Status {
	case execution.ExecutionStatusPending, execution.ExecutionStatusRunning, execution.ExecutionStatusSuccess, execution.ExecutionStatusFailed, execution.ExecutionStatusTimeout, execution.ExecutionStatusCancelled:
		return nil
	}
	return errors.New("invalid production scan status")
}

func (c *MetaClient) EnsureTransferProductionBasicScan(ctx context.Context, tenantID uint, id string) (*MetaProductionBasicScan, error) {
	if c == nil || tenantID == 0 || !nonzeroExecutionUUID(id) {
		return nil, errors.New("invalid production scan request")
	}
	var result MetaProductionBasicScan
	transport := newTenantHTTPClient(c.baseURL, c.serviceTokenSource, c.httpClient).withTenantID(tenantID)
	if err := transport.doStrictJSON(ctx, http.MethodPost, "/api/v1/meta/runtime/transfer-productions/"+id+"/basic-scan", &result, 16<<10); err != nil {
		return nil, err
	}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	if result.ProductionExecutionID != id {
		return nil, errors.New("production scan origin mismatch")
	}
	return &result, nil
}
