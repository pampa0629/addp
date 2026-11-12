package service

import (
	"context"
	"time"

	"github.com/addp/common/client"
)

// One bounded delivery step joins the existing Supervisor recovery cycle.
// It cannot dispatch Runtime work or invoke a publication Provider.
func (f *privateFileWorkflowExecutor) continueProductionAuthorization(ctx context.Context) error {
	if f.productionRuntime == nil {
		return nil
	}
	step, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	claim, err := f.repository.ClaimProductionAuthorization(step, time.Now().UTC())
	if err != nil || claim == nil {
		return err
	}
	state := claim.State
	result, code, deliveryErr := client.ContinueInitialProduction(step, claim.TenantID, claim.Reference, f.productionRuntime(claim.TenantID), f.productionScanner)
	if deliveryErr == nil && result != nil {
		state.Status, state.Code = result.Outcome, result.DenialCode
		if result.Grant != nil {
			state.GrantRequestID = result.Grant.RequestID.String()
		}
	} else {
		state.Code = code
	}
	return f.repository.RecordProductionAuthorization(step, *claim, state)
}
