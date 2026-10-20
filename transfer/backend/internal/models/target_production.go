package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	plugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
)

const TargetProductionMetadataKey = "target_production"

const (
	TargetProductionSchema    = "transfer.target-production/v3"
	TargetProductionPending   = "pending"
	TargetProductionPrepared  = "prepared"
	TargetProductionCommitted = "committed"
)

// TargetProduction is private execution provenance, never an authorization or
// a public execution output. A created preparation alone does not prove an
// initial business DataItem or a successful execution.
type TargetProduction struct {
	SchemaVersion       string                        `json:"schema_version"`
	TenantID            int                           `json:"tenant_id"`
	ExecutionID         string                        `json:"execution_id"`
	Attempt             int                           `json:"attempt"`
	RetryOfExecutionID  *string                       `json:"retry_of_execution_id,omitempty"`
	TargetLocator       string                        `json:"target_locator"`
	Phase               string                        `json:"phase"`
	Preparation         plugin.TableTargetDisposition `json:"preparation,omitempty"`
	StartedAt           time.Time                     `json:"started_at"`
	PreparedAt          *time.Time                    `json:"prepared_at,omitempty"`
	CommittedAt         *time.Time                    `json:"committed_at,omitempty"`
	SourceReadSet       *plugin.QueryReadSet          `json:"source_read_set"`
	CreatedAndCommitted bool                          `json:"created_and_committed"`
}

func DecodeTargetProduction(value interface{}) (TargetProduction, error) {
	var result TargetProduction
	data, ok := value.(string)
	if !ok || len(data) > 256<<10 || !utf8.ValidString(data) {
		return result, fmt.Errorf("invalid target production record")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("invalid target production record: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return result, fmt.Errorf("trailing target production record")
	}
	if result.SchemaVersion != TargetProductionSchema || result.TenantID <= 0 || result.ExecutionID == "" || result.Attempt <= 0 || result.TargetLocator == "" || result.StartedAt.IsZero() ||
		(result.RetryOfExecutionID != nil && (*result.RetryOfExecutionID == "" || *result.RetryOfExecutionID == result.ExecutionID)) {
		return result, fmt.Errorf("incomplete target production identity")
	}
	switch result.Phase {
	case TargetProductionPending:
		if result.Preparation != "" || result.PreparedAt != nil || result.CommittedAt != nil {
			return result, fmt.Errorf("pending production has outcome facts")
		}
	case TargetProductionPrepared, TargetProductionCommitted:
		if result.Preparation != plugin.TableTargetCreated && result.Preparation != plugin.TableTargetRecreated && result.Preparation != plugin.TableTargetReused {
			return result, fmt.Errorf("unknown target preparation")
		}
		if result.PreparedAt == nil || result.PreparedAt.IsZero() || result.PreparedAt.Before(result.StartedAt) {
			return result, fmt.Errorf("invalid preparation time")
		}
		if result.Phase == TargetProductionPrepared && result.CommittedAt != nil {
			return result, fmt.Errorf("prepared production claims commit")
		}
		if result.Phase == TargetProductionCommitted && (result.CommittedAt == nil || result.CommittedAt.IsZero() || result.CommittedAt.Before(*result.PreparedAt)) {
			return result, fmt.Errorf("invalid commit time")
		}
	default:
		return result, fmt.Errorf("unknown production phase")
	}
	if result.CreatedAndCommitted && (result.Phase != TargetProductionCommitted || result.Preparation != plugin.TableTargetCreated) {
		return result, fmt.Errorf("creation commit evidence contradicts production phase or operation")
	}
	if execution.ValidateProductionReadSet(result.SourceReadSet) != nil || (result.Phase != TargetProductionCommitted && result.SourceReadSet != nil) {
		return result, fmt.Errorf("invalid committed production sources")
	}
	return result, nil
}

// Store the sealed owner record as one JSON string inside JSONMap. JSONMap's
// general decoder uses float64; round-tripping typed path IDs through an object
// would silently change large Engine IDs before System consumes the evidence.
func EncodeTargetProduction(record TargetProduction) (string, error) {
	encoded, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	value := string(encoded)
	if _, err := DecodeTargetProduction(value); err != nil {
		return "", err
	}
	return value, nil
}
