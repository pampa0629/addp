package engineaccess

import (
	"context"
	"encoding/json"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/authorization"
	"github.com/google/uuid"
)

// SourceGrantRelation is a derived management view, never a second ACL or an
// effective-access verdict. RequestID locates one current relation for withdrawal.
type SourceGrantRelation struct {
	RequestID     uuid.UUID       `json:"request_id"`
	CatalogPath   json.RawMessage `json:"catalog_path" swaggertype:"object"`
	RecipientType string          `json:"recipient_type"`
	RecipientID   int64           `json:"recipient_id,string" swaggertype:"string"`
	Action        string          `json:"action"`
	ExpiryMode    string          `json:"expiry_mode"`
	ExpiresAt     *time.Time      `json:"expires_at"`
	GrantedAt     time.Time       `json:"granted_at"`
	GrantCount    int64           `json:"grant_count"`
	ApprovalMode  string          `json:"approval_mode"`
}

const currentGrantRelationsSQL = `WITH now AS MATERIALIZED (SELECT clock_timestamp() AS at),
active AS MATERIALIZED (
 SELECT g.* FROM system.engine_access_grants g CROSS JOIN now
 WHERE g.tenant_id = ? AND g.engine_id = ? AND g.granted_at <= now.at
 AND (g.expires_at IS NULL OR g.expires_at > now.at)
 AND NOT EXISTS (SELECT 1 FROM system.engine_access_grant_revocations r WHERE r.request_id = g.request_id)
), relations AS (
 SELECT catalog_path, recipient_type, recipient_id, action, count(*) AS grant_count,
 CASE WHEN bool_or(expiry_mode = 'until_revoked') THEN 'until_revoked' ELSE 'at_time' END AS expiry_mode,
 CASE WHEN bool_or(expiry_mode = 'until_revoked') THEN NULL ELSE max(expires_at) END AS expires_at,
 CASE WHEN count(DISTINCT approval_mode) = 1 THEN min(approval_mode) ELSE 'mixed' END AS approval_mode,
 (array_agg(request_id ORDER BY granted_at DESC, request_id DESC))[1] AS request_id,
 max(granted_at) AS granted_at
 FROM active GROUP BY catalog_path, recipient_type, recipient_id, action
)
 SELECT * FROM relations`

func (s *Service) ListSourceGrantRelations(ctx context.Context, actor Actor, engineID int64, page, size int, filter SourceGrantFilter) ([]SourceGrantRelation, int64, error) {
	if page <= 0 || size <= 0 || size > 100 || page > int(^uint(0)>>1)/size {
		return nil, 0, commonapi.ErrBadRequest
	}
	if err := filter.Validate(); err != nil {
		return nil, 0, err
	}
	rows := make([]SourceGrantRelation, 0)
	var total int64
	err := s.withEngineManagementScope(ctx, actor, engineID, authorization.PermissionSystemEngineAccessGrantRead, false,
		func(tx *Repository, check func() error) error {
			// One statement captures the wall clock, current rows and total together,
			// including an empty page. No unbounded history load or in-memory grouping.
			var result struct {
				Items json.RawMessage
				Total int64
			}
			predicate, args := filter.predicate()
			query := `WITH current_relations AS (` + currentGrantRelationsSQL + `), filtered AS (
 SELECT * FROM current_relations WHERE ` + predicate + `), page AS (
 SELECT * FROM filtered ORDER BY granted_at DESC, request_id DESC LIMIT ? OFFSET ?)
 SELECT COALESCE((SELECT jsonb_agg(to_jsonb(p) || jsonb_build_object('recipient_id', p.recipient_id::text)
 ORDER BY p.granted_at DESC, p.request_id DESC) FROM page p), '[]'::jsonb) AS items,
 (SELECT count(*) FROM filtered) AS total`
			parameters := append([]any{actor.TenantID, engineID}, args...)
			parameters = append(parameters, size, (page-1)*size)
			if err := tx.db.WithContext(ctx).Raw(query, parameters...).Scan(&result).Error; err != nil {
				return err
			}
			if err := json.Unmarshal(result.Items, &rows); err != nil {
				return err
			}
			total = result.Total
			return check()
		})
	return rows, total, err
}

// Caller holds the exact target boundary. Personal and organization recipients
// are never merged; approval provenance and operator do not split a relation.
func (r *Repository) activeRelationGrants(ctx context.Context, grant sourceGrant) ([]sourceGrant, error) {
	var rows []sourceGrant
	err := r.db.WithContext(ctx).Where(`tenant_id = ? AND engine_id = ? AND catalog_path = ?::jsonb
 AND recipient_type = ? AND recipient_id = ? AND action = ? AND granted_at <= clock_timestamp()
 AND (expires_at IS NULL OR expires_at > clock_timestamp())
 AND NOT EXISTS (SELECT 1 FROM system.engine_access_grant_revocations r WHERE r.request_id = system.engine_access_grants.request_id)`,
		grant.TenantID, grant.EngineID, string(grant.CatalogPath), grant.RecipientType, grant.RecipientID, grant.Action).
		Order("request_id").Find(&rows).Error
	return rows, err
}
