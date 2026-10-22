package service

import (
	"errors"
	"slices"
	"strings"

	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"github.com/google/uuid"
)

func preparePolicyRoleOptions(requested, previous []dataprotection.RoleMaskingOption, baseline models.ProtectionBaseline, valueType string, defaultDecision dataprotection.Decision) ([]dataprotection.RoleMaskingOption, error) {
	known := make(map[string]bool, len(previous))
	for _, option := range previous {
		known[option.ID] = true
	}
	options := make([]dataprotection.RoleMaskingOption, len(requested))
	for index, option := range requested {
		if option.ID == "" {
			option.ID = uuid.NewString()
		} else if !known[option.ID] {
			return nil, errors.New("unknown role option identity")
		}
		option.Name = strings.TrimSpace(option.Name)
		option.RoleKeys = slices.Clone(option.RoleKeys)
		slices.Sort(option.RoleKeys)
		option.RoleKeys = slices.Compact(option.RoleKeys)
		if err := validatePolicyDecision(baseline, valueType, option.Decision); err != nil {
			return nil, err
		}
		options[index] = option
	}
	if err := dataprotection.ValidateRoleMaskingOptions(options, defaultDecision, valueType); err != nil {
		return nil, err
	}
	return options, nil
}

// A changed baseline invalidates options independently from the default. Missing
// options explicitly reject saved choices while ordinary reads stay protected.
func compiledPolicyRoleOptions(policy models.ProtectionPolicy, revision models.ProtectionPolicyRevision, baseline models.ProtectionBaseline, valueType string, assessmentRevision int64, decision dataprotection.Decision) *dataprotection.RoleMaskingOptions {
	if decision.Effect != dataprotection.EffectMask {
		return nil
	}
	options := make([]dataprotection.RoleMaskingOption, 0, len(revision.RoleOptions))
	for _, option := range revision.RoleOptions {
		if validatePolicyDecision(baseline, valueType, option.Decision) == nil {
			options = append(options, option)
		}
	}
	if len(options) == 0 {
		return nil
	}
	return &dataprotection.RoleMaskingOptions{PolicyID: policy.ID, PolicyRevision: revision.Revision, AssessmentRevision: assessmentRevision, Options: options}
}
