package engineaccess

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
)

const metadataRefreshPermission = "meta.scan_task.execute"

// Uses the same current principal/membership/department/project CTE as actual
// reads. Querying owned source rules never enumerates the source catalog.
const metadataRefreshGrantsSQL = `WITH input AS MATERIALIZED (
 SELECT ?::bigint AS tenant_id, ?::bigint AS principal_id, ?::bigint AS membership_id,
 ?::bigint AS authorization_version, clock_timestamp() AS observed_at
)` + sourceReadSubjectCTEs + ` SELECT DISTINCT g.catalog_path,g.node_recursive,g.item_fingerprint,g.boundary_hash,
 g.file_composition,g.scope_boundary,g.metadata_target
 FROM input JOIN system.engine_access_grants g ON g.tenant_id=input.tenant_id AND g.engine_id=?
 JOIN recipients r ON r.recipient_type=g.recipient_type AND r.recipient_id=g.recipient_id
 WHERE g.action IN ('metadata_read','read') AND ` + sourceGrantCurrentPredicate

// GetMetadataRefreshScope is an uncached observation for the current ordinary
// User. Meta owner IO occurs between two independently qualified snapshots.
func (s *Service) GetMetadataRefreshScope(ctx context.Context, credential string, query shared.MetadataRefreshQuery) (*shared.MetadataRefreshScope, error) {
	if query.Validate() != nil {
		return nil, commonapi.ErrBadRequest
	}
	if query.Kind == "engine" {
		return s.getManagedManualScanScope(ctx, credential, query)
	}
	initial, err := s.readMetadataRefreshScope(ctx, credential, query)
	if err != nil {
		return nil, err
	}
	if query.Kind == "item" {
		bindings, err := s.resolveMetadataBindings(ctx, initial.TenantID, []plugin.EngineCatalogPath{query.Target})
		if err != nil {
			return nil, err
		}
		if len(bindings) != 1 || bindings[0].ItemFingerprint == "" {
			return nil, commonapi.ErrForbidden
		}
		exact := []shared.MetadataRefreshRange{{Item: &bindings[0]}}
		if !shared.MetadataRefreshScopeStillCovered(exact, initial.Ranges, query.Target.EngineID) {
			return nil, commonapi.ErrForbidden
		}
		initial.Ranges = exact
	}
	if query.Kind == "node" {
		if s.nodeIdentities == nil {
			return nil, ErrAuthorizationFactsUnavailable
		}
		factQuery := authorizationfacts.NodeIdentityQuery{Target: query.Target}
		fact, err := s.nodeIdentities.ResolveNodeIdentity(ctx, uint(initial.TenantID), factQuery)
		if err != nil || fact == nil || fact.Validate(uint(initial.TenantID), factQuery) != nil {
			return nil, ErrAuthorizationFactsUnavailable
		}
		if !fact.Exists {
			return nil, commonapi.ErrForbidden
		}
	}
	paths := []plugin.EngineCatalogPath{}
	for _, r := range initial.Ranges {
		if r.Item != nil {
			path, _ := r.Item.PrimaryTarget()
			paths = append(paths, path)
		}
	}
	var bindings []authorizationfacts.Binding
	if len(paths) > 0 && query.Kind == "node" {
		bindings, err = s.resolveMetadataBindings(ctx, initial.TenantID, paths)
		if err != nil {
			return nil, err
		}
	}
	confirmed := make([]shared.MetadataRefreshRange, 0, len(initial.Ranges))
	index := 0
	for _, r := range initial.Ranges {
		if r.Item != nil {
			matched := query.Kind == "item" || (index < len(bindings) && reflect.DeepEqual(*r.Item, bindings[index]))
			index++
			if !matched {
				continue
			}
		} else {
			q := authorizationfacts.NodeIdentityQuery{Target: r.Node.Target}
			fact, err := s.nodeIdentities.ResolveNodeIdentity(ctx, uint(initial.TenantID), q)
			if err != nil || fact == nil || fact.Validate(uint(initial.TenantID), q) != nil {
				return nil, ErrAuthorizationFactsUnavailable
			}
			if !fact.Exists {
				continue
			}
		}
		confirmed = append(confirmed, r)
	}
	current, err := s.readMetadataRefreshScope(ctx, credential, query)
	if err != nil {
		return nil, err
	}
	if current.TenantID != initial.TenantID || current.Operator != initial.Operator || current.EngineVersion != initial.EngineVersion ||
		!shared.MetadataRefreshScopeStillCovered(confirmed, current.Ranges, query.Target.EngineID) {
		return nil, commonapi.ErrForbidden
	}
	// A newly granted range never expands the plan prepared before owner IO.
	current.Ranges = confirmed
	return current, current.Validate()
}

// Explicit centralized manual scans consume engine administration authority,
// independently from data visibility. They can bootstrap an unscanned engine.
func (s *Service) getManagedManualScanScope(ctx context.Context, credential string, query shared.MetadataRefreshQuery) (*shared.MetadataRefreshScope, error) {
	var actor Actor
	if err := s.repository.readCommitted(ctx, func(tx *Repository) error {
		auth, err := iam.NewAuthContextService(tx.identity())
		if err != nil {
			return err
		}
		current, err := auth.ResolveUserAccessToken(ctx, credential)
		if err != nil {
			return err
		}
		if current == nil || current.Context.TenantID == nil || current.Context.TenantMembershipID == nil || qualifyOrdinarySourceUser(*current, metadataRefreshPermission) != nil {
			return commonapi.ErrForbidden
		}
		actor.TenantID, err = strconv.ParseInt(*current.Context.TenantID, 10, 64)
		if err != nil {
			return err
		}
		actor.PrincipalID, err = strconv.ParseInt(current.Principal.ID, 10, 64)
		if err != nil {
			return err
		}
		actor.MembershipID, err = strconv.ParseInt(*current.Context.TenantMembershipID, 10, 64)
		if err != nil {
			return err
		}
		actor.AuthorizationVersion, err = strconv.ParseInt(current.Authorization.AuthorizationVersion, 10, 64)
		if err != nil {
			return err
		}
		actor.TokenExpiresAt = current.Token.ExpiresAt
		return nil
	}); err != nil {
		return nil, err
	}
	var result *shared.MetadataRefreshScope
	err := s.withEngineManagementScope(ctx, actor, int64(query.Target.EngineID), metadataRefreshPermission, false, func(tx *Repository, check func() error) error {
		engine, err := tx.engine(ctx, actor.TenantID, int64(query.Target.EngineID), false)
		if err != nil {
			return err
		}
		if engine.Version <= 0 || (engine.EngineType != "postgresql" && engine.EngineType != "nfs") || (engine.EngineType == "postgresql") != (query.Target.Segments[0].Term == "server") {
			return commonapi.ErrForbidden
		}
		now, err := tx.wallClock(ctx)
		if err != nil {
			return err
		}
		if err := check(); err != nil {
			return err
		}
		result = &shared.MetadataRefreshScope{TenantID: actor.TenantID, EngineVersion: strconv.FormatInt(engine.Version, 10), Query: query, Operator: shared.SharingFulfillmentOperator{PrincipalID: actor.PrincipalID, MembershipID: actor.MembershipID, AuthorizationVersion: actor.AuthorizationVersion}, ObservedAt: now, Ranges: []shared.MetadataRefreshRange{{Node: &shared.SourceNodeScope{Target: query.Target, Recursive: true}}}}
		return result.Validate()
	})
	return result, err
}

func (s *Service) readMetadataRefreshScope(ctx context.Context, credential string, query shared.MetadataRefreshQuery) (*shared.MetadataRefreshScope, error) {
	var result *shared.MetadataRefreshScope
	err := s.repository.readCommitted(ctx, func(tx *Repository) error {
		auth, err := iam.NewAuthContextService(tx.identity())
		if err != nil {
			return err
		}
		current, err := auth.ResolveUserAccessToken(ctx, credential)
		if err != nil {
			return err
		}
		if current == nil || current.Context.TenantID == nil || current.Context.TenantMembershipID == nil || qualifyOrdinarySourceUser(*current, metadataRefreshPermission) != nil {
			return commonapi.ErrForbidden
		}
		parse := func(text string) (int64, error) {
			value, err := strconv.ParseInt(text, 10, 64)
			if err != nil || value <= 0 || strconv.FormatInt(value, 10) != text {
				return 0, commonapi.ErrForbidden
			}
			return value, nil
		}
		tenant, err := parse(*current.Context.TenantID)
		if err != nil {
			return err
		}
		principal, err := parse(current.Principal.ID)
		if err != nil {
			return err
		}
		member, err := parse(*current.Context.TenantMembershipID)
		if err != nil {
			return err
		}
		version, err := parse(current.Authorization.AuthorizationVersion)
		if err != nil {
			return err
		}
		engine, err := tx.engine(ctx, tenant, int64(query.Target.EngineID), false)
		if err != nil {
			return err
		}
		if engine == nil || engine.Version <= 0 || engine.LifecycleState != "active" || engine.DeletedAt != nil || (engine.EngineType != "postgresql" && engine.EngineType != "nfs") ||
			(engine.EngineType == "postgresql") != (query.Target.Segments[0].Term == "server") {
			return commonapi.ErrForbidden
		}
		ranges, err := tx.metadataRefreshRanges(ctx, tenant, userProvenance{PrincipalID: principal, MembershipID: member, AuthorizationVersion: version}, query)
		if err != nil {
			return err
		}

		now, err := tx.wallClock(ctx)
		if err != nil {
			return err
		}
		if !current.Token.ExpiresAt.After(now) {
			return commonapi.ErrUnauthorized
		}
		result = &shared.MetadataRefreshScope{TenantID: tenant, EngineVersion: strconv.FormatInt(engine.Version, 10), Query: query,
			Operator: shared.SharingFulfillmentOperator{PrincipalID: principal, MembershipID: member, AuthorizationVersion: version}, Ranges: ranges, ObservedAt: now}
		return nil
	})
	return result, err
}

func metadataRefreshGrantRange(grant sourceGrant) (shared.MetadataRefreshRange, error) {
	var path plugin.EngineCatalogPath
	if json.Unmarshal(grant.CatalogPath, &path) != nil {
		return shared.MetadataRefreshRange{}, errSourceReadRules
	}
	if grant.NodeRecursive != nil {
		r := shared.MetadataRefreshRange{Node: &shared.SourceNodeScope{Target: path, Recursive: *grant.NodeRecursive}}
		return r, r.Validate(path.EngineID)
	}
	boundary := map[string]any{"item_fingerprint": grant.ItemFingerprint, "boundary_hash": grant.BoundaryHash}
	switch {
	case len(grant.FileComposition) > 0 && string(grant.FileComposition) != "null":
		boundary["file_composition"] = grant.FileComposition
	case len(grant.ScopeBoundary) > 0 && string(grant.ScopeBoundary) != "null":
		boundary["scope"] = grant.ScopeBoundary
	case len(grant.MetadataTarget) > 0 && string(grant.MetadataTarget) != "null":
		boundary["metadata_target"] = grant.MetadataTarget
	default:
		boundary["native_target"] = path
	}
	raw, err := json.Marshal(boundary)
	if err != nil {
		return shared.MetadataRefreshRange{}, err
	}
	var binding authorizationfacts.Binding
	if json.Unmarshal(raw, &binding) != nil {
		return shared.MetadataRefreshRange{}, errSourceReadRules
	}
	r := shared.MetadataRefreshRange{Item: &binding}
	return r, r.Validate(path.EngineID)
}

func (tx *Repository) metadataRefreshRanges(ctx context.Context, tenant int64, source userProvenance, query shared.MetadataRefreshQuery) ([]shared.MetadataRefreshRange, error) {
	var err error
	var grants []sourceGrant
	if err := tx.db.WithContext(ctx).Raw(metadataRefreshGrantsSQL, tenant, source.PrincipalID, source.MembershipID, source.AuthorizationVersion, query.Target.EngineID).Scan(&grants).Error; err != nil {
		return nil, err
	}
	ranges := make([]shared.MetadataRefreshRange, 0, len(grants))
	for _, grant := range grants {
		r, err := metadataRefreshGrantRange(grant)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, r)
	}
	if query.Kind == "node" {
		ranges, err = shared.IntersectMetadataRefreshScope(query.Target, ranges)
		if err != nil {
			return nil, err
		}
	} else {
		// Resolve exact current identity below outside this IAM snapshot.
		selected := []shared.MetadataRefreshRange{}
		for _, r := range ranges {
			if r.Item != nil {
				primary, _ := r.Item.PrimaryTarget()
				if reflect.DeepEqual(primary, query.Target) {
					selected = append(selected, r)
				}
			}
		}
		// Node-covered exact items are bound by the Meta preparation path;
		// node ranges stay as candidate maxima until then.
		for _, r := range ranges {
			if r.Node != nil && r.Node.Covers(query.Target) {
				selected = append(selected, r)
			}
		}
		ranges = selected
	}
	return ranges, nil
}
