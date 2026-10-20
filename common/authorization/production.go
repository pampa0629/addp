package authorization

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const InitialProductionResultSchema = "addp.initial_production_result/v1"

// Issuance history only, never current readability or permission to execute.
type InitialProductionGrant struct {
	RequestID  uuid.UUID  `json:"request_id"`
	GrantedAt  time.Time  `json:"granted_at"`
	ExpiryMode string     `json:"expiry_mode"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

type InitialProductionResult struct {
	SchemaVersion string                  `json:"schema_version"`
	TenantID      int64                   `json:"tenant_id,string" swaggertype:"string"`
	ProducerOwner string                  `json:"producer_owner"`
	ExecutionID   uuid.UUID               `json:"execution_id"`
	Outcome       string                  `json:"outcome"`
	RecordedAt    time.Time               `json:"recorded_at"`
	Grant         *InitialProductionGrant `json:"grant,omitempty"`
	DenialCode    string                  `json:"denial_code,omitempty"`
}

type InitialProductionLookup struct {
	Found  bool                     `json:"found"`
	Result *InitialProductionResult `json:"result,omitempty"`
}

func ValidInitialProductionDenial(code string) bool {
	switch code {
	case "production_ineligible", "source_access_denied", "execution_permission_denied", "target_history_exists", "engine_changed":
		return true
	}
	return false
}

func (r InitialProductionResult) Validate() error {
	if r.SchemaVersion != InitialProductionResultSchema || r.TenantID <= 0 || r.ProducerOwner != "transfer" || r.ExecutionID == uuid.Nil || r.RecordedAt.IsZero() {
		return errors.New("invalid initial production result identity")
	}
	switch r.Outcome {
	case "denied":
		if r.Grant != nil || !ValidInitialProductionDenial(r.DenialCode) {
			return errors.New("invalid initial production denial")
		}
	case "issued":
		if r.DenialCode != "" || r.Grant == nil || r.Grant.RequestID == uuid.Nil || r.Grant.GrantedAt.IsZero() ||
			!SharingExpiryFuture(r.Grant.ExpiryMode, r.Grant.ExpiresAt, r.Grant.GrantedAt) {
			return errors.New("invalid initial production issuance")
		}
	default:
		return errors.New("unknown initial production terminal outcome")
	}
	return nil
}

func (r InitialProductionLookup) Validate() error {
	if !r.Found && r.Result == nil {
		return nil
	}
	if !r.Found || r.Result == nil {
		return errors.New("inconsistent initial production lookup")
	}
	return r.Result.Validate()
}
