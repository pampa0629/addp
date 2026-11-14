package engineaccess

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
)

// Navigation uses the same live recipient and Grant predicates as actual reads.
const navigationGrantsSQL = `WITH input AS MATERIALIZED (
 SELECT ?::bigint tenant_id,?::bigint principal_id,?::bigint membership_id,
 ?::bigint authorization_version,clock_timestamp() observed_at
)` + sourceReadSubjectCTEs + `
 SELECT g.request_id,g.catalog_path,g.item_fingerprint,g.boundary_hash,g.node_recursive FROM input JOIN system.engine_access_grants g ON g.tenant_id=input.tenant_id
 JOIN recipients r ON r.recipient_type=g.recipient_type AND r.recipient_id=g.recipient_id
 JOIN system.engines e ON e.id=g.engine_id AND e.tenant_id=g.tenant_id
 WHERE g.engine_id IN ? AND e.lifecycle_state='active' AND e.deleted_at IS NULL
 AND e.engine_type IN ('postgresql','nfs','minio')
 AND g.action IN ('metadata_read','read') AND ` + sourceGrantCurrentPredicate

func (s *Service) CheckManagerNavigation(ctx context.Context, credential string, q shared.SourceNavigationCheckRequest) (*shared.SourceMetadataCheck, error) {
	for _, root := range q.Targets {
		if len(root.Segments) != 1 {
			return nil, commonapi.ErrBadRequest
		}
	}
	return s.checkNavigation(ctx, credential, q, false, func(current shared.AuthContext) error {
		return qualifyOrdinarySourceUser(current, ManagerPreviewReadPermission)
	})
}
func (s *Service) CheckMetaNavigation(ctx context.Context, credential string, q shared.SourceNavigationCheckRequest) (*shared.SourceMetadataCheck, error) {
	return s.checkNavigation(ctx, credential, q, true, func(current shared.AuthContext) error {
		return qualifyOrdinarySourceUser(current, MetaCatalogReadPermission)
	})
}

func (s *Service) readNavigationGrants(ctx context.Context, credential string, engines []uint, nodesOnly bool, qualify func(shared.AuthContext) error) (int64, []sourceGrant, time.Time, error) {
	var tenant int64
	var observed time.Time
	var grants []sourceGrant
	err := s.repository.readCommitted(ctx, func(tx *Repository) error {
		auth, err := iam.NewAuthContextService(tx.identity())
		if err != nil {
			return err
		}
		current, err := auth.ResolveUserAccessToken(ctx, credential)
		if err != nil {
			return err
		}
		if current == nil || current.Context.TenantID == nil || current.Context.TenantMembershipID == nil {
			return commonapi.ErrForbidden
		}
		if err = qualify(*current); err != nil {
			return err
		}
		ids := make([]int64, 4)
		for i, value := range []string{*current.Context.TenantID, current.Principal.ID, *current.Context.TenantMembershipID, current.Authorization.AuthorizationVersion} {
			ids[i], err = strconv.ParseInt(value, 10, 64)
			if err != nil || ids[i] <= 0 {
				return commonapi.ErrForbidden
			}
		}
		tenant = ids[0]
		query := navigationGrantsSQL
		if nodesOnly {
			query += " AND g.node_recursive IS NOT NULL"
		}
		if err = tx.db.WithContext(ctx).Raw(query, ids[0], ids[1], ids[2], ids[3], engines).Scan(&grants).Error; err != nil {
			return err
		}
		observed, err = tx.wallClock(ctx)
		if err != nil {
			return err
		}
		if !current.Token.ExpiresAt.After(observed) {
			return commonapi.ErrUnauthorized
		}
		return nil
	})
	return tenant, grants, observed, err
}

func (s *Service) checkNavigation(ctx context.Context, credential string, q shared.SourceNavigationCheckRequest, nodesOnly bool, qualify func(shared.AuthContext) error) (*shared.SourceMetadataCheck, error) {
	if !q.Valid() {
		return nil, commonapi.ErrBadRequest
	}
	if s == nil || s.repository == nil || s.repository.db == nil {
		return nil, errSourceReadRules
	}
	engines := []uint{}
	seen := map[uint]bool{}
	for _, target := range q.Targets {
		if !seen[target.EngineID] {
			seen[target.EngineID] = true
			engines = append(engines, target.EngineID)
		}
	}
	tenant, grants, observed, err := s.readNavigationGrants(ctx, credential, engines, nodesOnly, qualify)
	if err != nil {
		return nil, err
	}
	result := &shared.SourceMetadataCheck{Targets: make([]shared.SourceMetadataTargetCheck, len(q.Targets))}
	result.ObservedAt = observed
	for i, target := range q.Targets {
		result.Targets[i].Target = target
	}
	// Meta supplies tree candidates from its own node projection. System proves
	// each granted origin, never probes a guessed or ungranted descendant. A
	// covered candidate path does not assert that a node exists in the catalog.
	positions := map[uuid.UUID][]int{}
	proven := map[uuid.UUID]bool{}
	nodeFacts := map[string]bool{}
	exact := map[string][]sourceGrant{}
	paths := []plugin.EngineCatalogPath{}
	exactKeys := []string{}
	for _, g := range grants {
		var path plugin.EngineCatalogPath
		if json.Unmarshal(g.CatalogPath, &path) != nil {
			return nil, errSourceReadRules
		}
		for i, target := range q.Targets {
			if shared.NavigationContains(target, path) || (g.NodeRecursive != nil && *g.NodeRecursive && shared.NavigationContains(path, target)) {
				positions[g.RequestID] = append(positions[g.RequestID], i)
			}
		}
		if len(positions[g.RequestID]) == 0 {
			continue
		}
		key := string(g.CatalogPath)
		if g.NodeRecursive == nil {
			if _, seen := exact[key]; !seen {
				paths = append(paths, path)
				exactKeys = append(exactKeys, key)
			}
			exact[key] = append(exact[key], g)
			continue
		}
		exists, seen := nodeFacts[key]
		if !seen {
			if s.nodeIdentities == nil {
				return nil, ErrAuthorizationFactsUnavailable
			}
			query := authorizationfacts.NodeIdentityQuery{Target: path}
			fact, e := s.nodeIdentities.ResolveNodeIdentity(ctx, uint(tenant), query)
			if e != nil || fact == nil || fact.Validate(uint(tenant), query) != nil {
				return nil, ErrAuthorizationFactsUnavailable
			}
			exists = fact.Exists
			nodeFacts[key] = exists
		}
		if exists {
			proven[g.RequestID] = true
		}
	}
	for offset := 0; offset < len(paths); offset += 200 {
		end := min(offset+200, len(paths))
		bindings, e := s.resolveMetadataBindings(ctx, tenant, paths[offset:end])
		if e != nil {
			return nil, e
		}
		for i, binding := range bindings {
			if binding.ItemFingerprint == "" {
				continue
			}
			for _, g := range exact[exactKeys[offset+i]] {
				if binding.ItemFingerprint == g.ItemFingerprint && binding.BoundaryHash == g.BoundaryHash {
					proven[g.RequestID] = true
				}
			}
		}
	}
	if len(proven) == 0 {
		return result, nil
	}
	// Only immutable grant IDs that survived both snapshots may expose paths.
	finalTenant, current, finalTime, err := s.readNavigationGrants(ctx, credential, engines, nodesOnly, qualify)
	if err != nil {
		return nil, err
	}
	if finalTenant != tenant {
		return nil, commonapi.ErrForbidden
	}
	result.ObservedAt = finalTime
	for _, g := range current {
		if proven[g.RequestID] {
			for _, i := range positions[g.RequestID] {
				result.Targets[i].Visible = true
			}
		}
	}
	return result, nil
}
