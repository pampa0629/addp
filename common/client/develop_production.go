package client

import (
	"context"
	"net/http"

	"github.com/addp/common/production"
)

// ReadCommittedProduction carries no User credential or configurable target.
// The configured Develop origin and current Tenant Service credential are the
// only transport authorities; all response identity is checked before use.
func (c *DevelopClient) ReadCommittedProduction(ctx context.Context, tenant uint, ref production.Reference) (*production.Receipt, error) {
	if c == nil || tenant == 0 || ref.Validate() != nil || ref.Owner != "develop" {
		return nil, production.ErrEvidence
	}
	var result production.Receipt
	path := "/api/v1/develop/runtime/productions/" + ref.ExecutionID + "/" + ref.ResultID + "/receipt"
	if err := c.WithTenantID(tenant).doStrictJSON(ctx, http.MethodGet, path, &result, 256<<10); err != nil {
		return nil, err
	}
	if result.Validate() != nil || result.Reference != ref || result.OperationID != ref.ResultID || result.TenantID != int64(tenant) {
		return nil, production.ErrEvidence
	}
	return &result, nil
}
