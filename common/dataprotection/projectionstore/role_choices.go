package projectionstore

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
)

// RoleOptions only observes one exact, caller-selected target. It never lists a tenant.
func (s *Store) RoleOptions(ctx context.Context, tenantID int64, target dataprotection.ResourceReference, action string, roles []string, now time.Time) ([]dataprotection.FieldRoleOptions, error) {
	if err := s.EnsureCurrent(ctx, tenantID); err != nil {
		return nil, err
	}
	gate := s.Gate(tenantID, target, now)
	result := []dataprotection.FieldRoleOptions{}
	if !gate.Managed {
		return result, nil
	}
	if gate.Err != nil || gate.State != dataprotection.ProjectionStateActive {
		return nil, deniedGateError(gate)
	}
	for _, projection := range gate.Projections {
		for _, rule := range projection.Rules {
			if rule.Action != action || rule.RoleOptions == nil {
				continue
			}
			options := rule.RoleOptions
			field := dataprotection.FieldRoleOptions{Target: target, ComponentKey: rule.Component.Key, PolicyID: options.PolicyID, AssessmentRevision: options.AssessmentRevision, PolicyRevision: options.PolicyRevision, Options: []dataprotection.RoleOptionCandidate{}}
			for _, option := range options.Options {
				choice := dataprotection.RoleOptionSelection{PolicyID: options.PolicyID, AssessmentRevision: options.AssessmentRevision, PolicyRevision: options.PolicyRevision, OptionID: option.ID}
				if _, err := rule.ResolveRoleOption(choice, roles); err == nil {
					field.Options = append(field.Options, dataprotection.RoleOptionCandidate{ID: option.ID, Name: option.Name})
				}
			}
			result = append(result, field)
		}
	}
	return result, nil
}

func (s *Store) ValidateRoleChoices(ctx context.Context, tenantID int64, selection *dataprotection.QueryRoleSelection, action string, now time.Time) error {
	if err := selection.Validate(now); err != nil {
		return err
	}
	if selection == nil {
		return nil
	}
	for _, choice := range selection.Choices {
		candidates, err := s.RoleOptions(ctx, tenantID, choice.Target, action, selection.RoleKeys, now)
		if err != nil {
			return err
		}
		found := false
		for _, field := range candidates {
			if field.ComponentKey != choice.ComponentKey || field.PolicyID != choice.Selection.PolicyID || field.AssessmentRevision != choice.Selection.AssessmentRevision || field.PolicyRevision != choice.Selection.PolicyRevision {
				continue
			}
			for _, option := range field.Options {
				found = found || option.ID == choice.Selection.OptionID
			}
		}
		if !found {
			return errors.Join(dataprotection.ErrDenied, dataprotection.ErrRoleOptionUnavailable)
		}
	}
	return nil
}

func planRoleRules(plan querySourceProtection, selection *dataprotection.QueryRoleSelection, action string) ([]dataprotection.Rule, error) {
	rules := append([]dataprotection.Rule(nil), plan.rules...)
	if selection == nil {
		return rules, nil
	}
	for index, rule := range rules {
		for _, choice := range selection.Choices {
			if choice.Target != plan.target || choice.ComponentKey != rule.Component.Key || rule.Action != action {
				continue
			}
			decision, err := rule.ResolveRoleOption(choice.Selection, selection.RoleKeys)
			if err != nil {
				return nil, errors.Join(dataprotection.ErrDenied, err)
			}
			rules[index].Decision = decision
			rules[index].Authorizations = nil
		}
	}
	return rules, nil
}

func selectedRule(plan querySourceProtection, rule dataprotection.Rule, selection *dataprotection.QueryRoleSelection) bool {
	if selection != nil {
		for _, choice := range selection.Choices {
			if choice.Target == plan.target && choice.ComponentKey == rule.Component.Key {
				return true
			}
		}
	}
	return false
}

// A choice must be consumed by proven direct/identity output. A source merely
// occurring in WHERE, or an unrelated alias with the same name, is insufficient.
func validateConsumedRoleChoices(plans []querySourceProtection, selection *dataprotection.QueryRoleSelection, action string) error {
	if selection == nil {
		return nil
	}
	for _, choice := range selection.Choices {
		consumed := false
		for _, plan := range plans {
			if plan.target != choice.Target {
				continue
			}
			for _, rule := range plan.rules {
				if rule.Action != action || rule.Component.Key != choice.ComponentKey {
					continue
				}
				if _, err := rule.ResolveRoleOption(choice.Selection, selection.RoleKeys); err != nil {
					return errors.Join(dataprotection.ErrDenied, err)
				}
				path := make([]string, len(rule.Component.Path))
				for i, s := range rule.Component.Path {
					path[i] = s.Name
				}
				if plan.source.OpaqueOutput {
					return dataprotection.ErrLineageUnverified
				}
				if plan.source.IdentityOutput {
					consumed = true
				}
				for _, binding := range plan.source.Bindings {
					if slices.Equal(binding.SourcePath, path) && binding.Transformation != plugin.QueryOutputTransformationDirect {
						return dataprotection.ErrComputedOutputUnsupported
					}
					if slices.Equal(binding.SourcePath, path) && binding.Transformation == plugin.QueryOutputTransformationDirect {
						consumed = true
					}
				}
			}
		}
		if !consumed {
			return errors.Join(dataprotection.ErrDenied, dataprotection.ErrRoleOptionUnavailable)
		}
	}
	return nil
}
