package events

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const CleanupRequestSchema = "addp.cleanup-request/v1"
const CleanupProtocolMaxBytes = 16 * 1024
const CleanupCountMax int64 = 1_000_000_000_000

// CleanupContext contains only neutral lifecycle selection. Owner paths and
// secrets cannot be represented here.
type CleanupContext struct {
	EngineID               uint   `json:"engine_id,omitempty"`
	TenantID               uint   `json:"tenant_id,omitempty"`
	ExternalArtifactPolicy string `json:"external_artifact_policy,omitempty"`
}

func (c CleanupContext) Map() map[string]interface{} {
	out := map[string]interface{}{}
	if c.EngineID != 0 {
		out["engine_id"] = c.EngineID
	}
	if c.TenantID != 0 {
		out["tenant_id"] = c.TenantID
	}
	if c.ExternalArtifactPolicy != "" {
		out["external_artifact_policy"] = c.ExternalArtifactPolicy
	}
	return out
}

type CleanupConfirmation struct {
	Confirmed    bool   `json:"confirmed"`
	TextProvided bool   `json:"text_provided"`
	RiskLevel    string `json:"risk_level,omitempty"`
	ImpactDigest string `json:"impact_digest,omitempty"`
}

// CleanupRequest is immutable input, never authority to bypass Owner checks.
type CleanupRequest struct {
	Schema            string              `json:"schema"`
	RequestID         string              `json:"request_id"`
	ParentExecutionID string              `json:"parent_execution_id"`
	TenantID          uint                `json:"tenant_id"`
	RequestedBy       uint                `json:"requested_by"`
	Action            string              `json:"action"`
	CleanupMode       string              `json:"cleanup_mode,omitempty"`
	TriggerType       string              `json:"trigger_type"`
	CauseEvent        string              `json:"cause_event,omitempty"`
	SourceOperationID string              `json:"source_operation_id,omitempty"`
	ExpectedModules   []string            `json:"expected_modules"`
	BasedOnScan       string              `json:"based_on_scan,omitempty"`
	Context           CleanupContext      `json:"context"`
	Confirmation      CleanupConfirmation `json:"confirmation"`
	RequestedAt       time.Time           `json:"requested_at"`
	Deadline          time.Time           `json:"deadline"`
	Digest            string              `json:"digest"`
}

func cleanupUUIDValid(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func CleanupModuleValid(module string) bool {
	switch module {
	case ModuleMeta, ModuleManager, ModuleTransfer, ModuleDevelop, ModuleService, ModuleQuality, ModuleGraph, ModuleAsset, ModuleModel, ModuleStandard:
		return true
	default:
		return false
	}
}
func CleanupDigestValid(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
func CleanupStrictDecode(data []byte, into interface{}) error {
	if len(data) == 0 || len(data) > CleanupProtocolMaxBytes {
		return fmt.Errorf("cleanup protocol size is invalid")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(into); err != nil {
		return err
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("cleanup protocol requires one JSON object")
	}
	return nil
}
func (r CleanupRequest) computedDigest() (string, error) {
	r.Digest = ""
	data, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}
func (r *CleanupRequest) Seal() error {
	if r == nil {
		return fmt.Errorf("cleanup request required")
	}
	r.Schema = CleanupRequestSchema
	r.ExpectedModules = append([]string(nil), r.ExpectedModules...)
	sort.Strings(r.ExpectedModules)
	r.RequestedAt = r.RequestedAt.UTC()
	r.Deadline = r.Deadline.UTC()
	digest, err := r.computedDigest()
	if err != nil {
		return err
	}
	r.Digest = digest
	return r.Validate()
}
func (r CleanupRequest) Validate() error {
	invalid := func() error { return fmt.Errorf("invalid cleanup request contract") }
	if r.Schema != CleanupRequestSchema || !cleanupUUIDValid(r.RequestID) || !cleanupUUIDValid(r.ParentExecutionID) || r.TenantID == 0 || r.RequestedAt.IsZero() || !r.Deadline.After(r.RequestedAt) || r.Deadline.Sub(r.RequestedAt) > 24*time.Hour || len(r.ExpectedModules) == 0 || len(r.ExpectedModules) > 10 || !sort.StringsAreSorted(r.ExpectedModules) {
		return invalid()
	}
	for i, m := range r.ExpectedModules {
		if !CleanupModuleValid(m) || i > 0 && r.ExpectedModules[i-1] == m {
			return invalid()
		}
	}
	switch r.TriggerType {
	case "manual":
		if r.RequestedBy == 0 {
			return invalid()
		}
	case "event":
		if r.SourceOperationID == "" || len(r.SourceOperationID) > 128 || r.CauseEvent == "" {
			return invalid()
		}
	default:
		return invalid()
	}
	switch r.CauseEvent {
	case "", CleanupCauseEngineDeleting, CleanupCauseEngineDeleted, CleanupCauseTenantDeleted:
	default:
		return invalid()
	}
	if r.Context.EngineID > 0 && r.Context.TenantID > 0 || r.Context.TenantID > 0 && r.Context.TenantID != r.TenantID {
		return invalid()
	}
	if (r.CauseEvent == CleanupCauseEngineDeleting || r.CauseEvent == CleanupCauseEngineDeleted) && r.Context.EngineID == 0 || r.CauseEvent == CleanupCauseTenantDeleted && r.Context.TenantID != r.TenantID {
		return invalid()
	}
	switch r.Action {
	case CleanupActionScan:
		if r.CleanupMode != "" || r.BasedOnScan != "" || r.Context.ExternalArtifactPolicy != "" || r.Confirmation != (CleanupConfirmation{}) {
			return invalid()
		}
	case CleanupActionExecute:
		if ValidateCleanupMode(r.CleanupMode) != nil || !cleanupUUIDValid(r.BasedOnScan) || !r.Confirmation.Confirmed || !CleanupDigestValid(r.Confirmation.ImpactDigest) {
			return invalid()
		}
		switch r.Confirmation.RiskLevel {
		case "low", "medium", "high":
		default:
			return invalid()
		}
		if (r.CleanupMode == CleanupModePhysical || r.Confirmation.RiskLevel == "high") && !r.Confirmation.TextProvided {
			return invalid()
		}
		if r.CleanupMode == CleanupModePhysical {
			if r.Context.ExternalArtifactPolicy != "delete" && r.Context.ExternalArtifactPolicy != "abandon" {
				return invalid()
			}
		} else if r.Context.ExternalArtifactPolicy != "" {
			return invalid()
		}
	default:
		return invalid()
	}
	digest, err := r.computedDigest()
	if err != nil || digest != r.Digest {
		return invalid()
	}
	data, err := json.Marshal(r)
	if err != nil || len(data) > CleanupProtocolMaxBytes {
		return invalid()
	}
	return nil
}

func ValidateCleanupSummary(s CleanupResultSummary, impact CleanupImpactSummary) error {
	counts := []int64{int64(s.ScannedItems), int64(s.AffectedRecords), int64(s.DeletedPhysicalArtifacts), s.FreedBytes, int64(s.MarkedMissingSource), int64(s.MarkedOutdated), int64(s.DisabledTaskDefinitions), int64(s.DeletedTaskDefinitions), int64(s.SkippedItems), int64(s.ErrorCount), int64(impact.Rebindable), int64(impact.WillDisable), int64(impact.WillDelete), int64(impact.Running), int64(impact.ExternalArtifact)}
	for _, n := range counts {
		if n < 0 || n > CleanupCountMax {
			return fmt.Errorf("cleanup count exceeds bounds")
		}
	}
	switch s.RiskLevel {
	case "low", "medium", "high":
	default:
		return fmt.Errorf("cleanup risk is invalid")
	}
	return nil
}
