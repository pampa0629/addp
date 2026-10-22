package dataprotection

import (
	"errors"
	"strings"
	"time"
)

// FieldRoleChoice persists a reference, never role facts or a replacement decision.
type FieldRoleChoice struct {
	Target       ResourceReference   `json:"target"`
	ComponentKey string              `json:"component_key"`
	Selection    RoleOptionSelection `json:"selection"`
}

// QueryRoleSelection is request-local evidence already verified by the Owner.
// Execution Owners bind it to the current attempt; HTTP Owners bind it to the current User.
type QueryRoleSelection struct {
	Choices   []FieldRoleChoice
	RoleKeys  []string
	ExpiresAt time.Time
}

type RoleOptionCandidate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type FieldRoleOptions struct {
	Target             ResourceReference     `json:"target"`
	ComponentKey       string                `json:"component_key"`
	PolicyID           string                `json:"policy_id"`
	AssessmentRevision int64                 `json:"assessment_revision"`
	PolicyRevision     int64                 `json:"policy_revision"`
	Options            []RoleOptionCandidate `json:"options"`
}

func ValidateFieldRoleChoices(choices []FieldRoleChoice) error {
	if len(choices) > 128 {
		return ErrRoleOptionUnavailable
	}
	seen := map[string]bool{}
	for _, choice := range choices {
		if choice.Target.OwnerModule != "meta" || choice.Target.ResourceType != "data_item" || choice.Target.ComponentKey != "" || strings.TrimSpace(choice.Target.ResourceIdentity) == "" || choice.ComponentKey == "" || choice.ComponentKey != strings.TrimSpace(choice.ComponentKey) || choice.Selection.PolicyID == "" || choice.Selection.OptionID == "" || choice.Selection.AssessmentRevision <= 0 || choice.Selection.PolicyRevision <= 0 {
			return ErrRoleOptionUnavailable
		}
		key := choice.Target.ResourceIdentity + "\x00" + choice.ComponentKey
		if seen[key] {
			return ErrRoleOptionUnavailable
		}
		seen[key] = true
	}
	return nil
}

func (s *QueryRoleSelection) Validate(now time.Time) error {
	if s == nil {
		return nil
	}
	if err := ValidateFieldRoleChoices(s.Choices); err != nil {
		return err
	}
	if len(s.Choices) == 0 || s.RoleKeys == nil || !s.ExpiresAt.After(now) {
		return errors.Join(ErrDenied, ErrRoleOptionIneligible)
	}
	for i, key := range s.RoleKeys {
		if !roleOptionKeyPattern.MatchString(key) || i > 0 && key <= s.RoleKeys[i-1] {
			return ErrRoleOptionIneligible
		}
	}
	return nil
}
