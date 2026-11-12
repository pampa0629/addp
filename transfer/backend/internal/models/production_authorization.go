package models

import (
	"encoding/json"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/production"
)

const ProductionAuthorizationMetadataKey = "initial_authorization"

// A one-time development-history retirement, not a source access decision.
const ProductionAuthorizationRetired = "authorization_history_retired"

// Delivery status only. System's immutable outcome and canonical Grant remain
// authoritative; no source rules, actor credentials or access verdicts here.
type ProductionAuthorization = production.DeliveryState

func DecodeProductionAuthorization(raw any) (ProductionAuthorization, error) {
	var result ProductionAuthorization
	body, err := json.Marshal(raw)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return result, err
	}
	if err := result.Validate("transfer.initial-authorization/v1"); err != nil {
		return result, err
	}
	return result, nil
}

// InitialProductionAuthorization is initialized atomically with the real
// committed target fact. Parent task status is deliberately not an input.
func InitialProductionAuthorization(item *execution.TaskExecution, now time.Time) *ProductionAuthorization {
	if item == nil {
		return nil
	}
	if item.Module != execution.ModuleTransfer || item.TaskType != execution.TaskTypeSync || item.ExecutionBoundary != execution.ExecutionBoundaryBounded || item.Attempt != 1 || item.RetryOfExecutionID != nil {
		return nil
	}
	copy := execution.TaskExecution{TenantID: item.TenantID}
	if (item.TriggeredBy != nil || item.ActorPrincipalID != nil || item.ActorTenantMembershipID != nil || item.IssuedAuthorizationVersion != nil) && execution.CopyActorOrigin(&copy, item) != nil {
		return nil
	}
	receipt, err := DecodeTargetProduction(item.Metadata[TargetProductionMetadataKey])
	if err != nil || receipt.TenantID != item.TenantID || receipt.ExecutionID != item.ExecutionID || receipt.Attempt != item.Attempt || receipt.RetryOfExecutionID != nil || receipt.Phase != TargetProductionCommitted || !receipt.CreatedAndCommitted || receipt.Preparation != plugin.TableTargetCreated {
		return nil
	}
	return &ProductionAuthorization{SchemaVersion: "transfer.initial-authorization/v1", Status: "pending", NextAttemptUnix: now.Unix()}
}
