package projectionstore

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
)

type querySourceProtection struct {
	target dataprotection.ResourceReference
	source plugin.QueryOutputSource
	rules  []dataprotection.Rule
}

func defaultQueryRules(rules []dataprotection.Rule) []dataprotection.Rule {
	result := append([]dataprotection.Rule(nil), rules...)
	for i := range result {
		result[i].Authorizations = nil
	}
	return result
}

func captureAuthorizedQuery(result *plugin.QueryResult, plans []querySourceProtection, subject dataprotection.SubjectReference, tenantID int64, action string) (*dataprotection.AuthorizedQueryResult, error) {
	if subject.Type != "user" || subject.ID == "" {
		return nil, nil
	}
	now := time.Now().UTC()
	snapshot := &dataprotection.AuthorizedQueryResult{TenantID: tenantID, Subject: subject}
	for _, plan := range plans {
		source := plan.source
		source.OpaqueOutput = false
		source.Bindings = nil
		for _, rule := range plan.rules {
			if rule.Action != action || rule.EffectiveDecision(subject, now).Effect != dataprotection.EffectAllow {
				continue
			}
			path := make([]string, len(rule.Component.Path))
			for i, segment := range rule.Component.Path {
				path[i] = segment.Name
			}
			if plan.source.IdentityOutput {
				source.Bindings = append(source.Bindings, plugin.QueryOutputBinding{SourcePath: path, OutputPath: path, Transformation: plugin.QueryOutputTransformationDirect})
			}
			for _, binding := range plan.source.Bindings {
				if slices.Equal(path, binding.SourcePath) && binding.Transformation == plugin.QueryOutputTransformationDirect {
					source.Bindings = append(source.Bindings, binding)
				}
			}
		}
		snapshot.Sources = append(snapshot.Sources, dataprotection.AuthorizedQuerySource{Target: plan.target, Source: source, DefaultRules: defaultQueryRules(plan.rules)})
	}
	open := false
	for _, source := range snapshot.Sources {
		open = open || len(source.Source.Bindings) > 0
	}
	if !open {
		return nil, nil
	}
	clone, err := dataprotection.CloneQueryResult(result)
	if err != nil {
		return nil, err
	}
	// Sanitize every non-authorized output before it can enter ephemeral owner memory.
	for _, plan := range plans {
		if err := dataprotection.ProtectQueryResultSourceAt(clone, plan.source, action, plan.rules, subject, now); err != nil {
			return nil, err
		}
	}
	snapshot.Result = *clone
	return snapshot, nil
}

// ReadAuthorizedQueryResult rechecks current local protection on every read,
// including expiration while Security is unavailable. It never mutates the in-memory snapshot.
func (s *Store) ReadAuthorizedQueryResult(ctx context.Context, tenantID int64, snapshot *dataprotection.AuthorizedQueryResult, subject dataprotection.SubjectReference, action string) (*dataprotection.AuthorizedQueryReadResult, error) {
	if snapshot == nil || snapshot.TenantID != tenantID {
		return nil, dataprotection.ErrDenied
	}
	if err := s.EnsureCurrent(ctx, tenantID); err != nil {
		return nil, err
	}
	return s.readAuthorizedQueryResultAt(ctx, tenantID, snapshot, subject, action, time.Now().UTC())
}

func (s *Store) readAuthorizedQueryResultAt(ctx context.Context, tenantID int64, snapshot *dataprotection.AuthorizedQueryResult, subject dataprotection.SubjectReference, action string, now time.Time) (*dataprotection.AuthorizedQueryReadResult, error) {
	if snapshot == nil || snapshot.TenantID != tenantID || len(snapshot.Sources) == 0 {
		return nil, dataprotection.ErrDenied
	}
	if subject != snapshot.Subject {
		subject = dataprotection.SubjectReference{}
	}
	result, err := dataprotection.CloneQueryResult(&snapshot.Result)
	if err != nil {
		return nil, err
	}
	var deadline *time.Time
	for _, source := range snapshot.Sources {
		gate := s.Gate(tenantID, source.Target, now)
		if !gate.Managed || gate.Err != nil || gate.State != dataprotection.ProjectionStateActive {
			return nil, dataprotection.ErrDenied
		}
		rules := []dataprotection.Rule{}
		for _, projection := range gate.Projections {
			current, err := dataprotection.ValidateTableProjection(projection, action, source.Source.Fields, now)
			if err != nil {
				return nil, err
			}
			rules = append(rules, current...)
		}
		// Changed default semantics require a fresh query, avoiding double hashing
		// or exposing stale weaker protection for outputs that were already masked.
		a, _ := json.Marshal(defaultQueryRules(rules))
		b, _ := json.Marshal(source.DefaultRules)
		if string(a) != string(b) {
			return nil, dataprotection.ErrDenied
		}
		for _, projection := range gate.Projections {
			for _, rule := range projection.Rules {
				if rule.Action != action || rule.EffectiveDecision(subject, now).Effect != dataprotection.EffectAllow {
					continue
				}
				path := make([]string, len(rule.Component.Path))
				for i, segment := range rule.Component.Path {
					path[i] = segment.Name
				}
				captured := false
				for _, binding := range source.Source.Bindings {
					captured = captured || slices.Equal(path, binding.SourcePath)
				}
				if !captured {
					continue
				}
				for _, grant := range rule.Authorizations {
					if grant.Subject != subject || now.Before(grant.ValidFrom) || !now.Before(grant.ValidUntil) {
						continue
					}
					until := grant.ValidUntil
					if projection.ExpiresAt != nil && projection.ExpiresAt.Before(until) {
						until = *projection.ExpiresAt
					}
					if deadline == nil || until.Before(*deadline) {
						deadline = &until
					}
				}
			}
		}
		output, outputRules := authorizedOutputRules(source.Source, rules)
		if len(source.Source.Bindings) == 0 {
			continue
		}
		if len(outputRules) == 0 {
			return nil, dataprotection.ErrDenied
		}
		if err := dataprotection.ProtectQueryResultSourceAt(result, output, action, outputRules, subject, now); err != nil {
			return nil, err
		}
	}
	return &dataprotection.AuthorizedQueryReadResult{QueryResult: *result, PlaintextExpiresAt: deadline}, nil
}

func authorizedOutputRules(source plugin.QueryOutputSource, rules []dataprotection.Rule) (plugin.QueryOutputSource, []dataprotection.Rule) {
	selected := []dataprotection.Rule{}
	for _, rule := range rules {
		path := make([]string, len(rule.Component.Path))
		for i, segment := range rule.Component.Path {
			path[i] = segment.Name
		}
		for _, binding := range source.Bindings {
			if slices.Equal(path, binding.SourcePath) {
				selected = append(selected, rule)
				break
			}
		}
	}
	// Identity output keeps the original typed object/array traversal. Bindings
	// are capture markers here; replaying them would flatten array containers.
	if source.IdentityOutput {
		source.Bindings = nil
	}
	return source, selected
}
