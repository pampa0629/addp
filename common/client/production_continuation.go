package client

import (
	"context"

	"github.com/addp/common/authorization"
	"github.com/addp/common/production"
)

type ProductionGrantRuntime interface {
	ResolveInitialProductionGrant(context.Context, production.Reference) (*authorization.InitialProductionLookup, error)
	IssueInitialProductionGrant(context.Context, production.Reference) (*authorization.InitialProductionResult, error)
}

type ProductionScanner interface {
	EnsureProductionBasicScan(context.Context, uint, production.Reference) (*MetaProductionIdentification, error)
}

// ContinueInitialProduction is the sole cross-owner delivery sequence. The
// caller persists only delivery state; System remains the terminal authority.
func ContinueInitialProduction(ctx context.Context, tenant uint, ref production.Reference, runtime ProductionGrantRuntime, scanner ProductionScanner) (*authorization.InitialProductionResult, string, error) {
	if tenant == 0 || ref.Validate() != nil || runtime == nil {
		return nil, "system_unavailable", production.ErrEvidence
	}
	lookup, err := runtime.ResolveInitialProductionGrant(ctx, ref)
	if err != nil || lookup == nil || lookup.Validate() != nil {
		if err == nil {
			err = production.ErrEvidence
		}
		return nil, "system_unavailable", err
	}
	result := lookup.Result
	if !lookup.Found {
		result, err = runtime.IssueInitialProductionGrant(ctx, ref)
		if err != nil {
			code, _ := TenantAPIErrorCode(err)
			if status, ok := TenantAPIStatusCode(err); ok && status == 503 && code == "initial_production_pending" && scanner != nil {
				scan, scanErr := scanner.EnsureProductionBasicScan(ctx, tenant, ref)
				if scanErr == nil && scan != nil && scan.Validate() == nil && scan.Reference == ref && scan.TenantID == int64(tenant) {
					if scan.Status == "failed" || scan.Status == "timeout" || scan.Status == "cancelled" {
						return nil, "scan_failed", err
					}
					return nil, "scan_pending", err
				}
			}
			return nil, "authorization_pending", err
		}
	}
	if result == nil || result.Validate() != nil || result.TenantID != int64(tenant) || result.ProducerOwner != ref.Owner || result.ExecutionID.String() != ref.ExecutionID || result.ResultID.String() != ref.ResultID {
		return nil, "authorization_pending", production.ErrEvidence
	}
	return result, result.DenialCode, nil
}
