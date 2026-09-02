package protection

import (
	"errors"
	"strings"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/datatype"
)

const (
	ConsumerOwner     = "manager"
	ActionPreview     = "preview"
	ActionProfile     = "profile"
	ActionSearchIndex = "search_index"
)

var ErrRequired = errors.New("manager data protection is required")

// LocalProjectionGate is the Manager request-time view of Security projection
// state. Implementations must be local; request paths must not call Security.
type LocalProjectionGate interface {
	Gate(int64, dataprotection.ResourceReference, time.Time) projectionstore.GateResult
}

func DataItemGate(
	gate LocalProjectionGate,
	tenantID uint,
	itemFingerprint string,
	now time.Time,
) projectionstore.GateResult {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if gate == nil || tenantID == 0 || itemFingerprint == "" {
		return projectionstore.GateResult{Managed: true, Err: ErrRequired}
	}
	return gate.Gate(int64(tenantID), dataprotection.ResourceReference{
		OwnerModule:      "meta",
		ResourceType:     "data_item",
		ResourceIdentity: itemFingerprint,
	}, now)
}

// TableRules validates locally installed rules for one Manager action against
// the exact Meta table snapshot used by the caller. Unmanaged resources return
// no rules and stay on the original path. Missing or invalid managed rules fail
// closed.
func TableRules(
	itemFingerprint string,
	fields []datatype.FieldInfo,
	gate projectionstore.GateResult,
	action string,
	now time.Time,
) ([]dataprotection.Rule, error) {
	if !gate.Managed {
		return nil, nil
	}
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	action = strings.TrimSpace(action)
	if itemFingerprint == "" || action == "" || gate.Err != nil || gate.State != dataprotection.ProjectionStateActive || len(gate.Projections) == 0 || len(fields) == 0 {
		return nil, ErrRequired
	}

	rules := make([]dataprotection.Rule, 0)
	seen := make(map[string]struct{})
	for _, projection := range gate.Projections {
		if projection.ConsumerOwner != ConsumerOwner ||
			projection.Target.OwnerModule != "meta" ||
			projection.Target.ResourceType != "data_item" ||
			projection.Target.ResourceIdentity != itemFingerprint {
			return nil, ErrRequired
		}
		projectionRules, err := dataprotection.ValidateTableProjection(projection, action, fields, now)
		if err != nil {
			return nil, ErrRequired
		}
		for _, rule := range projectionRules {
			key := strings.TrimSpace(rule.Action) + "\x00" + strings.TrimSpace(rule.Component.Key)
			if _, exists := seen[key]; exists {
				return nil, ErrRequired
			}
			seen[key] = struct{}{}
			rules = append(rules, rule)
		}
	}
	if len(rules) == 0 {
		return nil, ErrRequired
	}
	return rules, nil
}

func ProtectRows(rows []map[string]interface{}, action string, rules []dataprotection.Rule) error {
	if len(rules) == 0 {
		return nil
	}
	for _, row := range rows {
		if row == nil {
			return ErrRequired
		}
		if err := dataprotection.ProtectDocument(row, action, rules); err != nil {
			return ErrRequired
		}
	}
	return nil
}
