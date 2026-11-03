package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	engineplugin "github.com/addp/common/engine/plugin"
)

var errSourceReadRules = errors.New("invalid or incomplete source read rule observation")

// Private rule-layer input, NOT authentication evidence. A future trusted IAM /
// execution consumer must establish the live credential and complete Provider
// read set before using this method; no HTTP body may supply these references.
type sourceReadRequest struct {
	Action   string
	TenantID int64
	Source   userProvenance
	Targets  []engineplugin.EngineCatalogPath
	Bindings []authorizationfacts.Binding
}

type sourceReadTargetRule struct {
	Path      engineplugin.EngineCatalogPath
	Covered   bool
	Reason    string
	ExpiresAt *time.Time
}

// Covered means only that current source rules cover ALL targets. It is not an
// executable permission, token or lease; function, execution and Security checks
// are independent. The observation must not be cached as a later access verdict.
type sourceReadRules struct {
	ObservedAt time.Time
	Covered    bool
	Targets    []sourceReadTargetRule
	// ExpiresAt is the earliest effective source deadline in this observation.
	// Nil means until revoked ONLY when Covered is true. This is not a lease
	// or an issued produced-data Grant; future issuance must freeze its own facts.
	ExpiresAt *time.Time
}

func (request sourceReadRequest) encode() ([]engineplugin.EngineCatalogPath, json.RawMessage, error) {
	if request.TenantID <= 0 || !request.Source.valid() || !authorization.ValidSourceAccessAction(request.Action) {
		return nil, nil, errSourceReadRules
	}
	paths, _, err := encodeSourceReadTargets(request.Targets)
	if err != nil {
		return nil, nil, err
	}
	batch, err := encodeBoundSourceTargets(paths, request.Bindings, request.Action)
	return paths, batch, err
}

func encodeBoundSourceTargets(paths []engineplugin.EngineCatalogPath, bindings []authorizationfacts.Binding, action string) (json.RawMessage, error) {
	if len(paths) == 0 || len(paths) != len(bindings) || !authorization.ValidSourceAccessAction(action) {
		return nil, errSourceReadRules
	}
	for i, b := range bindings {
		if b.Validate() != nil || (b.MetadataTarget != nil && action != authorization.SourceAccessMetadataRead) {
			return nil, errSourceReadRules
		}
		a, err := authorization.EncodeSharingTarget(paths[i])
		primary, primaryErr := b.PrimaryTarget()
		p, otherErr := authorization.EncodeSharingTarget(primary)
		if err != nil || primaryErr != nil || otherErr != nil || string(a) != string(p) {
			return nil, errSourceReadRules
		}
	}
	type ruleTarget struct {
		Binding authorizationfacts.Binding `json:"binding"`
		Action  string                     `json:"requested_action"`
	}
	bound := make([]ruleTarget, len(bindings))
	for i, binding := range bindings {
		bound[i] = ruleTarget{Binding: binding, Action: action}
	}
	return json.Marshal(bound)
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
	ExpiresAt  *time.Time
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
		case "grant", "no_grant", "source_unavailable", "target_unavailable":
		default:
			return nil, errSourceReadRules
		}
		covered := row.Reason == "grant"
		result.Targets[i] = sourceReadTargetRule{Path: paths[i], Covered: covered, Reason: row.Reason}
		if row.ExpiresAt != nil {
			if !covered || !row.ExpiresAt.After(result.ObservedAt) {
				return nil, errSourceReadRules
			}
			expiresAt := row.ExpiresAt.UTC()
			result.Targets[i].ExpiresAt = &expiresAt
			if result.ExpiresAt == nil || expiresAt.Before(*result.ExpiresAt) {
				deadline := expiresAt
				result.ExpiresAt = &deadline
			}
		}
		result.Covered = result.Covered && covered
	}
	if !result.Covered {
		result.ExpiresAt = nil
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

// The trusted credential adapter and qualified management inspection reuse the
// same rule CTEs; management observations never become credential evidence.
func (r *Repository) queryCurrentSourceRules(ctx context.Context, request sourceReadRequest, paths []engineplugin.EngineCatalogPath, batch json.RawMessage) (*sourceReadRules, error) {
	if !authorization.ValidSourceAccessAction(request.Action) {
		return nil, errSourceReadRules
	}
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
// revocation PKs support the anti-joins. Never take target or IAM locks.
// The fulfillment Deadline is an issuance window, NOT the Grant access expiry.
const currentSourceReadRulesSQL = `
WITH input AS MATERIALIZED (
	SELECT ?::bigint AS tenant_id, ?::bigint AS principal_id,
	       ?::bigint AS membership_id, ?::bigint AS authorization_version,
	       ?::jsonb AS targets, clock_timestamp() AS observed_at
)` + sourceReadRuleCTEs + ` SELECT position, observed_at, reason, expires_at FROM rules ORDER BY position`

// Both actual reads and management inspection consume these exact predicates.
// The caller supplies input from trusted credential resolution or IAM lookup.
const sourceReadRuleCTEs = `, targets AS (
 SELECT primary_path.path, target.value->'binding' AS proven_boundary, target.ordinality AS position, target.value->>'requested_action' AS requested_action,
       (primary_path.path->>'engine_id')::bigint AS engine_id,
       target.value->'binding'->>'item_fingerprint' AS item_fingerprint, target.value->'binding'->>'boundary_hash' AS boundary_hash
	FROM input, jsonb_array_elements(input.targets) WITH ORDINALITY AS target(value, ordinality)
 CROSS JOIN LATERAL (SELECT CASE WHEN jsonb_exists(target.value->'binding', 'file_composition') THEN
   (SELECT ref->'target' FROM jsonb_array_elements(target.value->'binding'->'file_composition'->'refs') AS ref
    WHERE ref->>'primary' = 'true') WHEN jsonb_exists(target.value->'binding', 'scope') THEN target.value->'binding'->'scope'->'root'
    WHEN jsonb_exists(target.value->'binding', 'metadata_target') THEN target.value->'binding'->'metadata_target'->'target'
    ELSE target.value->'binding'->'native_target' END AS path) primary_path
)` + sourceReadSubjectCTEs + `, matched_grants AS MATERIALIZED (
 SELECT g.*, target.position,
  LEAST(g.expires_at, (SELECT membership_expires_at FROM current_source)) AS effective_expires_at
 FROM targets target CROSS JOIN input
 JOIN system.engine_access_grants g ON g.tenant_id = input.tenant_id
  AND g.engine_id = target.engine_id AND (
   (g.node_recursive IS NULL AND g.item_fingerprint = target.item_fingerprint AND g.boundary_hash = target.boundary_hash)
   OR (g.node_recursive IS NOT NULL AND system.source_node_covers(g.catalog_path, g.node_recursive, target.path)
       AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(target.proven_boundary->'file_composition'->'refs','[]'::jsonb)) AS ref
         WHERE system.source_node_covers(g.catalog_path,g.node_recursive,ref->'target') IS NOT TRUE)
       AND EXISTS (SELECT 1 FROM system.engines e WHERE e.id=target.engine_id AND e.tenant_id=input.tenant_id AND e.engine_type IN ('postgresql','nfs','minio')))
  )
 JOIN recipients r ON r.recipient_type = g.recipient_type AND r.recipient_id = g.recipient_id
 WHERE (g.action = target.requested_action OR (target.requested_action = 'metadata_read' AND g.action = 'read'))
  AND ` + sourceGrantCurrentPredicate + `
), grant_deadlines AS (
 SELECT position,
 CASE WHEN bool_or(effective_expires_at IS NULL) THEN NULL ELSE max(effective_expires_at) END AS expires_at
 FROM matched_grants GROUP BY position
), rule_reasons AS (
SELECT target.position, input.observed_at,
CASE WHEN NOT EXISTS (SELECT 1 FROM current_source) THEN 'source_unavailable'
     WHEN NOT EXISTS (SELECT 1 FROM system.engines e WHERE e.tenant_id = input.tenant_id
                      AND e.id = target.engine_id AND e.lifecycle_state = 'active'
                      AND (target.requested_action = 'metadata_read' OR
                       ((target.path->'segments'->-1->>'term' <> 'file' OR e.engine_type IN ('ftp','nfs'))
                       AND (target.path->'segments'->-1->>'term' <> 'object' OR e.engine_type = 'minio')))) THEN 'target_unavailable'
     WHEN EXISTS (SELECT 1 FROM matched_grants g WHERE g.position = target.position) THEN 'grant'
     ELSE 'no_grant' END AS reason
FROM targets target CROSS JOIN input
), rules AS (
 SELECT r.*, CASE WHEN r.reason = 'grant' THEN d.expires_at END AS expires_at
 FROM rule_reasons r LEFT JOIN grant_deadlines d USING (position))`

const sourceGrantCurrentPredicate = `g.granted_at <= input.observed_at
 AND ((g.expiry_mode = 'until_revoked' AND g.expires_at IS NULL)
 OR (g.expiry_mode = 'at_time' AND g.expires_at > input.observed_at))
 AND NOT EXISTS (SELECT 1 FROM system.engine_access_grant_revocations revocation WHERE revocation.request_id=g.request_id)`

// Current identity and direct organization membership are shared by actual reads
// and management observations. Neither a historical operator nor a parent
// department is a source of access.
const sourceReadSubjectCTEs = `, current_source AS (
 SELECT input.*, m.expires_at AS membership_expires_at FROM input
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
)`
