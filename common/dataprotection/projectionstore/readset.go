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
func (s *Store) PrepareQueryProtection(
	ctx context.Context,
	tenantID int64,
	model plugin.EngineCatalogModelSpec,
	prepared plugin.PreparedQuery,
	action string,
	now time.Time,
) (func(*plugin.QueryResult) error, error) {
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
		return noQueryProtection, nil
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
	for _, target := range targets {
		gate := s.Gate(tenantID, target, now)
		if !gate.Managed {
			continue
		}
		if gate.State != dataprotection.ProjectionStateActive || gate.Err != nil {
			return nil, dataprotection.ErrDenied
		}
		managed[target.ResourceIdentity] = gate
	}
	if len(managed) == 0 {
		return noQueryProtection, nil
	}

	lineage, err := prepared.OutputLineage(ctx)
	if err != nil {
		return nil, err
	}
	type sourceProtection struct {
		source plugin.QueryOutputSource
		rules  []dataprotection.Rule
	}
	plans := make([]sourceProtection, 0, len(managed))
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
		plans = append(plans, sourceProtection{source: source, rules: rules})
		matched[target.ResourceIdentity] = struct{}{}
	}
	if len(matched) != len(managed) {
		return nil, errors.New("protected query lineage is incomplete")
	}
	return func(result *plugin.QueryResult) error {
		for _, plan := range plans {
			if err := dataprotection.ProtectQueryResultSource(result, plan.source, action, plan.rules); err != nil {
				return fmt.Errorf("protect query result: %w", err)
			}
		}
		return nil
	}, nil
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
) (func(*plugin.QueryResult) error, error) {
	if strings.TrimSpace(action) == "" {
		return nil, errors.New("protection table gate requires an action")
	}
	if err := s.EnsureCurrent(ctx, tenantID); err != nil {
		return nil, err
	}
	if !s.HasManagedTargets(tenantID) {
		return noQueryProtection, nil
	}
	target, err := dataprotection.DataItemTargetFromCatalogPath(model, path)
	if err != nil {
		return nil, err
	}
	gate := s.Gate(tenantID, target, now)
	if !gate.Managed {
		return noQueryProtection, nil
	}
	if gate.State != dataprotection.ProjectionStateActive || gate.Err != nil {
		return nil, dataprotection.ErrDenied
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
	return func(result *plugin.QueryResult) error {
		if err := dataprotection.ProtectQueryResultSource(result, source, action, rules); err != nil {
			return fmt.Errorf("protect table result: %w", err)
		}
		return nil
	}, nil
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
