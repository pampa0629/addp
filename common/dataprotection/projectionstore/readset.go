package projectionstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

// PrepareQueryProtection compiles one owner-local result protection closure
// from the same immutable PreparedQuery that the owner will execute. It keeps
// tenants without managed resources off the ReadSet and OutputLineage paths.
// subject must come from the owner's trusted current-user context; an empty
// subject applies default protection and does not infer an execution's author.
func (s *Store) PrepareQueryProtection(
	ctx context.Context,
	tenantID int64,
	model plugin.EngineCatalogModelSpec,
	prepared plugin.PreparedQuery,
	action string,
	subject dataprotection.SubjectReference,
	selection *dataprotection.QueryRoleSelection,
	now time.Time,
) (*dataprotection.PreparedTableProtection, error) {
	if err := selection.Validate(now); err != nil {
		return nil, err
	}
	if prepared == nil {
		return nil, errors.New("protection query gate requires a prepared query")
	}
	if strings.TrimSpace(action) == "" {
		return nil, errors.New("protection query gate requires an action")
	}
	if err := s.EnsureCurrent(ctx, tenantID); err != nil {
		return nil, err
	}
	if !s.HasManagedTargets(tenantID) {
		if selection != nil {
			return nil, dataprotection.ErrRoleOptionUnavailable
		}
		return &dataprotection.PreparedTableProtection{Apply: noQueryProtection, ApplyDefault: noQueryProtection}, nil
	}
	if scope, err := prepared.EngineReadScope(ctx); err == nil {
		if s.engineScopeDisjoint(tenantID, scope) {
			if selection != nil {
				return nil, dataprotection.ErrRoleOptionUnavailable
			}
			return &dataprotection.PreparedTableProtection{Apply: noQueryProtection, ApplyDefault: noQueryProtection}, nil
		}
	} else if !errors.Is(err, plugin.ErrQueryEngineReadScopeUnresolved) {
		return nil, err
	}
	readSet, err := prepared.ReadSet(ctx)
	if err != nil {
		return nil, err
	}
	targets, err := dataprotection.DataItemTargetsFromQueryReadSet(model, readSet)
	if err != nil {
		return nil, err
	}
	managed := make(map[string]GateResult)
	projections := []dataprotection.Projection{}
	for _, target := range targets {
		gate := s.Gate(tenantID, target, now)
		if !gate.Managed {
			continue
		}
		if gate.State != dataprotection.ProjectionStateActive || gate.Err != nil {
			return nil, deniedGateError(gate)
		}
		managed[target.ResourceIdentity] = gate
		projections = append(projections, gate.Projections...)
	}
	if len(managed) == 0 {
		if selection != nil {
			return nil, dataprotection.ErrRoleOptionUnavailable
		}
		return &dataprotection.PreparedTableProtection{Apply: noQueryProtection, ApplyDefault: noQueryProtection}, nil
	}

	lineage, err := prepared.OutputLineage(ctx)
	if err != nil {
		return nil, err
	}
	plans := make([]querySourceProtection, 0, len(managed))
	derived := []string{}
	matched := make(map[string]struct{}, len(managed))
	for _, source := range lineage.Sources {
		target, targetErr := dataprotection.DataItemTargetFromCatalogPath(model, source.Path)
		if targetErr != nil {
			return nil, targetErr
		}
		gate, exists := managed[target.ResourceIdentity]
		if !exists {
			continue
		}
		rules := make([]dataprotection.Rule, 0, len(gate.Projections))
		for _, projection := range gate.Projections {
			projectionRules, validationErr := dataprotection.ValidateTableProjection(projection, action, source.Fields, now)
			if validationErr != nil {
				return nil, validationErr
			}
			rules = append(rules, projectionRules...)
		}
		chosen, choiceErr := planRoleRules(querySourceProtection{target: target, source: source, rules: rules}, selection, action)
		if choiceErr != nil {
			return nil, choiceErr
		}
		fields := dataprotection.QueryOutputDerivedFields(source, action, chosen, subject, now)
		derived = append(derived, fields...)
		plans = append(plans, querySourceProtection{target: target, source: source, rules: rules})
		matched[target.ResourceIdentity] = struct{}{}
	}
	if len(matched) != len(managed) {
		return nil, dataprotection.ErrLineageUnverified
	}
	if err := validateConsumedRoleChoices(plans, selection, action); err != nil {
		return nil, err
	}
	// Freeze the exact actor's preparation-time eligibility. A grant that
	// starts during execution cannot reclassify an already masked value as raw.
	plans = queryPlansAt(plans, subject, now)
	if binder, ok := prepared.(plugin.OutputTransformPreparedQuery); ok {
		operations := []plugin.QueryOutputTransform{}
		for _, plan := range plans {
			nativeRules := []dataprotection.Rule{}
			for _, rule := range plan.rules {
				if !selectedRule(plan, rule, selection) {
					nativeRules = append(nativeRules, rule)
				}
			}
			// Validate the complete default and selected mappings before deferring only selected transformations.
			chosen, err := planRoleRules(plan, selection, action)
			if err != nil {
				return nil, err
			}
			if _, err := dataprotection.QueryOutputTransformsAt(plan.source, action, chosen, subject, now); err != nil {
				return nil, err
			}
			compiled, err := dataprotection.QueryOutputTransformsAt(plan.source, action, nativeRules, subject, now)
			if err != nil {
				return nil, err
			}
			operations = append(operations, compiled...)
		}
		if len(operations) > 0 {
			if err := binder.BindOutputTransforms(ctx, operations); err != nil {
				return nil, fmt.Errorf("%w: %w", dataprotection.ErrDenied, err)
			}
		}
	}
	apply := func(actor dataprotection.SubjectReference, selected bool) func(*plugin.QueryResult) error {
		return func(result *plugin.QueryResult) error {
			if err := validateOutputProjections(projections); err != nil {
				return err
			}
			if selected {
				if err := selection.Validate(time.Now().UTC()); err != nil {
					return err
				}
			}
			for _, plan := range plans {
				rules := plan.rules
				if selected {
					var err error
					rules, err = planRoleRules(plan, selection, action)
					if err != nil {
						return err
					}
				}
				if err := dataprotection.ProtectQueryResultSource(result, plan.source, action, rules, actor); err != nil {
					return fmt.Errorf("protect query result: %w", err)
				}
			}
			return nil
		}
	}
	return &dataprotection.PreparedTableProtection{DerivedFields: derived, Apply: apply(subject, true), ApplyDefault: apply(dataprotection.SubjectReference{}, false), CaptureAuthorized: func(result *plugin.QueryResult, actor dataprotection.SubjectReference) (*dataprotection.AuthorizedQueryResult, error) {
		if actor != subject {
			return nil, dataprotection.ErrDenied
		}
		if err := validateOutputProjections(projections); err != nil {
			return nil, err
		}
		return captureAuthorizedQuery(result, plans, actor, tenantID, action, selection)
	}}, nil
}

func noQueryProtection(*plugin.QueryResult) error { return nil }

// PrepareTableProtection compiles one owner-local result protection closure
// for an exact provider-owned table/collection leaf. Native table readers use
// this instead of manufacturing a query solely for protection matching.
func (s *Store) PrepareTableProtection(
	ctx context.Context,
	tenantID int64,
	model plugin.EngineCatalogModelSpec,
	path plugin.EngineCatalogPath,
	fields []datatype.FieldInfo,
	action string,
	now time.Time,
) (*dataprotection.PreparedTableProtection, error) {
	if strings.TrimSpace(action) == "" {
		return nil, errors.New("protection table gate requires an action")
	}
	if err := s.EnsureCurrent(ctx, tenantID); err != nil {
		return nil, err
	}
	if !s.HasManagedTargets(tenantID) {
		return &dataprotection.PreparedTableProtection{Apply: noQueryProtection, ApplyDefault: noQueryProtection}, nil
	}
	target, err := dataprotection.DataItemTargetFromCatalogPath(model, path)
	if err != nil {
		return nil, err
	}
	gate := s.Gate(tenantID, target, now)
	if !gate.Managed {
		return &dataprotection.PreparedTableProtection{Apply: noQueryProtection, ApplyDefault: noQueryProtection}, nil
	}
	if gate.State != dataprotection.ProjectionStateActive || gate.Err != nil {
		return nil, deniedGateError(gate)
	}
	rules := make([]dataprotection.Rule, 0, len(gate.Projections))
	for _, projection := range gate.Projections {
		projectionRules, validationErr := dataprotection.ValidateTableProjection(projection, action, fields, now)
		if validationErr != nil {
			return nil, validationErr
		}
		rules = append(rules, projectionRules...)
	}
	source := plugin.QueryOutputSource{
		Path: path, Fields: append([]datatype.FieldInfo(nil), fields...), IdentityOutput: true,
	}
	derived := dataprotection.QueryOutputDerivedFields(source, action, rules, dataprotection.SubjectReference{}, now)

	return &dataprotection.PreparedTableProtection{DerivedFields: derived, ValidateSourceSchema: func(current []datatype.FieldInfo) error {
		if _, err := dataprotection.TableSchemaSnapshotHash(current); err != nil {
			return fmt.Errorf("%w: live source structure unavailable", dataprotection.ErrSchemaMismatch)
		}
		for _, projection := range gate.Projections {
			if _, err := dataprotection.ValidateTableProjection(projection, action, current, time.Now().UTC()); err != nil {
				return err
			}
		}
		return nil
	}, Apply: func(result *plugin.QueryResult) error {
		if err := validateOutputProjections(gate.Projections); err != nil {
			return err
		}
		if err := dataprotection.ProtectQueryResultSource(result, source, action, rules, dataprotection.SubjectReference{}); err != nil {
			return fmt.Errorf("protect table result: %w", err)
		}
		return nil
	}}, nil
}

// A successful read admission does not extend a projection's lifetime. Check
// every bound source before transforming any output, including empty pages.
func validateOutputProjections(projections []dataprotection.Projection) error {
	now := time.Now().UTC()
	for _, projection := range projections {
		if err := projection.Validate(now); err != nil {
			return fmt.Errorf("%w: %w", dataprotection.ErrDenied, err)
		}
	}
	return nil
}

// RequireCatalogPathUnmanaged gates one concrete provider-owned DataItem leaf.
// Native table, record, content, and stream readers use this path instead of
// building an artificial query solely for protection matching.
func (s *Store) RequireCatalogPathUnmanaged(
	ctx context.Context,
	tenantID int64,
	model plugin.EngineCatalogModelSpec,
	path plugin.EngineCatalogPath,
	now time.Time,
) error {
	target, err := dataprotection.DataItemTargetFromCatalogPath(model, path)
	if err != nil {
		return err
	}
	return s.RequireUnmanaged(ctx, tenantID, []dataprotection.ResourceReference{target}, now)
}
