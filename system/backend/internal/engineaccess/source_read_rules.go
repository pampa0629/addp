package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
)

var errSourceReadRules = errors.New("invalid or incomplete source read rule observation")

// Private rule-layer input, NOT authentication evidence. A future trusted IAM /
// execution consumer must establish the live credential and complete Provider
// read set before using this method; no HTTP body may supply these references.
type sourceReadRequest struct {
	TenantID int64
	Source   userProvenance
	Targets  []engineplugin.EngineCatalogPath
}

type sourceReadTargetRule struct {
	Path    engineplugin.EngineCatalogPath
	Covered bool
	Reason  string
}

// Covered means only that current source rules cover ALL targets. It is not an
// executable permission, token or lease; function, execution and Security checks
// are independent. The observation must not be cached as a later access verdict.
type sourceReadRules struct {
	ObservedAt time.Time
	Covered    bool
	Targets    []sourceReadTargetRule
}

func (request sourceReadRequest) encode() ([]engineplugin.EngineCatalogPath, json.RawMessage, error) {
	if request.TenantID <= 0 || !request.Source.valid() {
		return nil, nil, errSourceReadRules
	}
	return encodeSourceReadTargets(request.Targets)
}

func encodeSourceReadTargets(targets []engineplugin.EngineCatalogPath) ([]engineplugin.EngineCatalogPath, json.RawMessage, error) {
	if len(targets) == 0 {
		return nil, nil, errSourceReadRules
	}
	paths := make([]engineplugin.EngineCatalogPath, 0, len(targets))
	encoded := make([]json.RawMessage, 0, len(targets))
	seen := make(map[string]bool, len(targets))
	for _, path := range targets {
		canonical, err := authorization.EncodeSharingTarget(path)
		if err != nil {
			return nil, nil, errSourceReadRules
		}
		if seen[string(canonical)] {
			continue
		}
		seen[string(canonical)] = true
		// Keep the returned target immutable even if the caller reuses its slice.
		var copy engineplugin.EngineCatalogPath
		if err := json.Unmarshal(canonical, &copy); err != nil {
			return nil, nil, errSourceReadRules
		}
		paths = append(paths, copy)
		encoded = append(encoded, canonical)
	}
	batch, err := json.Marshal(encoded)
	return paths, batch, err
}

type sourceReadRuleRow struct {
	Position   int64
	ObservedAt time.Time
	Reason     string
}

func sourceReadObservation(paths []engineplugin.EngineCatalogPath, rows []sourceReadRuleRow) (*sourceReadRules, error) {
	if len(paths) == 0 || len(rows) != len(paths) || rows[0].ObservedAt.IsZero() {
		return nil, errSourceReadRules
	}
	result := &sourceReadRules{ObservedAt: rows[0].ObservedAt.UTC(), Covered: true, Targets: make([]sourceReadTargetRule, len(paths))}
	for i, row := range rows {
		if row.Position != int64(i+1) || !row.ObservedAt.Equal(result.ObservedAt) {
			return nil, errSourceReadRules
		}
		switch row.Reason {
		case "grant", "explicit_deny", "no_grant", "source_unavailable", "target_unavailable":
		default:
			return nil, errSourceReadRules
		}
		covered := row.Reason == "grant"
		result.Targets[i] = sourceReadTargetRule{Path: paths[i], Covered: covered, Reason: row.Reason}
		result.Covered = result.Covered && covered
	}
	return result, nil
}

func (r *Repository) readCurrentSourceRules(ctx context.Context, request sourceReadRequest) (*sourceReadRules, error) {
	paths, batch, err := request.encode()
	if err != nil {
		return nil, err
	}
	var result *sourceReadRules
	err = r.readCommitted(ctx, func(tx *Repository) error {
		var queryErr error
		result, queryErr = tx.queryCurrentSourceRules(ctx, request, paths, batch)
		return queryErr
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Only called inside the observation's own read-only transaction. The trusted
// credential adapter uses this same query, not a second source-rule algorithm.
func (r *Repository) queryCurrentSourceRules(ctx context.Context, request sourceReadRequest, paths []engineplugin.EngineCatalogPath, batch json.RawMessage) (*sourceReadRules, error) {
	var rows []sourceReadRuleRow
	if err := r.db.WithContext(ctx).Raw(currentSourceReadRulesSQL, request.TenantID,
		request.Source.PrincipalID, request.Source.MembershipID, request.Source.AuthorizationVersion,
		string(batch)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return sourceReadObservation(paths, rows)
}

// One statement gives the complete set one committed snapshot and one wall
// clock. Existing target indexes begin with tenant_id, engine_id; immutable
// revocation/release PKs support the anti-joins. Never take target or IAM locks.
// The fulfillment Deadline is an issuance window, NOT the Grant access expiry.
// Committed Deny establishment is immediate; its audit timestamp must not act
// as a scheduled start time if the database wall clock later moves backwards.
const currentSourceReadRulesSQL = `
WITH input AS MATERIALIZED (
	SELECT ?::bigint AS tenant_id, ?::bigint AS principal_id,
	       ?::bigint AS membership_id, ?::bigint AS authorization_version,
	       ?::jsonb AS targets, clock_timestamp() AS observed_at
), targets AS (
	SELECT target.value AS path, target.ordinality AS position,
	       (target.value->>'engine_id')::bigint AS engine_id
	FROM input, jsonb_array_elements(input.targets) WITH ORDINALITY AS target(value, ordinality)
), current_source AS (
	SELECT input.* FROM input
	JOIN system.principals p ON p.id = input.principal_id AND p.principal_type = 'user'
	  AND p.status = 'active' AND p.authorization_version = input.authorization_version
	JOIN system.users u ON u.id = p.id
	JOIN system.tenant_memberships m ON m.id = input.membership_id AND m.principal_id = p.id
	  AND m.tenant_id = input.tenant_id AND m.status = 'active' AND m.joined_at <= input.observed_at
	  AND (m.expires_at IS NULL OR m.expires_at > input.observed_at)
	JOIN system.tenants t ON t.id = m.tenant_id AND t.status = 'active'
), recipients AS (
	SELECT 'user'::text AS recipient_type, principal_id AS recipient_id FROM current_source
	UNION
	SELECT 'department', d.id FROM current_source s
	JOIN system.department_memberships m ON m.tenant_id = s.tenant_id
	  AND m.tenant_membership_id = s.membership_id AND m.status = 'active'
	JOIN system.departments d ON d.tenant_id = m.tenant_id AND d.id = m.department_id AND d.status = 'active'
	UNION
	SELECT 'project_group', g.id FROM current_source s
	JOIN system.project_group_memberships m ON m.tenant_id = s.tenant_id
	  AND m.tenant_membership_id = s.membership_id AND m.status = 'active'
	JOIN system.project_groups g ON g.tenant_id = m.tenant_id AND g.id = m.project_group_id AND g.status = 'active'
)
SELECT target.position, input.observed_at,
CASE WHEN NOT EXISTS (SELECT 1 FROM current_source) THEN 'source_unavailable'
     WHEN NOT EXISTS (SELECT 1 FROM system.engines e WHERE e.tenant_id = input.tenant_id
                      AND e.id = target.engine_id AND e.lifecycle_state = 'active') THEN 'target_unavailable'
     WHEN EXISTS (
	SELECT 1 FROM system.engine_access_denies d
	JOIN recipients r ON r.recipient_type = d.recipient_type AND r.recipient_id = d.recipient_id
	WHERE d.tenant_id = input.tenant_id AND d.engine_id = target.engine_id AND d.catalog_path = target.path
	  AND d.action = 'read'
	  AND ((d.expiry_mode = 'until_revoked' AND d.expires_at IS NULL)
	       OR (d.expiry_mode = 'at_time' AND d.expires_at > input.observed_at))
	  AND NOT EXISTS (SELECT 1 FROM system.engine_access_deny_releases release WHERE release.deny_id = d.deny_id)
     ) THEN 'explicit_deny'
     WHEN EXISTS (
	SELECT 1 FROM system.engine_access_grants g
	JOIN recipients r ON r.recipient_type = g.recipient_type AND r.recipient_id = g.recipient_id
	WHERE g.tenant_id = input.tenant_id AND g.engine_id = target.engine_id AND g.catalog_path = target.path
	  AND g.action = 'read' AND g.granted_at <= input.observed_at
	  AND ((g.expiry_mode = 'until_revoked' AND g.expires_at IS NULL)
	       OR (g.expiry_mode = 'at_time' AND g.expires_at > input.observed_at))
	  AND NOT EXISTS (SELECT 1 FROM system.engine_access_grant_revocations revocation WHERE revocation.request_id = g.request_id)
     ) THEN 'grant'
     ELSE 'no_grant' END AS reason
FROM targets target CROSS JOIN input ORDER BY target.position`
