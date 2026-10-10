package engineaccess

import (
	"context"
	"encoding/json"
	"time"

	commonapi "github.com/addp/common/api"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

// SourceGrantInspection is an uncached rule observation, not actual access or
// credential evidence. No denial identifiers or bodies are exposed.
type SourceGrantInspection struct {
	AccountID   int64                          `json:"account_id,string" swaggertype:"string"`
	CatalogPath engineplugin.EngineCatalogPath `json:"catalog_path"`
	ObservedAt  time.Time                      `json:"observed_at"`
	RuleCovered bool                           `json:"rule_covered"`
	Reason      string                         `json:"reason" enums:"grant,no_grant,explicit_deny,source_unavailable,target_unavailable"`
	Sources     []SourceGrantInspectionSource  `json:"sources"`
}

type SourceGrantInspectionSource struct {
	RequestID     uuid.UUID  `json:"request_id"`
	ApprovalMode  string     `json:"approval_mode"`
	RecipientType string     `json:"recipient_type"`
	RecipientID   int64      `json:"recipient_id,string" swaggertype:"string"`
	ExpiryMode    string     `json:"expiry_mode"`
	ExpiresAt     *time.Time `json:"expires_at"`
	GrantCount    int64      `json:"grant_count"`
}

// The current list and exact rule observer share source aggregation as well as
// the actual-read predicates. A source anchor identifies only that relation.
const inspectionSourceCTE = `, sources AS (
 SELECT position, recipient_type, recipient_id, count(*) AS grant_count,
 (array_agg(request_id ORDER BY granted_at DESC, request_id DESC))[1] AS request_id,
 max(granted_at) AS granted_at,
 CASE WHEN count(DISTINCT approval_mode) = 1 THEN min(approval_mode) ELSE 'mixed' END AS approval_mode,
 CASE WHEN bool_or(expiry_mode = 'until_revoked') THEN 'until_revoked' ELSE 'at_time' END AS expiry_mode,
 CASE WHEN bool_or(expiry_mode = 'until_revoked') THEN NULL ELSE max(expires_at) END AS expires_at
 FROM matched_grants GROUP BY position, recipient_type, recipient_id
), source_sets AS (
 SELECT position, sum(grant_count) AS grant_count, max(granted_at) AS granted_at,
 CASE WHEN bool_or(expiry_mode = 'until_revoked') THEN 'until_revoked' ELSE 'at_time' END AS expiry_mode,
 CASE WHEN bool_or(expiry_mode = 'until_revoked') THEN NULL ELSE max(expires_at) END AS expires_at,
 CASE WHEN count(DISTINCT approval_mode) = 1 THEN min(approval_mode) ELSE 'mixed' END AS approval_mode,
 jsonb_agg(jsonb_build_object('request_id', request_id, 'approval_mode', approval_mode,
 'recipient_type', recipient_type, 'recipient_id', recipient_id::text,
 'grant_count', grant_count, 'expiry_mode', expiry_mode, 'expires_at', expires_at)
 ORDER BY recipient_type, recipient_id) AS sources
 FROM sources GROUP BY position
)`

// One committed statement observes IAM, rules, source relations and pagination.
// Only current grant-bearing targets are listed; an empty list is not an Allow
// or a denial verdict for all other engine data. No source database is queried.
const accountGrantRelationsSQL = `WITH clock AS MATERIALIZED (SELECT clock_timestamp() AS at),
 account AS MATERIALIZED (
 SELECT m.tenant_id, p.id AS principal_id, m.id AS membership_id, p.authorization_version
 FROM system.tenant_memberships m
 JOIN system.principals p ON p.id = m.principal_id AND p.principal_type = 'user'
 JOIN system.users u ON u.id = p.id WHERE m.tenant_id = ? AND p.id = ?
), active AS MATERIALIZED (
 SELECT g.* FROM system.engine_access_grants g CROSS JOIN clock
 WHERE g.tenant_id = ? AND g.engine_id = ? AND g.granted_at <= clock.at
 AND (g.expires_at IS NULL OR g.expires_at > clock.at)
 AND NOT EXISTS (SELECT 1 FROM system.engine_access_grant_revocations r WHERE r.request_id = g.request_id)
 AND strpos(lower(array_to_string(ARRAY(SELECT x->>'name' FROM jsonb_array_elements(g.catalog_path->'segments') x), ' / ')), lower(?)) > 0
), input AS MATERIALIZED (
 SELECT account.*, COALESCE((SELECT jsonb_agg(path ORDER BY path::text)
 FROM (SELECT DISTINCT catalog_path AS path FROM active) paths), '[]'::jsonb) AS targets,
 clock.at AS observed_at FROM account CROSS JOIN clock
)` + sourceReadRuleCTEs + inspectionSourceCTE + `, applicable AS (
 SELECT target.path AS catalog_path, input.principal_id AS recipient_id,
 'user'::text AS recipient_type, 'read'::text AS action,
 COALESCE(s.grant_count, 0) AS grant_count, s.granted_at, s.expiry_mode, s.expires_at, s.approval_mode,
 jsonb_build_object('account_id', input.principal_id::text, 'catalog_path', target.path,
 'observed_at', rules.observed_at, 'rule_covered', rules.reason = 'grant', 'reason', rules.reason,
 'sources', COALESCE(s.sources, '[]'::jsonb)) AS inspection
 FROM targets target CROSS JOIN input JOIN rules ON rules.position = target.position
 LEFT JOIN source_sets s ON s.position = target.position
 WHERE s.position IS NOT NULL OR EXISTS (SELECT 1 FROM active g WHERE g.catalog_path = target.path
 AND g.recipient_type = 'user' AND g.recipient_id = input.principal_id)
), page AS (
 SELECT * FROM applicable ORDER BY catalog_path::text LIMIT ? OFFSET ?
)
 SELECT EXISTS(SELECT 1 FROM account) AS account_exists,
 COALESCE((SELECT jsonb_agg(to_jsonb(p) || jsonb_build_object('recipient_id', p.recipient_id::text)
 ORDER BY p.catalog_path::text) FROM page p), '[]'::jsonb) AS items,
 (SELECT count(*) FROM applicable) AS total`

func (r *Repository) listAccountGrantRelations(ctx context.Context, tenantID, engineID, accountID int64, page, size int, search string) ([]SourceGrantRelation, int64, error) {
	var result struct {
		AccountExists bool
		Items         json.RawMessage
		Total         int64
	}
	if err := r.db.WithContext(ctx).Raw(accountGrantRelationsSQL, tenantID, accountID, tenantID, engineID, search, size, (page-1)*size).Scan(&result).Error; err != nil {
		return nil, 0, err
	}
	if !result.AccountExists {
		return nil, 0, commonapi.ErrNotFound
	}
	rows := make([]SourceGrantRelation, 0)
	if err := json.Unmarshal(result.Items, &rows); err != nil {
		return nil, 0, err
	}
	return rows, result.Total, nil
}
