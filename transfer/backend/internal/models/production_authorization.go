package models

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

const ProductionAuthorizationMetadataKey = "initial_authorization"

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
