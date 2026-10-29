package models

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/addp/common/events"
	"github.com/addp/common/execution"
)

// CleanupRequestRecord is System's durable acceptance and frozen cutoff. Owner
// professional output never belongs here. Protocol bytes are kept as text so
// JSONB number conversion cannot change the signed request.
type CleanupRequestRecord struct {
	RequestID          string `gorm:"column:request_id;primaryKey"`
	TenantID           uint
	RequestedBy        uint
	ParentExecutionID  string
	IntentDigest       string
	RequestedScope     string
	SourceOperationID  string
	Action             string
	Protocol           string
	EngineConfirmation string
	CreatedAt          time.Time
	Cutoff             *string
}

func (CleanupRequestRecord) TableName() string { return "system.cleanup_requests" }

// CleanupEngineConfirmation is private System workflow evidence. It does not
// expand the neutral Owner request or confer authority on its own.
type CleanupEngineConfirmation struct {
	Phase                  string `json:"phase"`
	ExternalArtifactPolicy string `json:"external_artifact_policy"`
	AssessmentID           string `json:"assessment_id,omitempty"`
	ImpactDigest           string `json:"impact_digest,omitempty"`
}

type CleanupCutoff struct {
	ObservedAt time.Time                  `json:"observed_at"`
	Status     string                     `json:"status"`
	Receipts   []execution.CleanupReceipt `json:"receipts"`
	Missing    []string                   `json:"missing"`
}

func (r CleanupRequestRecord) Input() (events.CleanupRequest, error) {
	var input events.CleanupRequest
	if err := events.CleanupStrictDecode([]byte(r.Protocol), &input); err != nil {
		return input, err
	}
	if err := input.Validate(); err != nil {
		return input, err
	}
	if input.RequestID != r.RequestID || input.TenantID != r.TenantID || input.RequestedBy != r.RequestedBy || input.ParentExecutionID != r.ParentExecutionID || input.SourceOperationID != r.SourceOperationID || input.Action != r.Action {
		return input, fmt.Errorf("cleanup private request binding is invalid")
	}
	var scope []string
	if events.CleanupStrictDecode([]byte(r.RequestedScope), &scope) != nil || scope == nil || !sort.StringsAreSorted(scope) || len(scope) > 10 {
		return input, fmt.Errorf("invalid cleanup submitted scope")
	}
	for i, m := range scope {
		if !events.CleanupModuleValid(m) || i > 0 && scope[i-1] == m {
			return input, fmt.Errorf("invalid cleanup submitted scope")
		}
	}
	canonical, _ := json.Marshal(scope)
	if string(canonical) != r.RequestedScope {
		return input, fmt.Errorf("noncanonical cleanup submitted scope")
	}
	if r.EngineConfirmation != "" {
		var evidence CleanupEngineConfirmation
		if events.CleanupStrictDecode([]byte(r.EngineConfirmation), &evidence) != nil || input.CauseEvent != events.CleanupCauseEngineDeleting || input.Context.EngineID == 0 || (evidence.ExternalArtifactPolicy != "delete" && evidence.ExternalArtifactPolicy != "abandon") {
			return input, fmt.Errorf("invalid cleanup Engine confirmation")
		}
		switch evidence.Phase {
		case "preflight":
			if evidence.AssessmentID != "" || evidence.ImpactDigest != "" || input.Action != events.CleanupActionScan {
				return input, fmt.Errorf("invalid preflight confirmation")
			}
		case "validation":
			if evidence.AssessmentID == "" || !events.CleanupDigestValid(evidence.ImpactDigest) {
				return input, fmt.Errorf("invalid confirmed assessment")
			}
		default:
			return input, fmt.Errorf("invalid Engine phase")
		}
	}
	return input, nil
}

func (r CleanupRequestRecord) FrozenCutoff() (*CleanupCutoff, error) {
	if r.Cutoff == nil {
		return nil, nil
	}
	var value CleanupCutoff
	if err := events.CleanupStrictDecode([]byte(*r.Cutoff), &value); err != nil {
		return nil, err
	}
	input, err := r.Input()
	if err != nil {
		return nil, err
	}
	if value.ObservedAt.IsZero() || value.ObservedAt.Before(input.RequestedAt) {
		return nil, fmt.Errorf("invalid cutoff time")
	}
	seen := map[string]bool{}
	failed := false
	for _, receipt := range value.Receipts {
		if !events.CleanupExpectedForModule(input.ExpectedModules, receipt.Module) || seen[receipt.Module] || receipt.RequestID != input.RequestID || receipt.RequestDigest != input.Digest || receipt.ParentExecutionID != input.ParentExecutionID || receipt.TenantID != int(input.TenantID) || receipt.Action != input.Action || receipt.CleanupMode != input.CleanupMode || !events.CleanupDigestValid(receipt.ImpactDigest) {
			return nil, fmt.Errorf("invalid cutoff receipt binding")
		}
		seen[receipt.Module] = true
		if receipt.Status != events.CleanupResultSuccess && receipt.Status != events.CleanupResultSkipped {
			failed = true
		}
	}
	for _, m := range value.Missing {
		if !events.CleanupExpectedForModule(input.ExpectedModules, m) || seen[m] {
			return nil, fmt.Errorf("invalid cutoff missing set")
		}
		seen[m] = true
	}
	if len(seen) != len(input.ExpectedModules) {
		return nil, fmt.Errorf("incomplete cutoff")
	}
	expected := execution.ExecutionStatusSuccess
	if len(value.Missing) > 0 {
		expected = execution.ExecutionStatusTimeout
		if value.ObservedAt.Before(input.Deadline) {
			return nil, fmt.Errorf("early timeout cutoff")
		}
	} else if failed {
		expected = execution.ExecutionStatusFailed
	}
	if value.Status != expected {
		return nil, fmt.Errorf("invalid cutoff status")
	}
	return &value, nil
}
