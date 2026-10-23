package dataprotection

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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

// DecodeFieldRoleChoices is the sole strict decoder for persisted Owner choices.
// Missing properties are handled by the Owner; an explicit null is invalid.
func DecodeFieldRoleChoices(raw any) ([]FieldRoleChoice, error) {
	encoded, err := json.Marshal(raw)
	if err != nil || len(encoded) > 128<<10 {
		return nil, ErrRoleOptionUnavailable
	}
	var choices []FieldRoleChoice
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&choices) != nil || choices == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrRoleOptionUnavailable
	}
	if err := ValidateFieldRoleChoices(choices); err != nil {
		return nil, err
	}
	return choices, nil
}
