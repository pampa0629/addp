package dataprotection

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// RoleMaskingOption grants eligibility only; it never changes the default.
type RoleMaskingOption struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	RoleKeys []string `json:"role_keys"`
	Decision Decision `json:"decision"`
}

type RoleMaskingOptions struct {
	PolicyID           string              `json:"policy_id"`
	AssessmentRevision int64               `json:"assessment_revision"`
	PolicyRevision     int64               `json:"policy_revision"`
	Options            []RoleMaskingOption `json:"options"`
}

type RoleOptionSelection struct {
	PolicyID           string `json:"policy_id"`
	AssessmentRevision int64  `json:"assessment_revision"`
	PolicyRevision     int64  `json:"policy_revision"`
	OptionID           string `json:"option_id"`
}

var (
	roleOptionKeyPattern     = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)
	ErrRoleOptionUnavailable = errors.New("protection role option is unavailable or has changed")
	ErrRoleOptionIneligible  = errors.New("current subject is not eligible for the protection role option")
)

func ValidateRoleMaskingOptions(options []RoleMaskingOption, defaultDecision Decision, valueType string) error {
	if len(options) > 32 || (len(options) != 0 && defaultDecision.Effect != EffectMask) {
		return errors.New("role options require a masking default and at most 32 options")
	}
	seen := make(map[string]bool, len(options))
	for _, option := range options {
		if uuid.Validate(option.ID) != nil || seen[option.ID] || option.Name != strings.TrimSpace(option.Name) || option.Name == "" || utf8.RuneCountInString(option.Name) > 100 {
			return errors.New("invalid or duplicate protection role option identity")
		}
		seen[option.ID] = true
		if len(option.RoleKeys) == 0 || len(option.RoleKeys) > 32 {
			return errors.New("role option requires 1 to 32 role keys")
		}
		previous := ""
		for _, key := range option.RoleKeys {
			if !roleOptionKeyPattern.MatchString(key) || len(key) > 128 || key <= previous {
				return errors.New("role option keys must be valid, unique and sorted")
			}
			previous = key
		}
		if option.Decision.Effect != EffectMask {
			return errors.New("role options must mask protected values")
		}
		if err := ValidateStructuredDecision(option.Decision, valueType); err != nil {
			return err
		}
	}
	return nil
}

// ResolveRoleOption only matches an explicit choice. The Owner must first bind
// this rule and currentRoleKeys to the trusted tenant, source and live actor.
// In particular, request-supplied roles and cached expired observations are not
// evidence. Plaintext exemptions never replace an explicit masking choice.
func (r Rule) ResolveRoleOption(selection RoleOptionSelection, currentRoleKeys []string) (Decision, error) {
	if r.Validate() != nil || r.RoleOptions == nil || selection.PolicyID != r.RoleOptions.PolicyID || selection.PolicyRevision != r.RoleOptions.PolicyRevision || selection.AssessmentRevision != r.RoleOptions.AssessmentRevision {
		return Decision{}, ErrRoleOptionUnavailable
	}
	for _, option := range r.RoleOptions.Options {
		if option.ID != selection.OptionID {
			continue
		}
		for _, eligible := range option.RoleKeys {
			for _, current := range currentRoleKeys {
				if eligible == current {
					return cloneRoleOptionDecision(option.Decision), nil
				}
			}
		}
		return Decision{}, ErrRoleOptionIneligible
	}
	return Decision{}, ErrRoleOptionUnavailable
}

func cloneRoleOptionDecision(decision Decision) Decision {
	if decision.Parameters != nil {
		parameters := make(map[string]any, len(decision.Parameters))
		for key, value := range decision.Parameters {
			parameters[key] = value
		}
		decision.Parameters = parameters
	}
	return decision
}
