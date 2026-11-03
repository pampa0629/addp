package engineaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	commonapi "github.com/addp/common/api"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

// SourceGrantInspection is an uncached rule observation, never access evidence.
type SourceGrantInspection struct {
	AccountID       int64                          `json:"account_id,string" swaggertype:"string"`
	CatalogPath     engineplugin.EngineCatalogPath `json:"catalog_path"`
	NodeRecursive   *bool                          `json:"node_recursive,omitempty"`
	ItemFingerprint string                         `json:"item_fingerprint,omitempty"`
	ObservedAt      time.Time                      `json:"observed_at"`
	RuleCovered     bool                           `json:"rule_covered"`
	Reason          string                         `json:"reason" enums:"grant,no_grant,source_unavailable,target_unavailable"`
	Sources         []SourceGrantInspectionSource  `json:"sources"`
}

type SourceGrantInspectionSource struct {
	Action        string          `json:"action"`
	RequestID     uuid.UUID       `json:"request_id"`
	ApprovalMode  string          `json:"approval_mode"`
	CatalogPath   json.RawMessage `json:"catalog_path" swaggertype:"object"`
	NodeRecursive *bool           `json:"node_recursive,omitempty"`
	RecipientType string          `json:"recipient_type"`
	RecipientID   int64           `json:"recipient_id,string" swaggertype:"string"`
	ExpiryMode    string          `json:"expiry_mode"`
	ExpiresAt     *time.Time      `json:"expires_at"`
	GrantCount    int64           `json:"grant_count"`
}

// The current list and exact rule observer share source aggregation as well as
// the actual-read predicates. A source anchor identifies only that relation.
const inspectionSourceCTE = `, observed_grants AS (SELECT * FROM matched_grants)` + inspectionSourceAggregationCTEs

const inspectionSourceAggregationCTEs = `, sources AS (
 SELECT position, catalog_path, node_recursive, recipient_type, recipient_id, action, count(*) AS grant_count,
 (array_agg(request_id ORDER BY granted_at DESC, request_id DESC))[1] AS request_id,
 max(granted_at) AS granted_at,
 CASE WHEN count(DISTINCT approval_mode) = 1 THEN min(approval_mode) ELSE 'mixed' END AS approval_mode,
 CASE WHEN bool_or(expiry_mode = 'until_revoked') THEN 'until_revoked' ELSE 'at_time' END AS expiry_mode,
 CASE WHEN bool_or(expiry_mode = 'until_revoked') THEN NULL ELSE max(expires_at) END AS expires_at
 FROM observed_grants GROUP BY position, catalog_path, node_recursive, recipient_type, recipient_id, action
), source_sets AS (
 SELECT position, sum(grant_count) AS grant_count, max(granted_at) AS granted_at,
 CASE WHEN bool_or(expiry_mode = 'until_revoked') THEN 'until_revoked' ELSE 'at_time' END AS expiry_mode,
 CASE WHEN bool_or(expiry_mode = 'until_revoked') THEN NULL ELSE max(expires_at) END AS expires_at,
 CASE WHEN count(DISTINCT approval_mode) = 1 THEN min(approval_mode) ELSE 'mixed' END AS approval_mode,
 jsonb_agg(jsonb_build_object('request_id', request_id, 'approval_mode', approval_mode,
 'catalog_path', catalog_path, 'node_recursive', node_recursive, 'recipient_type', recipient_type, 'recipient_id', recipient_id::text, 'action', action,
 'grant_count', grant_count, 'expiry_mode', expiry_mode, 'expires_at', expires_at)
 ORDER BY recipient_type, recipient_id, action) AS sources
 FROM sources GROUP BY position
)`

// The account list retains original node/DataItem targets. It never invents leaf
// issuance rows for a node Grant. Matching inherited sources includes their real
// scope and withdrawal anchor; the observation still is not current Meta proof.
const accountGrantRelationsSQL = `WITH account AS MATERIALIZED (
 SELECT m.tenant_id,p.id AS principal_id,m.id AS membership_id,p.authorization_version
 FROM system.tenant_memberships m JOIN system.principals p ON p.id=m.principal_id
 JOIN system.users u ON u.id=p.id WHERE m.tenant_id=? AND p.id=? AND p.principal_type='user'
), input AS MATERIALIZED (SELECT account.*,clock_timestamp() AS observed_at FROM account)
` + sourceReadSubjectCTEs + `, active AS MATERIALIZED (
 SELECT g.* FROM system.engine_access_grants g CROSS JOIN input
 WHERE g.tenant_id=input.tenant_id AND g.engine_id=? AND g.granted_at<=input.observed_at
 AND (g.expires_at IS NULL OR g.expires_at>input.observed_at)
 AND NOT EXISTS(SELECT 1 FROM system.engine_access_grant_revocations r WHERE r.request_id=g.request_id)
 AND %s
), relation_targets AS (
 SELECT catalog_path,node_recursive,item_fingerprint,action,
 row_number() OVER (ORDER BY catalog_path::text,node_recursive,action) AS position
 FROM active CROSS JOIN input WHERE (recipient_type='user' AND recipient_id=input.principal_id)
 OR EXISTS(SELECT 1 FROM recipients r WHERE r.recipient_type=active.recipient_type AND r.recipient_id=active.recipient_id)
 GROUP BY catalog_path,node_recursive,item_fingerprint,action
), observed_grants AS (
 SELECT g.*,target.position FROM relation_targets target JOIN active g ON
 ((g.catalog_path=target.catalog_path AND g.node_recursive IS NOT DISTINCT FROM target.node_recursive)
  OR (target.node_recursive IS NULL AND g.node_recursive IS NOT NULL AND system.source_node_covers(g.catalog_path,g.node_recursive,target.catalog_path)))
 JOIN recipients r ON r.recipient_type=g.recipient_type AND r.recipient_id=g.recipient_id
 WHERE (g.action=target.action OR (target.action='metadata_read' AND g.action='read'))
)` + inspectionSourceAggregationCTEs + `, applicable AS (
 SELECT target.catalog_path,target.node_recursive,target.item_fingerprint,target.action,
 input.principal_id AS recipient_id,'user'::text AS recipient_type,COALESCE(s.grant_count,0) AS grant_count,
 s.granted_at,s.expiry_mode,s.expires_at,s.approval_mode,
 jsonb_build_object('account_id',input.principal_id::text,'catalog_path',target.catalog_path,
 'node_recursive',target.node_recursive,'item_fingerprint',target.item_fingerprint,'observed_at',input.observed_at,
 'rule_covered',reason.value='grant','reason',reason.value,'sources',COALESCE(s.sources,'[]'::jsonb)) AS inspection
 FROM relation_targets target CROSS JOIN input LEFT JOIN source_sets s ON s.position=target.position
 CROSS JOIN LATERAL (SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM current_source) THEN 'source_unavailable'
 WHEN NOT EXISTS(SELECT 1 FROM system.engines e WHERE e.tenant_id=input.tenant_id
 AND e.id=(target.catalog_path->>'engine_id')::bigint AND e.lifecycle_state='active'
 AND (target.node_recursive IS NULL OR e.engine_type='postgresql')) THEN 'target_unavailable'
 WHEN s.grant_count>0 THEN 'grant' ELSE 'no_grant' END AS value) reason
), page AS (SELECT * FROM applicable ORDER BY catalog_path::text,node_recursive,action LIMIT ? OFFSET ?)
 SELECT EXISTS(SELECT 1 FROM account) AS account_exists,
 COALESCE((SELECT jsonb_agg(to_jsonb(p) || jsonb_build_object('recipient_id',p.recipient_id::text)
 ORDER BY p.catalog_path::text,p.node_recursive,p.action) FROM page p),'[]'::jsonb) AS items,
 (SELECT count(*) FROM applicable) AS total`

func (r *Repository) listAccountGrantRelations(ctx context.Context, tenantID, engineID, accountID int64, page, size int, filter SourceGrantFilter) ([]SourceGrantRelation, int64, error) {
	var result struct {
		AccountExists bool
		Items         json.RawMessage
		Total         int64
	}
	predicate, args := filter.targetPredicate()
	parameters := append([]any{tenantID, accountID, engineID}, args...)
	parameters = append(parameters, size, (page-1)*size)
	if err := r.db.WithContext(ctx).Raw(fmt.Sprintf(accountGrantRelationsSQL, predicate), parameters...).Scan(&result).Error; err != nil {
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
