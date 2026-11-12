package models

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"

	"github.com/google/uuid"
)

const ProductionAuthorizationMetadataKey = "initial_authorization"

// A one-time development-history retirement, not a source access decision.
const ProductionAuthorizationRetired = "authorization_history_retired"

// Delivery status only. System's immutable outcome and canonical Grant remain
// authoritative; no source rules, actor credentials or access verdicts here.
type ProductionAuthorization struct {
	SchemaVersion   string `json:"schema_version"`
	Status          string `json:"status"`
	Code            string `json:"code,omitempty"`
	Attempts        int    `json:"attempts"`
	NextAttemptUnix int64  `json:"next_attempt_unix"`
	GrantRequestID  string `json:"grant_request_id,omitempty"`
}

func DecodeProductionAuthorization(raw any) (ProductionAuthorization, error) {
	var result ProductionAuthorization
	body, err := json.Marshal(raw)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return result, err
	}
	if result.SchemaVersion != "transfer.initial-authorization/v1" || result.Attempts < 0 || result.NextAttemptUnix < 0 {
		return result, errors.New("invalid initial authorization continuation")
	}
	switch result.Status {
	case "pending", "denied":
		if result.GrantRequestID != "" {
			return result, errors.New("unexpected initial Grant reference")
		}
	case "issued":
		id, err := uuid.Parse(result.GrantRequestID)
		if err != nil || id == uuid.Nil || id.String() != result.GrantRequestID {
			return result, errors.New("invalid initial Grant reference")
		}
	default:
		return result, errors.New("invalid initial authorization status")
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
	if execution.CopyActorOrigin(&copy, item) != nil {
		return nil
	}
	receipt, err := DecodeTargetProduction(item.Metadata[TargetProductionMetadataKey])
	if err != nil || receipt.TenantID != item.TenantID || receipt.ExecutionID != item.ExecutionID || receipt.Attempt != item.Attempt || receipt.RetryOfExecutionID != nil || receipt.Phase != TargetProductionCommitted || !receipt.CreatedAndCommitted || receipt.Preparation != plugin.TableTargetCreated {
		return nil
	}
	return &ProductionAuthorization{SchemaVersion: "transfer.initial-authorization/v1", Status: "pending", NextAttemptUnix: now.Unix()}
}
